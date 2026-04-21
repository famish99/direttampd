// Package slimproto implements the client side of the Lyrion/Squeezebox
// "Slimproto" control protocol, enough to register direttampd as an LMS player
// and receive start/stop/pause/seek commands. It is a clean-room Go
// implementation based on public documentation (wiki.lyrion.org) and does not
// reference any GPL source from Squeezelite.
package slimproto

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Op is a 4-byte operation code exchanged on the Slimproto TCP connection.
// Server -> client op codes ("strm", "audg", ...) and
// client -> server op codes ("HELO", "STAT", "RESP", "BYE!", ...) both use
// 4 ASCII bytes, but the framing differs: server frames prefix the op with a
// 2-byte big-endian length, client frames prefix the length *after* the op.
type Op [4]byte

// Server-to-client op codes that we handle or explicitly ignore.
var (
	OpStrm = Op{'s', 't', 'r', 'm'}
	OpAudg = Op{'a', 'u', 'd', 'g'}
	OpAude = Op{'a', 'u', 'd', 'e'}
	OpSetd = Op{'s', 'e', 't', 'd'}
	OpServ = Op{'s', 'e', 'r', 'v'}
	OpVers = Op{'v', 'e', 'r', 's'}
)

// Client-to-server op codes we emit.
var (
	OpHELO = Op{'H', 'E', 'L', 'O'}
	OpSTAT = Op{'S', 'T', 'A', 'T'}
	OpRESP = Op{'R', 'E', 'S', 'P'}
	OpBYE  = Op{'B', 'Y', 'E', '!'}
	OpMETA = Op{'M', 'E', 'T', 'A'}
)

// ServerFrame is a frame received from LMS.
type ServerFrame struct {
	Op      Op
	Payload []byte
}

// ReadServerFrame reads a single server->client frame from r.
// Server frames are: [2-byte BE length][4-byte op][payload...]
// where the length value includes the 4-byte op (so payload = length - 4).
func ReadServerFrame(r io.Reader) (ServerFrame, error) {
	var header [6]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return ServerFrame{}, err
	}
	length := binary.BigEndian.Uint16(header[0:2])
	if length < 4 {
		return ServerFrame{}, fmt.Errorf("slimproto: server frame length %d < 4", length)
	}
	var op Op
	copy(op[:], header[2:6])

	payload := make([]byte, int(length)-4)
	if _, err := io.ReadFull(r, payload); err != nil {
		return ServerFrame{}, err
	}
	return ServerFrame{Op: op, Payload: payload}, nil
}

// WriteClientFrame writes a single client->server frame to w.
// Client frames are: [4-byte op][4-byte BE length][payload...]
// where length is the payload length (not including op or length field).
func WriteClientFrame(w io.Writer, op Op, payload []byte) error {
	var header [8]byte
	copy(header[0:4], op[:])
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}
