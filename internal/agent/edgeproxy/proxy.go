// Package edgeproxy terminates Edge HTTP on the agent by dialing loopback only.
package edgeproxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

const (
	maxConcurrent = protocol.MaxEdgeConcurrentPerDevice
	reqTimeout    = 5 * time.Minute
	idleTimeout   = 2 * time.Minute
)

type Manager struct {
	Send func(v any) error

	mu      sync.Mutex
	active  map[string]*inflight
	running int
	streams *streamState
}

type inflight struct {
	reqID       string
	pipeW       *io.PipeWriter
	cancel      context.CancelFunc
	respAcks    chan int
	bodyWritten int64
}

func (m *Manager) Handle(data []byte) {
	var env struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &env) != nil {
		return
	}
	switch env.Type {
	case protocol.TypeEdgeHTTPBegin:
		var req protocol.EdgeHTTPBegin
		if json.Unmarshal(data, &req) != nil {
			return
		}
		m.begin(req)
	case protocol.TypeEdgeHTTPBody:
		var req protocol.EdgeHTTPBody
		if json.Unmarshal(data, &req) != nil {
			return
		}
		m.body(req)
	case protocol.TypeEdgeHTTPAck:
		var req protocol.EdgeHTTPAck
		if json.Unmarshal(data, &req) != nil {
			return
		}
		if req.Direction == "resp" {
			m.respAck(req)
		}
	case protocol.TypeEdgeHTTPFail:
		var req protocol.EdgeHTTPFail
		if json.Unmarshal(data, &req) != nil {
			return
		}
		m.abort(req.RequestID)
	case protocol.TypeEdgeProbe:
		var req protocol.EdgeProbe
		if json.Unmarshal(data, &req) != nil {
			return
		}
		_ = m.Send(m.execProbe(req))
	case protocol.TypeEdgeStreamOpen:
		m.handleStreamOpen(data)
	case protocol.TypeEdgeStreamData:
		m.handleStreamData(data)
	case protocol.TypeEdgeStreamAck:
		m.handleStreamAck(data)
	case protocol.TypeEdgeStreamClose, protocol.TypeEdgeStreamFail:
		m.handleStreamClose(data)
	}
}

func (m *Manager) begin(req protocol.EdgeHTTPBegin) {
	m.mu.Lock()
	if m.running >= maxConcurrent {
		m.mu.Unlock()
		_ = m.Send(protocol.EdgeHTTPFail{
			Type: protocol.TypeEdgeHTTPFail, RequestID: req.RequestID, Error: "too many concurrent edge streams",
		})
		return
	}
	m.running++
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), reqTimeout)
	pipeR, pipeW := io.Pipe()
	s := &inflight{
		reqID:    req.RequestID,
		pipeW:    pipeW,
		cancel:   cancel,
		respAcks: make(chan int, protocol.MaxEdgeInflightChunks+1),
	}
	m.mu.Lock()
	if m.active == nil {
		m.active = make(map[string]*inflight)
	}
	m.active[req.RequestID] = s
	m.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			_ = pipeR.Close()
			m.mu.Lock()
			delete(m.active, req.RequestID)
			m.running--
			m.mu.Unlock()
		}()
		m.run(ctx, req, pipeR, s)
	}()
}

