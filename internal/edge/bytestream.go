package edge

import (
	"context"
	"errors"
	"sync"

	"github.com/knot-infra/knot/pkg/protocol"
)

type byteStreamSession struct {
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

func newByteStreamSession(p *Proxy, requestID, deviceID, serviceID string) *byteStreamSession {
	return &byteStreamSession{
		proxy:     p,
		requestID: requestID,
		deviceID:  deviceID,
		serviceID: serviceID,
		events:    make(chan any, protocol.MaxEdgeStreamInflight*2+4),
	}
}

func (s *byteStreamSession) deliver(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.events <- v:
	default:
		go func() { s.events <- v }()
	}
}

func (s *byteStreamSession) fail(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.failErr = err
	s.mu.Unlock()
	select {
	case s.events <- protocol.EdgeStreamFail{Type: protocol.TypeEdgeStreamFail, RequestID: s.requestID, Error: err.Error()}:
	default:
	}
	close(s.events)
}

func (s *byteStreamSession) waitReady(ctx context.Context) error {
	raw, err := s.recv(ctx)
	if err != nil {
		return err
	}
	switch msg := raw.(type) {
	case protocol.EdgeStreamReady:
		if !msg.OK {
			if msg.Error != "" {
				return errors.New(msg.Error)
			}
			return ErrUnreachable
		}
		return nil
	case protocol.EdgeStreamFail:
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
		return ErrStreamAborted
	default:
		return errors.New("unexpected stream reply")
	}
}

func (s *byteStreamSession) waitAck(ctx context.Context, direction string, seq int) error {
	for {
		raw, err := s.recv(ctx)
		if err != nil {
			return err
		}
		switch msg := raw.(type) {
		case protocol.EdgeStreamAck:
			if msg.Direction == direction && msg.Seq == seq {
				return nil
			}
			if msg.Direction != direction {
				s.deliver(msg)
			}
		case protocol.EdgeStreamFail:
			if msg.Error != "" {
				return errors.New(msg.Error)
			}
			return ErrStreamAborted
		case protocol.EdgeStreamClose:
			return ErrStreamAborted
		case protocol.EdgeStreamData, protocol.EdgeStreamReady:
			s.deliver(msg)
		}
	}
}

func (s *byteStreamSession) recv(ctx context.Context) (any, error) {
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

func (p *Proxy) deliverByteStream(requestID string, v any) {
	p.mu.Lock()
	s := p.byteStreams[requestID]
	p.mu.Unlock()
	if s == nil {
		return
	}
	s.deliver(v)
}

func (p *Proxy) abortByteStreamsForDevice(deviceID string) {
	p.mu.Lock()
	var abort []*byteStreamSession
	for id, s := range p.byteStreams {
		if s.deviceID == deviceID {
			abort = append(abort, s)
			delete(p.byteStreams, id)
		}
	}
	p.mu.Unlock()
	for _, s := range abort {
		s.fail(ErrOffline)
	}
}
