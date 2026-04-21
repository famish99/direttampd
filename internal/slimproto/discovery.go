package slimproto

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// DiscoverServer broadcasts an LMS discovery probe on UDP 3483 and returns
// the address of the first server that responds. Response parsing follows the
// documented format: packet starts with 'E' and is followed by TLV sections
// where the 'IPAD' tag (when present) carries the server IP, and the JSON
// RPC port defaults to 9000 / control port to 3483.
//
// The returned server string is in "host:port" form with control port 3483,
// suitable for net.Dial("tcp", ...).
func DiscoverServer(timeout time.Duration) (string, error) {
	addr, err := net.ResolveUDPAddr("udp4", "255.255.255.255:3483")
	if err != nil {
		return "", fmt.Errorf("resolve broadcast addr: %w", err)
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return "", fmt.Errorf("open udp socket: %w", err)
	}
	defer conn.Close()

	// Enable SO_BROADCAST via setting (Go's ListenUDP sets it implicitly on Linux).
	// Discovery probe: byte 'e' followed by a NAME request tag.
	probe := []byte("eIPAD\x00NAME\x00JSON\x00")

	if _, err := conn.WriteTo(probe, addr); err != nil {
		return "", fmt.Errorf("send discovery probe: %w", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return "", fmt.Errorf("set deadline: %w", err)
	}

	buf := make([]byte, 1500)
	n, src, err := conn.ReadFromUDP(buf)
	if err != nil {
		var nerr net.Error
		if errors.As(err, &nerr) && nerr.Timeout() {
			return "", fmt.Errorf("no LMS server responded within %s", timeout)
		}
		return "", fmt.Errorf("read discovery reply: %w", err)
	}
	if n < 1 || (buf[0] != 'E' && buf[0] != 'e') {
		return "", fmt.Errorf("unexpected discovery reply from %s", src.String())
	}

	// We use the source IP of the reply as the server address; the control
	// port is always 3483.
	return net.JoinHostPort(src.IP.String(), "3483"), nil
}
