package edge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrOffline       = errors.New("tunnel offline")
	ErrTimeout       = errors.New("tunnel timeout")
	ErrNotHTTP       = errors.New("service is not HTTP")
	ErrNoRoute       = errors.New("no route")
	ErrUnreachable   = errors.New("backend unreachable")
	ErrTooMany       = errors.New("too many concurrent edge streams")
	ErrRequestLarge  = errors.New("request too large")
	ErrStreamAborted = errors.New("stream aborted")
)

type Sender interface {
	SendJSON(deviceID string, v any) error
	IsOnline(deviceID string) bool
}

type Probe struct {
	Reachable bool
	Error     string
	LatencyMS int64
	At        time.Time
}

type Health struct {
	Registered       bool     `json:"registered"`
	AgentOnline      bool     `json:"agent_online"`
	TunnelConnected  bool     `json:"tunnel_connected"`
	BackendReachable bool     `json:"backend_reachable"`
	EdgeDeviceID     string   `json:"edge_device_id,omitempty"`
	EdgeDeviceName   string   `json:"edge_device_name,omitempty"`
	EdgeOnline       bool     `json:"edge_online"`
	Listen           string   `json:"listen"`
	Hostnames        []string `json:"hostnames"`
	Error            string   `json:"error,omitempty"`
}

type Proxy struct {
	Store       *store.Store
	Sender      Sender
	Logs        *oplogs.Service
	ReqTimeout  time.Duration
	IdleTimeout time.Duration

	mu            sync.Mutex
	streams       map[string]*streamSession
	byteStreams   map[string]*byteStreamSession
	passthroughLn net.Listener
	deviceActive  map[string]int
	serviceActive map[string]int
	probes        map[string]Probe
	probePending  map[string]chan any
}

func New(st *store.Store, sender Sender) *Proxy {
	return &Proxy{
		Store:         st,
		Sender:        sender,
		ReqTimeout:    DefaultReqTimeout,
		IdleTimeout:   DefaultIdleTimeout,
		streams:       make(map[string]*streamSession),
		byteStreams:   make(map[string]*byteStreamSession),
		deviceActive:  make(map[string]int),
		serviceActive: make(map[string]int),
		probes:        make(map[string]Probe),
		probePending:  make(map[string]chan any),
	}
}

func (p *Proxy) HandleAgentMessage(_ context.Context, deviceID string, envelopeType string, raw []byte) error {
	switch envelopeType {
	case protocol.TypeEdgeHTTPRespHead:
		var msg protocol.EdgeHTTPRespHead
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverStream(msg.RequestID, msg)
	case protocol.TypeEdgeHTTPRespBody:
		var msg protocol.EdgeHTTPRespBody
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverStream(msg.RequestID, msg)
	case protocol.TypeEdgeHTTPAck:
		var msg protocol.EdgeHTTPAck
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverStream(msg.RequestID, msg)
	case protocol.TypeEdgeHTTPFail:
		var msg protocol.EdgeHTTPFail
		if err := json.Unmarshal(raw, &msg); err != nil {
			return err
		}
		p.deliverStream(msg.RequestID, msg)
	case protocol.TypeEdgeProbeResult:
		var res protocol.EdgeProbeResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return err
		}
		p.deliverProbe(res.RequestID, res)
	case protocol.TypeEdgeStreamReady, protocol.TypeEdgeStreamData,
		protocol.TypeEdgeStreamAck, protocol.TypeEdgeStreamFail, protocol.TypeEdgeStreamClose:
		return p.handleByteStreamAgentMessage(envelopeType, raw)
	}
	return nil
}

func (p *Proxy) OnDeviceDisconnect(deviceID string) {
	p.mu.Lock()
	var abort []*streamSession
	for id, s := range p.streams {
		if s.deviceID == deviceID {
			abort = append(abort, s)
			delete(p.streams, id)
		}
	}
	p.mu.Unlock()
	for _, s := range abort {
		s.fail(ErrOffline)
	}
	p.abortByteStreamsForDevice(deviceID)
}

func (p *Proxy) deliverStream(requestID string, v any) {
	p.mu.Lock()
	s := p.streams[requestID]
	p.mu.Unlock()
	if s == nil {
		return
	}
	s.deliver(v)
}

