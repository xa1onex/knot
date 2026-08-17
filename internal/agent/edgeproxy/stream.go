package edgeproxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"sync"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

type streamBridge struct {
	reqID    string
	conn     net.Conn
	send     func(v any) error
	upAcks   chan int
	cancel   context.CancelFunc
	upBytes  int64
	downSeq  int
}

type streamState struct {
	mu      sync.Mutex
	active  map[string]*streamBridge
	running int
}

func (m *Manager) streamState() *streamState {
	if m.streams == nil {
		m.streams = &streamState{active: make(map[string]*streamBridge)}
	}
	return m.streams
}

func (m *Manager) handleStreamOpen(data []byte) {
	var req protocol.EdgeStreamOpen
	if json.Unmarshal(data, &req) != nil {
		return
	}
	st := m.streamState()
	st.mu.Lock()
	if st.running >= protocol.MaxEdgeStreamConcurrent {
		st.mu.Unlock()
		_ = m.Send(protocol.EdgeStreamReady{
			Type: protocol.TypeEdgeStreamReady, RequestID: req.RequestID, OK: false, Error: "too many streams",
		})
		return
	}
	st.running++
	st.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	addr := net.JoinHostPort("127.0.0.1", itoa(req.Port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	ready := protocol.EdgeStreamReady{Type: protocol.TypeEdgeStreamReady, RequestID: req.RequestID}
	if err != nil {
		cancel()
		st.mu.Lock()
		st.running--
		st.mu.Unlock()
		ready.OK = false
		ready.Error = err.Error()
		_ = m.Send(ready)
		return
	}
	ready.OK = true
	_ = m.Send(ready)

	br := &streamBridge{
		reqID: req.RequestID, conn: conn, send: m.Send,
		upAcks: make(chan int, protocol.MaxEdgeStreamInflight+1), cancel: cancel,
	}
	st.mu.Lock()
	st.active[req.RequestID] = br
	st.mu.Unlock()

	go m.pumpOriginDown(ctx, br, st)
}

func (m *Manager) handleStreamData(data []byte) {
	var req protocol.EdgeStreamData
	if json.Unmarshal(data, &req) != nil || req.Direction != "up" {
		return
	}
	st := m.streamState()
	st.mu.Lock()
	br := st.active[req.RequestID]
	st.mu.Unlock()
	if br == nil {
		return
	}
	if req.DataB64 != "" {
		raw, err := base64.StdEncoding.DecodeString(req.DataB64)
		if err != nil {
			m.streamFail(req.RequestID, st, "bad chunk")
			return
		}
		br.upBytes += int64(len(raw))
		if br.upBytes > protocol.MaxEdgeStreamBytes {
			m.streamFail(req.RequestID, st, "stream too large")
			return
		}
		if _, err := br.conn.Write(raw); err != nil {
			m.streamFail(req.RequestID, st, err.Error())
			return
		}
	}
	_ = m.Send(protocol.EdgeStreamAck{
		Type: protocol.TypeEdgeStreamAck, RequestID: req.RequestID, Direction: "up", Seq: req.Seq,
	})
	if req.Last {
		if tc, ok := br.conn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}
}

func (m *Manager) handleStreamAck(data []byte) {
	var req protocol.EdgeStreamAck
	if json.Unmarshal(data, &req) != nil || req.Direction != "down" {
		return
	}
	st := m.streamState()
	st.mu.Lock()
	br := st.active[req.RequestID]
	st.mu.Unlock()
	if br == nil {
		return
	}
	select {
	case br.upAcks <- req.Seq:
	default:
	}
}

func (m *Manager) handleStreamClose(data []byte) {
	var req protocol.EdgeStreamClose
	if json.Unmarshal(data, &req) != nil {
		return
	}
	m.streamTeardown(req.RequestID)
}

func (m *Manager) pumpOriginDown(ctx context.Context, br *streamBridge, st *streamState) {
	defer m.streamTeardown(br.reqID)
	buf := make([]byte, protocol.EdgeStreamChunkBytes)
	seq := 0
	for {
		_ = br.conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		n, err := br.conn.Read(buf)
		if n > 0 {
			last := err == io.EOF
			msg := protocol.EdgeStreamData{
				Type: protocol.TypeEdgeStreamData, RequestID: br.reqID, Direction: "down", Seq: seq,
				DataB64: base64.StdEncoding.EncodeToString(buf[:n]), Last: last,
			}
			if sendErr := br.send(msg); sendErr != nil {
				return
			}
			if !m.waitDownAck(ctx, br, seq) {
				return
			}
			seq++
			if last {
				return
			}
		}
		if err == io.EOF {
			if seq == 0 {
				_ = br.send(protocol.EdgeStreamData{
					Type: protocol.TypeEdgeStreamData, RequestID: br.reqID, Direction: "down", Seq: 0, Last: true,
				})
			}
			return
		}
		if err != nil {
			return
		}
	}
}

func (m *Manager) waitDownAck(ctx context.Context, br *streamBridge, want int) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case got := <-br.upAcks:
			if got == want {
				return true
			}
		case <-time.After(2 * time.Minute):
			return false
		}
	}
}

func (m *Manager) streamFail(reqID string, st *streamState, msg string) {
	_ = m.Send(protocol.EdgeStreamFail{Type: protocol.TypeEdgeStreamFail, RequestID: reqID, Error: msg})
	m.streamTeardown(reqID)
}

func (m *Manager) streamTeardown(reqID string) {
	st := m.streamState()
	st.mu.Lock()
	br := st.active[reqID]
	if br != nil {
		delete(st.active, reqID)
		br.cancel()
		_ = br.conn.Close()
		st.running--
	}
	st.mu.Unlock()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
