package direct

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/network/stunx"
	"github.com/knot-infra/knot/internal/network/transport"
	"github.com/quic-go/quic-go"
)

type session struct {
	stream *quic.Stream
	conn   *quic.Conn
	udp    *net.UDPConn
	path   string
	once   sync.Once
}

func (s *session) Read(p []byte) (int, error)  { return s.stream.Read(p) }
func (s *session) Write(p []byte) (int, error) { return s.stream.Write(p) }
func (s *session) Path() string                { return s.path }
func (s *session) Close() error {
	s.once.Do(func() {
		_ = s.stream.Close()
		if s.conn != nil {
			_ = s.conn.CloseWithError(0, "done")
		}
		if s.udp != nil {
			_ = s.udp.Close()
		}
	})
	return nil
}

// Dialer implements transport.Dialer using QUIC + STUN hole punching.
type Dialer struct {
	Timeout time.Duration
}

func (d *Dialer) TryDirect(ctx context.Context, p transport.DialParams) (transport.Session, error) {
	if p.ForceRelay {
		return nil, nil
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	hosts, srflx, udp, err := stunx.GatherHostAndSRFLX(p.STUNURLs)
	if err != nil {
		return nil, err
	}

	for _, a := range hosts {
		_ = p.SignalOutbound(transport.Candidate{
			TransferID: p.TransferID, DeviceID: p.LocalDeviceID, Addr: a, Kind: "host",
		})
	}
	for _, a := range srflx {
		_ = p.SignalOutbound(transport.Candidate{
			TransferID: p.TransferID, DeviceID: p.LocalDeviceID, Addr: a, Kind: "srflx",
		})
	}

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"knot-direct"},
	}

	tr := &quic.Transport{Conn: udp}
	chosenCh := make(chan transport.Session, 1)

	go func() {
		ln, err := tr.Listen(tlsConf, nil)
		if err != nil {
			return
		}
		defer ln.Close()
		for {
			qconn, err := ln.Accept(ctx)
			if err != nil {
				return
			}
			stream, err := qconn.AcceptStream(ctx)
			if err != nil {
				_ = qconn.CloseWithError(1, "no stream")
				continue
			}
			if err := mutualAuth(stream, p); err != nil {
				_ = stream.Close()
				_ = qconn.CloseWithError(1, "auth failed")
				continue
			}
			sess := &session{stream: stream, conn: qconn, udp: udp, path: transport.PathDirect}
			select {
			case chosenCh <- sess:
			default:
				_ = sess.Close()
			}
			return
		}
	}()

	tryDial := func(addr string) {
		raddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return
		}
		qconn, err := tr.Dial(ctx, raddr, tlsConf, nil)
		if err != nil {
			return
		}
		stream, err := qconn.OpenStreamSync(ctx)
		if err != nil {
			_ = qconn.CloseWithError(1, "stream")
			return
		}
		if err := mutualAuth(stream, p); err != nil {
			_ = stream.Close()
			_ = qconn.CloseWithError(1, "auth failed")
			return
		}
		sess := &session{stream: stream, conn: qconn, udp: udp, path: transport.PathDirect}
		select {
		case chosenCh <- sess:
		default:
			_ = sess.Close()
		}
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case c, ok := <-p.CandidateInbox:
				if !ok {
					return
				}
				if c.TransferID != p.TransferID || c.DeviceID == p.LocalDeviceID {
					continue
				}
				go tryDial(c.Addr)
			}
		}
	}()

	select {
	case <-ctx.Done():
		_ = udp.Close()
		return nil, nil
	case sess := <-chosenCh:
		return sess, nil
	}
}

func mutualAuth(stream *quic.Stream, p transport.DialParams) error {
	if len(p.LocalPrivKey) != ed25519.PrivateKeySize || len(p.PeerPubKey) != ed25519.PublicKeySize {
		return errors.New("invalid keys")
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	out := make([]byte, 64)
	copy(out[:32], p.LocalPubKey)
	copy(out[32:], nonce)
	if _, err := stream.Write(out); err != nil {
		return err
	}
	in := make([]byte, 64)
	if _, err := io.ReadFull(stream, in); err != nil {
		return err
	}
	peerPub := in[:32]
	peerNonce := in[32:]
	if !bytesEqual(peerPub, p.PeerPubKey) {
		return fmt.Errorf("peer identity mismatch")
	}
	sig := ed25519.Sign(ed25519.PrivateKey(p.LocalPrivKey), peerNonce)
	if _, err := stream.Write(sig); err != nil {
		return err
	}
	peerSig := make([]byte, ed25519.SignatureSize)
	if _, err := io.ReadFull(stream, peerSig); err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(peerPub), nonce, peerSig) {
		return errors.New("peer signature invalid")
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func WriteFilePayload(w io.Writer, transferID string, size int64, sha256hex string, r io.Reader) error {
	idb := []byte(transferID)
	hb := []byte(sha256hex)
	hdr := make([]byte, 2+len(idb)+8+2+len(hb))
	binary.BigEndian.PutUint16(hdr[0:2], uint16(len(idb)))
	copy(hdr[2:], idb)
	off := 2 + len(idb)
	binary.BigEndian.PutUint64(hdr[off:off+8], uint64(size))
	off += 8
	binary.BigEndian.PutUint16(hdr[off:off+2], uint16(len(hb)))
	off += 2
	copy(hdr[off:], hb)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := io.Copy(w, r)
	return err
}

func ReadFilePayload(r io.Reader) (transferID string, size int64, sha256hex string, body io.Reader, err error) {
	var idLen uint16
	if err = binary.Read(r, binary.BigEndian, &idLen); err != nil {
		return
	}
	idb := make([]byte, idLen)
	if _, err = io.ReadFull(r, idb); err != nil {
		return
	}
	transferID = string(idb)
	if err = binary.Read(r, binary.BigEndian, &size); err != nil {
		return
	}
	var hLen uint16
	if err = binary.Read(r, binary.BigEndian, &hLen); err != nil {
		return
	}
	hb := make([]byte, hLen)
	if _, err = io.ReadFull(r, hb); err != nil {
		return
	}
	sha256hex = string(hb)
	body = io.LimitReader(r, size)
	return
}