func (p *Proxy) deliverProbe(requestID string, v any) {
	p.mu.Lock()
	ch := p.probePending[requestID]
	p.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- v:
	default:
	}
}

func (p *Proxy) LookupHost(host string) (*store.EdgeRoute, error) {
	return p.Store.GetEdgeRouteByHost(context.Background(), NormalizeHost(host))
}

func (p *Proxy) emitEdge(rt *store.EdgeRoute, status int, msg string) {
	if rt == nil {
		return
	}
	ev := oplogs.Event{
		UserID: rt.UserID, Source: oplogs.SourceEdge, Level: "error",
		Message: fmt.Sprintf("%d %s", status, msg),
		DeviceID: rt.DeviceID, ServiceID: rt.ServiceID, Service: rt.ServiceName,
		ReleaseID: rt.ActiveReleaseID,
		Metadata:  map[string]any{"status": status, "hostname": rt.Hostname},
	}
	if rt.ActiveReleaseID != "" {
		if rel, err := p.Store.GetRelease(context.Background(), rt.UserID, rt.ActiveReleaseID); err == nil {
			ev.TraceID = rel.TraceID
			ev.BuildID = rel.BuildID
			ev.JobID = rel.JobID
			ev.DeploymentID = rel.DeploymentID
		}
	}
	p.Logs.Emit(context.Background(), ev)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt, err := p.LookupHost(r.Host)
	if err != nil || rt == nil {
		http.Error(w, "no route", http.StatusNotFound)
		return
	}
	fail := func(status int, msg string) {
		http.Error(w, msg, status)
		if status >= 500 {
			p.emitEdge(rt, status, msg)
		}
	}
	if rt.TLSMode == protocol.TLSModeOriginTLS {
		http.Error(w, "use TLS passthrough port for origin_tls routes", http.StatusBadRequest)
		return
	}
	if rt.Protocol != "http" && rt.Protocol != "https" {
		fail(http.StatusBadGateway, "service is not HTTP")
		return
	}
	origin := p.ResolveOrigin(rt)
	if p.Sender == nil || !p.Sender.IsOnline(origin.DeviceID) {
		fail(http.StatusServiceUnavailable, "tunnel offline")
		return
	}
	if !p.acquire(origin.DeviceID, rt.ServiceID) {
		fail(http.StatusServiceUnavailable, ErrTooMany.Error())
		return
	}
	defer p.release(origin.DeviceID, rt.ServiceID)

	reqID := store.NewID()
	s := newStreamSession(p, reqID, origin.DeviceID, rt.ServiceID)
	p.mu.Lock()
	p.streams[reqID] = s
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.streams, reqID)
		p.mu.Unlock()
	}()

	ctx := r.Context()
	timeout := p.ReqTimeout
	if timeout <= 0 {
		timeout = DefaultReqTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	s.ctx = ctx

	path := r.URL.RequestURI()
	if path == "" {
		path = "/"
	}
	begin := protocol.EdgeHTTPBegin{
		Type:      protocol.TypeEdgeHTTPBegin,
		RequestID: reqID,
		Method:    r.Method,
		Path:      path,
		Port:      origin.Port,
		Headers: flattenHeaders(r.Header, []protocol.HeaderKV{
			{Name: "X-Forwarded-Host", Value: r.Host},
			{Name: "X-Forwarded-Proto", Value: forwardedProto(r)},
			{Name: "X-Forwarded-For", Value: clientIP(r)},
		}),
	}
	if err := p.Sender.SendJSON(origin.DeviceID, begin); err != nil {
		fail(http.StatusServiceUnavailable, "tunnel offline")
		return
	}

	if err := p.streamRequestBody(ctx, origin.DeviceID, reqID, r.Body, s); err != nil {
		if errors.Is(err, ErrRequestLarge) {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrStreamAborted) || errors.Is(err, ErrOffline) {
			fail(http.StatusBadGateway, "tunnel closed")
			return
		}
		fail(http.StatusBadGateway, "tunnel error: "+err.Error())
		return
	}

	headRaw, err := s.waitHead(ctx)
	if err != nil {
		if errors.Is(err, ErrOffline) || errors.Is(err, ErrStreamAborted) {
			fail(http.StatusBadGateway, "tunnel closed")
			return
		}
		fail(http.StatusBadGateway, "tunnel error: "+err.Error())
		return
	}
	head, ok := headRaw.(protocol.EdgeHTTPRespHead)
	if !ok {
		fail(http.StatusBadGateway, "bad tunnel payload")
		return
	}
	if !head.OK {
		msg := head.Error
		if msg == "" {
			msg = "backend error"
		}
		fail(http.StatusBadGateway, msg)
		return
	}

	outHdr := w.Header()
	for _, h := range head.Headers {
		if skipHop(h.Name) {
			continue
		}
		outHdr.Add(h.Name, h.Value)
	}
	status := head.Status
	if status < 100 {
		status = http.StatusOK
	}
	w.WriteHeader(status)

	fl, _ := w.(http.Flusher)
	if err := p.streamResponseBody(ctx, origin.DeviceID, reqID, w, fl, s); err != nil {
		// Headers already sent — abort connection; client retries.
		return
	}
}

