package slimproto

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// HelloInfo is the client identity sent in a HELO frame.
type HelloInfo struct {
	DeviceID     uint8  // LMS device class; 12 = "Squeezeplay", used by software players
	Revision     uint8  // Firmware revision (informational)
	MAC          [6]byte
	UUID         [16]byte // Optional, zero is acceptable
	WLANChannels uint16   // Unused for wired players; 0
	BytesRecv    uint64   // 0 on connect
	Language     [2]byte  // e.g. 'e','n'
	Capabilities string   // Comma-separated capability list
}

// EncodeHELO returns the payload of a HELO frame ready to pass to
// WriteClientFrame together with OpHELO.
func EncodeHELO(h HelloInfo) []byte {
	var buf bytes.Buffer
	buf.WriteByte(h.DeviceID)
	buf.WriteByte(h.Revision)
	buf.Write(h.MAC[:])
	buf.Write(h.UUID[:])
	_ = binary.Write(&buf, binary.BigEndian, h.WLANChannels)
	_ = binary.Write(&buf, binary.BigEndian, h.BytesRecv)
	buf.Write(h.Language[:])
	buf.WriteString(h.Capabilities)
	return buf.Bytes()
}

// StatEvent is the 4-ASCII-byte event code that identifies a STAT frame's
// meaning to LMS.
type StatEvent [4]byte

// STAT event codes per the public Slimproto documentation.
var (
	StatSTMa = StatEvent{'S', 'T', 'M', 'a'} // autostart: audio available in buffer
	StatSTMc = StatEvent{'S', 'T', 'M', 'c'} // connect
	StatSTMd = StatEvent{'S', 'T', 'M', 'd'} // decoder ready / needs next track
	StatSTMe = StatEvent{'S', 'T', 'M', 'e'} // connection established
	StatSTMf = StatEvent{'S', 'T', 'M', 'f'} // connection flushed
	StatSTMh = StatEvent{'S', 'T', 'M', 'h'} // end-of-headers
	StatSTMl = StatEvent{'S', 'T', 'M', 'l'} // buffer threshold reached
	StatSTMo = StatEvent{'S', 'T', 'M', 'o'} // output buffer underrun
	StatSTMp = StatEvent{'S', 'T', 'M', 'p'} // paused
	StatSTMr = StatEvent{'S', 'T', 'M', 'r'} // resumed
	StatSTMs = StatEvent{'S', 'T', 'M', 's'} // track started
	StatSTMt = StatEvent{'S', 'T', 'M', 't'} // timer / heartbeat
	StatSTMu = StatEvent{'S', 'T', 'M', 'u'} // underrun / end of data
	StatSTMn = StatEvent{'S', 'T', 'M', 'n'} // decoding error
)

// StatInfo is the payload of a STAT frame.
// Field sizes and ordering follow the documented 53-byte structure.
type StatInfo struct {
	Event            StatEvent
	CRLFs            uint8  // number of consecutive CRLFs parsed
	MASInitialized   uint8  // 'm' or 'p'
	MASMode          uint8  // 0
	BufferSize       uint32 // decode buffer size
	BufferFullness   uint32 // bytes queued in decode buffer
	BytesReceived    uint64 // total bytes received on stream
	SignalStrength   uint16 // 0-100, 0 for wired
	JiffiesMs        uint32 // ms timestamp
	OutputSize       uint32 // output buffer size
	OutputFullness   uint32 // bytes queued in output buffer
	ElapsedSeconds   uint32 // playback elapsed, seconds
	VoltageMv        uint16 // battery millivolts or 0
	ElapsedMs        uint32 // playback elapsed, milliseconds
	ServerTimestamp  uint32 // echoed from server 't' command
	ErrorCode        uint16 // 0 ok
}

