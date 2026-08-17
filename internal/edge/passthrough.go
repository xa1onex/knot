package edge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

type peekConn struct {
	net.Conn
	buf []byte
}

func (c *peekConn) Read(b []byte) (int, error) {
	if len(c.buf) > 0 {
		n := copy(b, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}
	return c.Conn.Read(b)
}

// StartPassthrough listens for raw TLS connections routed by SNI (origin_tls routes).
func (p *Proxy) StartPassthrough(ctx context.Context, addr string) (string, error) {
	if addr == "" {
		return "", nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	if p.passthroughLn != nil {
		_ = p.passthroughLn.Close()
	}
	p.passthroughLn = ln
	p.mu.Unlock()
	go p.servePassthrough(ctx, ln)
	return ln.Addr().String(), nil
}

func (p *Proxy) servePassthrough(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
		}
		go p.handlePassthroughConn(ctx, conn)
	}
}

func (p *Proxy) handlePassthroughConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	sni, record, err := readTLSClientHello(conn)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil || sni == "" {
		return
	}
	rt, err := p.LookupHost(sni)
	if err != nil || rt == nil {
		return
	}
	if rt.TLSMode != protocol.TLSModeOriginTLS {
		return
	}
	origin := p.ResolveOrigin(rt)
	if p.Sender == nil || !p.Sender.IsOnline(origin.DeviceID) {
		return
	}
	if !p.acquire(origin.DeviceID, rt.ServiceID) {
		return
	}
	defer p.release(origin.DeviceID, rt.ServiceID)

	reqID := store.NewID()
	s := newByteStreamSession(p, reqID, origin.DeviceID, rt.ServiceID)
	p.mu.Lock()
	if p.byteStreams == nil {
		p.byteStreams = make(map[string]*byteStreamSession)
	}
	p.byteStreams[reqID] = s
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.byteStreams, reqID)
		p.mu.Unlock()
	}()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.ctx = streamCtx

	open := protocol.EdgeStreamOpen{
		Type: protocol.TypeEdgeStreamOpen, RequestID: reqID,
		Port: origin.Port, Hostname: rt.Hostname,
	}
	if err := p.Sender.SendJSON(origin.DeviceID, open); err != nil {
		return
	}
	if err := s.waitReady(streamCtx); err != nil {
		return
	}

	pc := &peekConn{Conn: conn, buf: record}
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = p.pumpStreamDown(streamCtx, origin.DeviceID, reqID, pc, s)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = p.pumpStreamUp(streamCtx, origin.DeviceID, reqID, pc, s)
	}()

	wg.Wait()
	_ = p.Sender.SendJSON(origin.DeviceID, protocol.EdgeStreamClose{
		Type: protocol.TypeEdgeStreamClose, RequestID: reqID,
	})
}

func (p *Proxy) pumpStreamUp(ctx context.Context, deviceID, reqID string, r io.Reader, s *byteStreamSession) error {
	buf := make([]byte, protocol.EdgeStreamChunkBytes)
	seq := 0
	var total int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > protocol.MaxEdgeStreamBytes {
				_ = p.Sender.SendJSON(deviceID, protocol.EdgeStreamFail{
					Type: protocol.TypeEdgeStreamFail, RequestID: reqID, Error: ErrRequestLarge.Error(),
				})
				return ErrRequestLarge
			}
			last := err == io.EOF
			msg := protocol.EdgeStreamData{
				Type: protocol.TypeEdgeStreamData, RequestID: reqID, Direction: "up", Seq: seq,
				DataB64: base64.StdEncoding.EncodeToString(buf[:n]), Last: last,
			}
			if err := p.Sender.SendJSON(deviceID, msg); err != nil {
				return ErrOffline
			}
			if err := s.waitAck(ctx, "up", seq); err != nil {
				return err
			}
			seq++
			if last {
				return nil
			}
		}
		if err == io.EOF {
			if seq == 0 {
				msg := protocol.EdgeStreamData{
					Type: protocol.TypeEdgeStreamData, RequestID: reqID, Direction: "up", Seq: 0, Last: true,
				}
				if err := p.Sender.SendJSON(deviceID, msg); err != nil {
					return ErrOffline
				}
				return s.waitAck(ctx, "up", 0)
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (p *Proxy) pumpStreamDown(ctx context.Context, deviceID, reqID string, w io.Writer, s *byteStreamSession) error {
	idle := p.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	for {
		idleCtx, cancel := context.WithTimeout(ctx, idle)
		raw, err := s.recv(idleCtx)
		cancel()
		if err != nil {
			return err
		}
		switch msg := raw.(type) {
		case protocol.EdgeStreamData:
			if msg.Direction != "down" {
				s.deliver(msg)
				continue
			}
			if msg.DataB64 != "" {
				chunk, err := base64.StdEncoding.DecodeString(msg.DataB64)
				if err != nil {
					return err
				}
				if _, err := w.Write(chunk); err != nil {
					return err
				}
			}
			if err := p.Sender.SendJSON(deviceID, protocol.EdgeStreamAck{
				Type: protocol.TypeEdgeStreamAck, RequestID: reqID, Direction: "down", Seq: msg.Seq,
			}); err != nil {
				return ErrOffline
			}
			if msg.Last {
				return nil
			}
		case protocol.EdgeStreamAck:
			if msg.Direction == "up" {
				s.deliver(msg)
			}
			continue
		case protocol.EdgeStreamReady:
			continue
		case protocol.EdgeStreamFail:
			if msg.Error != "" {
				return errors.New(msg.Error)
			}
			return ErrStreamAborted
		case protocol.EdgeStreamClose:
			return nil
		default:
			continue
		}
	}
}

func (p *Proxy) handleByteStreamAgentMessage(envelopeType string, raw []byte) error {
	switch envelopeType {
	case protocol.TypeEdgeStreamReady:
		var msg protocol.EdgeStreamReady
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverByteStream(msg.RequestID, msg)
	case protocol.TypeEdgeStreamData:
		var msg protocol.EdgeStreamData
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverByteStream(msg.RequestID, msg)
	case protocol.TypeEdgeStreamAck:
		var msg protocol.EdgeStreamAck
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverByteStream(msg.RequestID, msg)
	case protocol.TypeEdgeStreamFail:
		var msg protocol.EdgeStreamFail
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverByteStream(msg.RequestID, msg)
	case protocol.TypeEdgeStreamClose:
		var msg protocol.EdgeStreamClose
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverByteStream(msg.RequestID, msg)
	}
	return nil
}