func (p *Proxy) streamRequestBody(ctx context.Context, deviceID, reqID string, body io.ReadCloser, s *streamSession) error {
	defer body.Close()
	buf := make([]byte, ChunkBytes)
	seq := 0
	var total int64
	for {
		n, err := body.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > MaxRequestBytes {
				_ = p.Sender.SendJSON(deviceID, protocol.EdgeHTTPFail{
					Type: protocol.TypeEdgeHTTPFail, RequestID: reqID, Error: ErrRequestLarge.Error(),
				})
				return ErrRequestLarge
			}
			last := err == io.EOF
			msg := protocol.EdgeHTTPBody{
				Type: protocol.TypeEdgeHTTPBody, RequestID: reqID, Seq: seq,
				DataB64: base64.StdEncoding.EncodeToString(buf[:n]), Last: last,
			}
			if err := p.Sender.SendJSON(deviceID, msg); err != nil {
				return ErrOffline
			}
			if err := s.waitReqAck(ctx, seq); err != nil {
				return err
			}
			seq++
			if last {
				return nil
			}
		}
		if err == io.EOF {
			if seq == 0 {
				msg := protocol.EdgeHTTPBody{
					Type: protocol.TypeEdgeHTTPBody, RequestID: reqID, Seq: 0, Last: true,
				}
				if err := p.Sender.SendJSON(deviceID, msg); err != nil {
					return ErrOffline
				}
				return s.waitReqAck(ctx, 0)
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (p *Proxy) streamResponseBody(ctx context.Context, deviceID, reqID string, w http.ResponseWriter, fl http.Flusher, s *streamSession) error {
	idle := p.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	for {
		idleCtx, cancel := context.WithTimeout(ctx, idle)
		raw, err := s.waitBody(idleCtx)
		cancel()
		if err != nil {
			return err
		}
		switch msg := raw.(type) {
		case protocol.EdgeHTTPRespBody:
			if msg.DataB64 != "" {
				chunk, err := base64.StdEncoding.DecodeString(msg.DataB64)
				if err != nil {
					return err
				}
				if _, err := w.Write(chunk); err != nil {
					return err
				}
				if fl != nil {
					fl.Flush()
				}
			}
			if err := p.Sender.SendJSON(deviceID, protocol.EdgeHTTPAck{
				Type: protocol.TypeEdgeHTTPAck, RequestID: reqID, Direction: "resp", Seq: msg.Seq,
			}); err != nil {
				return ErrOffline
			}
			if msg.Last {
				return nil
			}
		case protocol.EdgeHTTPAck:
			// Stray ack; ignore.
			continue
		case protocol.EdgeHTTPFail:
			return errors.New(msg.Error)
		default:
			return fmt.Errorf("unexpected tunnel reply")
		}
	}
}

func (p *Proxy) acquire(deviceID, serviceID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deviceActive[deviceID] >= MaxConcurrentDevice {
		return false
	}
	if p.serviceActive[serviceID] >= MaxConcurrentService {
		return false
	}
	p.deviceActive[deviceID]++
	p.serviceActive[serviceID]++
	return true
}

func (p *Proxy) release(deviceID, serviceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deviceActive[deviceID] > 0 {
		p.deviceActive[deviceID]--
	}
	if p.serviceActive[serviceID] > 0 {
		p.serviceActive[serviceID]--
	}
}

func (p *Proxy) Probe(ctx context.Context, deviceID string, port int) (Probe, error) {
	if p.Sender == nil || !p.Sender.IsOnline(deviceID) {
		return Probe{Reachable: false, Error: ErrOffline.Error(), At: time.Now().UTC()}, nil
	}
	reqID := store.NewID()
	ch := make(chan any, 1)
	p.mu.Lock()
	p.probePending[reqID] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.probePending, reqID)
		p.mu.Unlock()
	}()
	msg := protocol.EdgeProbe{Type: protocol.TypeEdgeProbe, RequestID: reqID, Port: port}
	if err := p.Sender.SendJSON(deviceID, msg); err != nil {
		return Probe{Reachable: false, Error: err.Error(), At: time.Now().UTC()}, nil
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Probe{Reachable: false, Error: ctx.Err().Error(), At: time.Now().UTC()}, nil
	case <-timer.C:
		return Probe{Reachable: false, Error: ErrTimeout.Error(), At: time.Now().UTC()}, nil
	case v := <-ch:
		res, ok := v.(protocol.EdgeProbeResult)
		if !ok {
			return Probe{}, fmt.Errorf("unexpected probe reply")
		}
		return Probe{Reachable: res.OK, Error: res.Error, LatencyMS: res.LatencyMS, At: time.Now().UTC()}, nil
	}
}

func (p *Proxy) RememberProbe(serviceID string, pr Probe) {
	p.mu.Lock()
	p.probes[serviceID] = pr
	p.mu.Unlock()
}

func (p *Proxy) LastProbe(serviceID string) (Probe, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pr, ok := p.probes[serviceID]
	return pr, ok
}

func (p *Proxy) Health(ctx context.Context, svc *store.Service, probe bool) Health {
	h := Health{
		Registered: true,
		Listen:     fmt.Sprintf("%s://%s:%d", svc.Protocol, svc.Bind, svc.Port),
		Hostnames:  []string{},
	}
	if p.Sender != nil {
		h.AgentOnline = p.Sender.IsOnline(svc.DeviceID)
		h.TunnelConnected = h.AgentOnline
	}
	routes, _ := p.Store.ListEdgeRoutesByService(ctx, svc.UserID, svc.ID)
	for i := range routes {
		h.Hostnames = append(h.Hostnames, routes[i].Hostname)
		if h.EdgeDeviceID == "" && routes[i].EdgeDeviceID != "" {
			h.EdgeDeviceID = routes[i].EdgeDeviceID
			h.EdgeDeviceName = routes[i].EdgeName
		}
	}
	if h.EdgeDeviceID != "" && p.Sender != nil {
		h.EdgeOnline = p.Sender.IsOnline(h.EdgeDeviceID)
	} else {
		h.EdgeOnline = true
	}
	if probe && h.TunnelConnected && (svc.Protocol == "http" || svc.Protocol == "https" || svc.Protocol == "tcp") {
		pr, err := p.Probe(ctx, svc.DeviceID, svc.Port)
		if err != nil {
			h.Error = err.Error()
		} else {
			p.RememberProbe(svc.ID, pr)
			h.BackendReachable = pr.Reachable
			if pr.Error != "" {
				h.Error = pr.Error
			}
		}
	} else if cached, ok := p.LastProbe(svc.ID); ok {
		h.BackendReachable = cached.Reachable && h.TunnelConnected
		if !h.TunnelConnected {
			h.BackendReachable = false
			h.Error = ErrOffline.Error()
		} else if cached.Error != "" {
			h.Error = cached.Error
		}
	} else if !h.TunnelConnected {
		h.Error = ErrOffline.Error()
	}
	return h
}

func NormalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimSuffix(host, ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}

func flattenHeaders(h http.Header, extra []protocol.HeaderKV) []protocol.HeaderKV {
	var out []protocol.HeaderKV
	for k, vals := range h {
		if skipHop(k) || strings.EqualFold(k, "Host") {
			continue
		}
		for _, v := range vals {
			out = append(out, protocol.HeaderKV{Name: k, Value: v})
		}
	}
	return append(out, extra...)
}

func skipHop(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailers", "transfer-encoding", "upgrade", "content-length":
		return true
	default:
		return false
	}
}

func forwardedProto(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return p
	}
	return "http"
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
