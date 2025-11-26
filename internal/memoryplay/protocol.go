package memoryplay

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// Message types (SendMessageType in C++)
const (
	MessageTypeData    = 0 // Audio data
	MessageTypeCommand = 1 // Control commands (Headers in C++)
	MessageTypeTag     = 2 // Tag/metadata
)

// Header sizes
const (
	PayloadHeaderSize = 9 // 3-byte length + 1-byte type + 1-byte flags + 4-byte identifier
	DataHeaderSize    = 1 // 1-byte pad
	MessageHeaderSize = 6 // 1-byte pad + 4-byte dependency + 1-byte weight (HeadersHeader in C++)
)

// Control command headers (Client → Host)
const (
	HeaderRequest = "Request"
	HeaderConnect = "Connect"
	HeaderSeek    = "Seek"
	HeaderPlay    = "Play"
	HeaderPause   = "Pause"

	// Request values
	RequestTargetList = "TargetList"
	RequestStatus     = "Status"

	// Seek values
	SeekFront = "Front"
	SeekQuit  = "Quit"
)

// Format constants for audio encoding
const (
	FormatPCM = 0 // PCM audio format
)

// FormatID represents audio format specification
type FormatID struct {
	SampleRate    uint32 // Sample rate in Hz
	BitsPerSample uint32 // Bits per sample (8, 16, 24, 32)
	Channels      uint32 // Number of channels
	Format        uint32 // Format type (FormatPCM, etc.)
}

// Response headers (Host → Client)
const (
	HeaderStatus   = "Status"
	HeaderLastTime = "LastTime"
	HeaderTag      = "Tag"

	// Status values
	StatusPlay       = "Play"
	StatusPause      = "Pause"
	StatusDisconnect = "Disconnect"
)

// PayloadHeader is the frame header for all messages (9 bytes)
type PayloadHeader struct {
	Length     uint32 // 24-bit length (only lower 24 bits used)
	Type       uint8  // Message type: 0=Data, 1=Command, 2=Tag
	Flags      uint8  // Flags
	Identifier uint32 // Message identifier
}

// Encode serializes PayloadHeader to wire format (9 bytes, big-endian)
func (h *PayloadHeader) Encode() []byte {
	buf := make([]byte, PayloadHeaderSize)
	// 3-byte length (big-endian)
	buf[0] = byte((h.Length >> 16) & 0xFF)
	buf[1] = byte((h.Length >> 8) & 0xFF)
	buf[2] = byte(h.Length & 0xFF)
	// 1-byte type
	buf[3] = h.Type
	// 1-byte flags
	buf[4] = h.Flags
	// 4-byte identifier (big-endian)
	binary.BigEndian.PutUint32(buf[5:9], h.Identifier)
	return buf
}

// DecodePayloadHeader reads a PayloadHeader from bytes
func DecodePayloadHeader(data []byte) (*PayloadHeader, error) {
	if len(data) < PayloadHeaderSize {
		return nil, fmt.Errorf("insufficient data for payload header")
	}
	return &PayloadHeader{
		Length:     uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2]),
		Type:       data[3],
		Flags:      data[4],
		Identifier: binary.BigEndian.Uint32(data[5:9]),
	}, nil
}

// DataHeader is the sub-header for data messages (1 byte)
type DataHeader struct {
	Pad uint8
}

// MessageHeader is the sub-header for command messages (6 bytes)
// Called HeadersHeader in C++, CommandHeader in original Go code
type MessageHeader struct {
	Pad        uint8
	Dependency uint32
	Weight     uint8
}

// Encode serializes MessageHeader to wire format (6 bytes, big-endian)
func (h *MessageHeader) Encode() []byte {
	buf := make([]byte, MessageHeaderSize)
	buf[0] = h.Pad
	binary.BigEndian.PutUint32(buf[1:5], h.Dependency)
	buf[5] = h.Weight
	return buf
}

// FrameMessage represents a command message with key=value pairs
type FrameMessage struct {
	Headers map[string]string
}

// NewFrameMessage creates a new command frame message
func NewFrameMessage() *FrameMessage {
	return &FrameMessage{
		Headers: make(map[string]string),
	}
}

// AddHeader adds a key=value pair to the message
func (msg *FrameMessage) AddHeader(key, value string) {
	msg.Headers[key] = value
}

// Encode serializes FrameMessage to wire format with frame wrapper
// Format: PayloadHeader + MessageHeader + "key=value\r\n" pairs
func (msg *FrameMessage) Encode() []byte {
	// Build the payload (key=value\r\n pairs)
	var payload bytes.Buffer
	for key, value := range msg.Headers {
		payload.WriteString(key)
		payload.WriteByte('=')
		payload.WriteString(value)
		payload.WriteString("\r\n")
	}

	// Create message header
	msgHeader := &MessageHeader{
		Pad:        0,
		Dependency: 0,
		Weight:     0,
	}
	msgHeaderBytes := msgHeader.Encode()

	// Calculate total payload length (message header + key=value pairs)
	payloadLength := uint32(len(msgHeaderBytes) + payload.Len())

	// Create payload header
	frameHeader := &PayloadHeader{
		Length:     payloadLength,
		Type:       MessageTypeCommand,
		Flags:      0,
		Identifier: 0,
	}
	frameHeaderBytes := frameHeader.Encode()

	// Combine all parts
	result := make([]byte, 0, len(frameHeaderBytes)+len(msgHeaderBytes)+payload.Len())
	result = append(result, frameHeaderBytes...)
	result = append(result, msgHeaderBytes...)
	result = append(result, payload.Bytes()...)

	return result
}