func (m *Manager) body(req protocol.EdgeHTTPBody) {
	m.mu.Lock()
	s := m.active[req.RequestID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	var raw []byte
	if req.DataB64 != "" {
		var err error
		raw, err = base64.StdEncoding.DecodeString(req.DataB64)
		if err != nil {
			m.fail(req.RequestID, "bad body")
			return
		}
	}
	if len(raw) > 0 {
		s.bodyWritten += int64(len(raw))
		if s.bodyWritten > protocol.MaxEdgeRequestBytes {
			m.fail(req.RequestID, "request too large")
			return
		}
		if _, err := s.pipeW.Write(raw); err != nil {
			m.fail(req.RequestID, err.Error())
			return
		}
	}
	_ = m.Send(protocol.EdgeHTTPAck{
		Type: protocol.TypeEdgeHTTPAck, RequestID: req.RequestID, Direction: "req", Seq: req.Seq,
	})
	if req.Last {
		_ = s.pipeW.Close()
	}
}

func (m *Manager) respAck(req protocol.EdgeHTTPAck) {
	m.mu.Lock()
	s := m.active[req.RequestID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	select {
	case s.respAcks <- req.Seq:
	default:
	}
}

func (m *Manager) abort(reqID string) {
	m.mu.Lock()
	s := m.active[reqID]
	if s != nil {
		delete(m.active, reqID)
		s.cancel()
		_ = s.pipeW.CloseWithError(io.ErrClosedPipe)
	}
	m.mu.Unlock()
}

func (m *Manager) fail(reqID, msg string) {
	_ = m.Send(protocol.EdgeHTTPFail{Type: protocol.TypeEdgeHTTPFail, RequestID: reqID, Error: msg})
	m.abort(reqID)
}

func (m *Manager) run(ctx context.Context, req protocol.EdgeHTTPBegin, body io.Reader, s *inflight) {
	head, stream, err := m.dialOrigin(ctx, req, body)
	if err != nil {
		_ = m.Send(protocol.EdgeHTTPFail{Type: protocol.TypeEdgeHTTPFail, RequestID: req.RequestID, Error: err.Error()})
		return
	}
	defer stream.Close()
	_ = m.Send(head)
	if err := m.streamOriginBody(ctx, req.RequestID, stream, s); err != nil {
		_ = m.Send(protocol.EdgeHTTPFail{Type: protocol.TypeEdgeHTTPFail, RequestID: req.RequestID, Error: err.Error()})
	}
}

func (m *Manager) dialOrigin(ctx context.Context, req protocol.EdgeHTTPBegin, body io.Reader) (protocol.EdgeHTTPRespHead, io.ReadCloser, error) {
	head := protocol.EdgeHTTPRespHead{Type: protocol.TypeEdgeHTTPRespHead, RequestID: req.RequestID}
	port := req.Port
	if port < 1 || port > 65535 {
		head.Error = "invalid port"
		return head, nil, errors.New(head.Error)
	}
	path := req.Path
	if path == "" {
		path = "/"
	}
	if err := validatePath(path); err != nil {
		head.Error = err.Error()
		return head, nil, err
	}
	rel, err := url.ParseRequestURI(path)
	if err != nil {
		head.Error = "invalid path"
		return head, nil, err
	}
	target := url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target.ResolveReference(rel).String(), body)
	if err != nil {
		head.Error = err.Error()
		return head, nil, err
	}
	for _, h := range req.Headers {
		if strings.EqualFold(h.Name, "Host") {
			continue
		}
		httpReq.Header.Add(h.Name, h.Value)
	}
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		head.Error = err.Error()
		return head, nil, err
	}
	head.OK = true
	head.Status = resp.StatusCode
	for k, vals := range resp.Header {
		if hopHeader(k) {
			continue
		}
		for _, v := range vals {
			head.Headers = append(head.Headers, protocol.HeaderKV{Name: k, Value: v})
		}
	}
	return head, resp.Body, nil
}

func hopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailers", "transfer-encoding", "upgrade", "content-length":
		return true
	default:
		return false
	}
}

func validatePath(path string) error {
	if strings.HasPrefix(path, "//") {
		return errors.New("invalid path")
	}
	rel, err := url.ParseRequestURI(path)
	if err != nil || rel.Scheme != "" || rel.Host != "" || !strings.HasPrefix(rel.EscapedPath(), "/") {
		return errors.New("invalid path")
	}
	if strings.Contains(path, "192.168.") || strings.Contains(path, "10.") {
		return errors.New("invalid path")
	}
	return nil
}

func (m *Manager) streamOriginBody(ctx context.Context, reqID string, body io.ReadCloser, s *inflight) error {
	defer body.Close()
	buf := make([]byte, protocol.EdgeChunkBytes)
	seq := 0
	for {
		n, err := body.Read(buf)
		if n > 0 {
			last := err == io.EOF
			msg := protocol.EdgeHTTPRespBody{
				Type: protocol.TypeEdgeHTTPRespBody, RequestID: reqID, Seq: seq,
				DataB64: base64.StdEncoding.EncodeToString(buf[:n]), Last: last,
			}
			if sendErr := m.Send(msg); sendErr != nil {
				return sendErr
			}
			if !m.waitRespAckSeq(ctx, s, seq) {
				return context.Canceled
			}
			seq++
			if last {
				return nil
			}
		}
		if err == io.EOF {
			if seq == 0 {
				msg := protocol.EdgeHTTPRespBody{
					Type: protocol.TypeEdgeHTTPRespBody, RequestID: reqID, Seq: 0, Last: true,
				}
				if sendErr := m.Send(msg); sendErr != nil {
					return sendErr
				}
				if !m.waitRespAckSeq(ctx, s, 0) {
					return context.Canceled
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (m *Manager) waitRespAckSeq(ctx context.Context, s *inflight, want int) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case got := <-s.respAcks:
			if got == want {
				return true
			}
		case <-time.After(idleTimeout):
			return false
		}
	}
}

func (m *Manager) execProbe(req protocol.EdgeProbe) protocol.EdgeProbeResult {
	res := protocol.EdgeProbeResult{Type: protocol.TypeEdgeProbeResult, RequestID: req.RequestID}
	addr := fmt.Sprintf("127.0.0.1:%d", req.Port)
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	_ = conn.Close()
	res.OK = true
	return res
}

func (m *Manager) AbortAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.active))
	for id := range m.active {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.abort(id)
	}
	if m.streams != nil {
		m.streams.mu.Lock()
		sids := make([]string, 0, len(m.streams.active))
		for id := range m.streams.active {
			sids = append(sids, id)
		}
		m.streams.mu.Unlock()
		for _, id := range sids {
			m.streamTeardown(id)
		}
	}
}
