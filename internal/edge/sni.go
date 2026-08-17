package edge

import (
	"encoding/binary"
	"io"
)

// readTLSClientHello reads the first TLS record and extracts the SNI hostname.
// Returns the full first record bytes so they can be replayed to the origin.
func readTLSClientHello(r io.Reader) (sni string, record []byte, err error) {
	hdr := make([]byte, 5)
	if _, err = io.ReadFull(r, hdr); err != nil {
		return "", nil, err
	}
	if hdr[0] != 0x16 { // Handshake
		return "", hdr, errNotClientHello
	}
	recLen := int(binary.BigEndian.Uint16(hdr[3:5]))
	if recLen <= 0 || recLen > 1<<14 {
		return "", hdr, errNotClientHello
	}
	body := make([]byte, recLen)
	if _, err = io.ReadFull(r, body); err != nil {
		return "", append(hdr, body...), err
	}
	record = append(hdr, body...)
	sni, err = parseSNIFromClientHello(body)
	return sni, record, err
}

var errNotClientHello = io.ErrUnexpectedEOF

func parseSNIFromClientHello(body []byte) (string, error) {
	// body = handshake message(s); first should be ClientHello (type 1)
	if len(body) < 4 || body[0] != 1 {
		return "", errNotClientHello
	}
	hsLen := int(body[1])<<16 | int(body[2])<<8 | int(body[3])
	if hsLen+4 > len(body) {
		return "", errNotClientHello
	}
	p := body[4:]
	if len(p) < 2+32+1 {
		return "", errNotClientHello
	}
	p = p[2+32:] // version + random
	sessLen := int(p[0])
	p = p[1:]
	if len(p) < sessLen+1 {
		return "", errNotClientHello
	}
	p = p[sessLen:]
	cipherLen := int(binary.BigEndian.Uint16(p))
	p = p[2:]
	if len(p) < cipherLen+1 {
		return "", errNotClientHello
	}
	p = p[cipherLen:]
	compLen := int(p[0])
	p = p[1:]
	if len(p) < compLen+2 {
		return "", errNotClientHello
	}
	p = p[compLen:]
	extLen := int(binary.BigEndian.Uint16(p))
	p = p[2:]
	if len(p) < extLen {
		return "", errNotClientHello
	}
	ext := p[:extLen]
	for len(ext) >= 4 {
		typ := binary.BigEndian.Uint16(ext[0:2])
		el := int(binary.BigEndian.Uint16(ext[2:4]))
		ext = ext[4:]
		if len(ext) < el {
			break
		}
		val := ext[:el]
		ext = ext[el:]
		if typ != 0 { // server_name
			continue
		}
		if len(val) < 2 {
			continue
		}
		listLen := int(binary.BigEndian.Uint16(val[0:2]))
		v := val[2:]
		if listLen > len(v) {
			continue
		}
		v = v[:listLen]
		for len(v) >= 3 {
			nameType := v[0]
			nl := int(binary.BigEndian.Uint16(v[1:3]))
			v = v[3:]
			if len(v) < nl {
				break
			}
			name := string(v[:nl])
			if nameType == 0 && name != "" {
				return name, nil
			}
			v = v[nl:]
		}
	}
	return "", errNotClientHello
}
