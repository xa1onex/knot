package transport

import (
	"context"
	"io"
)

const (
	PathDirect = "direct"
	PathRelay  = "relay"
)

// Session is a bidirectional byte stream between two agents for one transfer.
type Session interface {
	io.ReadWriteCloser
	Path() string
}

// Dialer opens a data session to a peer. Implementations: Direct (QUIC) or nil (use relay).
type Dialer interface {
	// TryDirect attempts a P2P session. Returns nil, nil if direct is unavailable.
	TryDirect(ctx context.Context, params DialParams) (Session, error)
}

type DialParams struct {
	TransferID     string
	LocalDeviceID  string
	PeerDeviceID   string
	LocalPrivKey   []byte // ed25519 private
	LocalPubKey    []byte
	PeerPubKey     []byte // expected peer ed25519 public
	Role           string // source | dest
	ForceRelay     bool
	STUNURLs       []string
	SignalOutbound func(msg any) error
	// CandidateInbox receives peer candidates during negotiation.
	CandidateInbox <-chan Candidate
}

type Candidate struct {
	TransferID string `json:"transfer_id"`
	DeviceID   string `json:"device_id"`
	Addr       string `json:"addr"` // host:port
	Kind       string `json:"kind"` // host | srflx
}
