package edge

import (
	"context"
	"errors"
	"sync"

	"github.com/knot-infra/knot/pkg/protocol"
)

type streamSession struct {
	proxy     *Proxy
	requestID string
	deviceID  string
	serviceID string
	ctx       context.Context

	mu      sync.Mutex
	events  chan any
	closed  bool
	failErr error
}

func newStreamSession(p *Proxy, requestID, deviceID, serviceID string) *streamSession {
	return &streamSession{
		proxy:     p,
		requestID: requestID,
		deviceID:  deviceID,
		serviceID: serviceID,
		events:    make(chan any, MaxInflightChunks*2+4),
	}
}

func (s *streamSession) deliver(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.events <- v
}

func (s *streamSession) fail(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.failErr = err
	s.mu.Unlock()
	select {
	case s.events <- protocol.EdgeHTTPFail{Type: protocol.TypeEdgeHTTPFail, RequestID: s.requestID, Error: err.Error()}:
	default:
	}
	close(s.events)
}

func (s *streamSession) waitHead(ctx context.Context) (any, error) {
	for {
		raw, err := s.recv(ctx)
		if err != nil {
			return nil, err
		}
		switch msg := raw.(type) {
		case protocol.EdgeHTTPRespHead:
			return msg, nil
		case protocol.EdgeHTTPFail:
			return nil, errors.New(msg.Error)
		}
	}
}

func (s *streamSession) waitBody(ctx context.Context) (any, error) {
	return s.recv(ctx)
}

func (s *streamSession) waitReqAck(ctx context.Context, seq int) error {
	for {
		raw, err := s.recv(ctx)
		if err != nil {
			return err
		}
		switch msg := raw.(type) {
		case protocol.EdgeHTTPAck:
			if msg.Direction == "req" && msg.Seq == seq {
				return nil
			}
		case protocol.EdgeHTTPFail:
			return errors.New(msg.Error)
		}
	}
}

func (s *streamSession) recv(ctx context.Context) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case raw, ok := <-s.events:
		if !ok {
			s.mu.Lock()
			err := s.failErr
			s.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return nil, ErrStreamAborted
		}
		return raw, nil
	}
}