// EncodeSTAT returns the payload of a STAT frame.
func EncodeSTAT(s StatInfo) []byte {
	buf := make([]byte, 0, 53)
	buf = append(buf, s.Event[:]...)
	buf = append(buf, s.CRLFs, s.MASInitialized, s.MASMode)
	buf = binary.BigEndian.AppendUint32(buf, s.BufferSize)
	buf = binary.BigEndian.AppendUint32(buf, s.BufferFullness)
	buf = binary.BigEndian.AppendUint64(buf, s.BytesReceived)
	buf = binary.BigEndian.AppendUint16(buf, s.SignalStrength)
	buf = binary.BigEndian.AppendUint32(buf, s.JiffiesMs)
	buf = binary.BigEndian.AppendUint32(buf, s.OutputSize)
	buf = binary.BigEndian.AppendUint32(buf, s.OutputFullness)
	buf = binary.BigEndian.AppendUint32(buf, s.ElapsedSeconds)
	buf = binary.BigEndian.AppendUint16(buf, s.VoltageMv)
	buf = binary.BigEndian.AppendUint32(buf, s.ElapsedMs)
	buf = binary.BigEndian.AppendUint32(buf, s.ServerTimestamp)
	buf = binary.BigEndian.AppendUint16(buf, s.ErrorCode)
	return buf
}

// StrmCmd is a decoded server 'strm' frame.
type StrmCmd struct {
	Command        byte   // 's','p','u','q','t','f','a','0'..'4'
	Autostart      byte   // '0','1','2','3'
	FormatCode     byte   // 'p' pcm, 'f' flac, 'm' mp3, 'o' ogg, 'a' aac
	PCMSampleSize  byte
	PCMSampleRate  byte
	PCMChannels    byte
	PCMEndianness  byte
	Threshold      uint8  // buffer threshold (KB)
	SpdifEnable    byte
	TransitionPer  uint8
	TransitionType byte
	Flags          byte
	OutputThresh   uint8
	Reserved       byte
	ReplayGain     uint32
	ServerPort     uint16
	ServerIP       uint32 // 0 = same as control server
	HTTPRequest    []byte // verbatim HTTP request bytes LMS wants us to send
}

// DecodeStrm parses a 'strm' payload.
// Returns an error if the payload is shorter than the fixed 24-byte header.
func DecodeStrm(payload []byte) (StrmCmd, error) {
	if len(payload) < 24 {
		return StrmCmd{}, fmt.Errorf("slimproto: strm payload too short (%d < 24)", len(payload))
	}
	c := StrmCmd{
		Command:        payload[0],
		Autostart:      payload[1],
		FormatCode:     payload[2],
		PCMSampleSize:  payload[3],
		PCMSampleRate:  payload[4],
		PCMChannels:    payload[5],
		PCMEndianness:  payload[6],
		Threshold:      payload[7],
		SpdifEnable:    payload[8],
		TransitionPer:  payload[9],
		TransitionType: payload[10],
		Flags:          payload[11],
		OutputThresh:   payload[12],
		Reserved:       payload[13],
		ReplayGain:     binary.BigEndian.Uint32(payload[14:18]),
		ServerPort:     binary.BigEndian.Uint16(payload[18:20]),
		ServerIP:       binary.BigEndian.Uint32(payload[20:24]),
	}
	if len(payload) > 24 {
		c.HTTPRequest = append([]byte(nil), payload[24:]...)
	}
	return c, nil
}

// HTTPRequestPath extracts the request path from the verbatim HTTP request
// bytes LMS embedded in the strm payload, e.g. "/stream.mp3?player=xx".
// Returns "/" if the request line cannot be parsed.
func (c StrmCmd) HTTPRequestPath() string {
	if len(c.HTTPRequest) == 0 {
		return "/"
	}
	line := c.HTTPRequest
	if idx := bytes.IndexByte(line, '\r'); idx >= 0 {
		line = line[:idx]
	} else if idx := bytes.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	parts := strings.SplitN(string(line), " ", 3)
	if len(parts) < 2 {
		return "/"
	}
	return parts[1]
}
