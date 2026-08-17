package stunx

import (
	"fmt"
	"net"
	"time"

	"github.com/pion/stun/v2"
)

// GatherHostAndSRFLX returns local UDP host candidates and optional STUN srflx.
func GatherHostAndSRFLX(stunURLs []string) (hosts []string, srflx []string, conn *net.UDPConn, err error) {
	conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, nil, nil, err
	}
	laddr := conn.LocalAddr().(*net.UDPAddr)
	port := laddr.Port

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			hosts = append(hosts, net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port)))
		}
	}
	// Always include 127.0.0.1 for same-host / test
	hosts = append(hosts, net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)))

	for _, u := range stunURLs {
		mapped, err := bindingRequest(conn, u)
		if err != nil {
			continue
		}
		srflx = append(srflx, mapped)
	}
	return hosts, srflx, conn, nil
}

func bindingRequest(conn *net.UDPConn, stunURL string) (string, error) {
	host := stunURL
	if len(host) > 5 && (host[:5] == "stun:" || host[:5] == "STUN:") {
		host = host[5:]
	}
	addr, err := net.ResolveUDPAddr("udp4", host)
	if err != nil {
		return "", err
	}
	msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	defer conn.SetDeadline(time.Time{})
	if _, err := conn.WriteTo(msg.Raw, addr); err != nil {
		return "", err
	}
	buf := make([]byte, 1500)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		return "", err
	}
	var res stun.Message
	res.Raw = buf[:n]
	if err := res.Decode(); err != nil {
		return "", err
	}
	var xor stun.XORMappedAddress
	if err := xor.GetFrom(&res); err != nil {
		return "", err
	}
	return net.JoinHostPort(xor.IP.String(), fmt.Sprintf("%d", xor.Port)), nil
}