// EncodeFormatID serializes FormatID to binary wire format (16 bytes, little-endian for Diretta)
func EncodeFormatID(format *FormatID) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], format.SampleRate)
	binary.LittleEndian.PutUint32(buf[4:8], format.BitsPerSample)
	binary.LittleEndian.PutUint32(buf[8:12], format.Channels)
	binary.LittleEndian.PutUint32(buf[12:16], format.Format)
	return buf
}

// AudioDataMessage wraps FormatID + audio payload for transmission
type AudioDataMessage struct {
	Format *FormatID
	Data   []byte
}

// Encode serializes AudioDataMessage to wire format with frame wrapper
// Format: PayloadHeader + DataHeader + FormatID + audio data
func (msg *AudioDataMessage) Encode() []byte {
	// Encode format ID
	formatBytes := EncodeFormatID(msg.Format)

	// Create data header (1 byte pad)
	dataHeader := []byte{0} // pad = 0

	// Calculate total payload length (data header + format + audio)
	payloadLength := uint32(DataHeaderSize + len(formatBytes) + len(msg.Data))

	// Create payload header
	frameHeader := &PayloadHeader{
		Length:     payloadLength,
		Type:       MessageTypeData,
		Flags:      0,
		Identifier: 0,
	}
	frameHeaderBytes := frameHeader.Encode()

	// Combine all parts
	result := make([]byte, 0, len(frameHeaderBytes)+DataHeaderSize+len(formatBytes)+len(msg.Data))
	result = append(result, frameHeaderBytes...)
	result = append(result, dataHeader...)
	result = append(result, formatBytes...)
	result = append(result, msg.Data...)

	return result
}

// TagMessage wraps tag/metadata for transmission
type TagMessage struct {
	Data []byte
}

// Encode serializes TagMessage to wire format with frame wrapper
func (msg *TagMessage) Encode() []byte {
	// Create data header (1 byte pad)
	dataHeader := []byte{0} // pad = 0

	// Calculate total payload length (data header + tag data)
	payloadLength := uint32(DataHeaderSize + len(msg.Data))

	// Create payload header
	frameHeader := &PayloadHeader{
		Length:     payloadLength,
		Type:       MessageTypeTag,
		Flags:      0,
		Identifier: 0,
	}
	frameHeaderBytes := frameHeader.Encode()

	// Combine all parts
	result := make([]byte, 0, len(frameHeaderBytes)+DataHeaderSize+len(msg.Data))
	result = append(result, frameHeaderBytes...)
	result = append(result, dataHeader...)
	result = append(result, msg.Data...)

	return result
}

// ParseFrameMessage reads a framed message from a reader
func ParseFrameMessage(r *bufio.Reader) (*FrameMessage, error) {
	// Read payload header (9 bytes)
	headerBuf := make([]byte, PayloadHeaderSize)
	if _, err := io.ReadFull(r, headerBuf); err != nil {
		return nil, fmt.Errorf("failed to read payload header: %w", err)
	}

	header, err := DecodePayloadHeader(headerBuf)
	if err != nil {
		return nil, err
	}

	// Read the payload
	payloadBuf := make([]byte, header.Length)
	if _, err := io.ReadFull(r, payloadBuf); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	// Only parse command messages (type 1)
	if header.Type != MessageTypeCommand {
		return nil, fmt.Errorf("unexpected message type: %d (expected command type 1)", header.Type)
	}

	// Skip message header (6 bytes)
	if len(payloadBuf) < MessageHeaderSize {
		return nil, fmt.Errorf("payload too short for message header")
	}
	payloadData := payloadBuf[MessageHeaderSize:]

	// Parse key=value\r\n pairs
	msg := NewFrameMessage()
	var key, value strings.Builder
	inValue := false

	for i := 0; i < len(payloadData); i++ {
		c := payloadData[i]

		if c == '\r' || c == '\n' {
			// End of key=value pair
			if key.Len() > 0 {
				msg.AddHeader(key.String(), value.String())
			}
			key.Reset()
			value.Reset()
			inValue = false
			continue
		}

		if !inValue {
			// Reading key
			if c == '=' {
				inValue = true
			} else {
				key.WriteByte(c)
			}
		} else {
			// Reading value
			value.WriteByte(c)
		}
	}

	// Handle last pair if no trailing newline
	if key.Len() > 0 {
		msg.AddHeader(key.String(), value.String())
	}

	return msg, nil
}

// Get returns the value for a given header key
func (msg *FrameMessage) Get(key string) (string, bool) {
	val, ok := msg.Headers[key]
	return val, ok
}
