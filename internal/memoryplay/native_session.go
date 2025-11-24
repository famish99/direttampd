package memoryplay

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NativeSession implements a MemoryPlay control session using native Go TCP
type NativeSession struct {
	conn      net.Conn
	reader    *bufio.Reader
	mu        sync.Mutex
	connected bool
}

// CreateNativeSession creates a new control session to a MemoryPlay host
// hostAddress should be in the format "IP,PORT" (e.g., "::1,34133")
// interfaceNumber specifies the network interface to use (0 for default)
func CreateNativeSession(hostAddress string, interfaceNumber uint32) (*NativeSession, error) {
	// Parse the address format: IP,PORT
	// Example: "::1,34133"

	// Split IP and PORT
	lastComma := strings.LastIndex(hostAddress, ",")
	if lastComma == -1 {
		return nil, fmt.Errorf("invalid host address format: expected IP,PORT, got %s", hostAddress)
	}

	ipAddr := hostAddress[:lastComma]
	portStr := hostAddress[lastComma+1:]

	// Build the TCP address
	// For IPv6 with scope, format is: [ipv6%interface]:port
	var addr string
	if interfaceNumber != 0 {
		addr = fmt.Sprintf("[%s%%%d]:%s", ipAddr, interfaceNumber, portStr)
	} else {
		addr = fmt.Sprintf("[%s]:%s", ipAddr, portStr)
	}

	// Connect to the MemoryPlay host
	conn, err := net.DialTimeout("tcp6", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	return &NativeSession{
		conn:      conn,
		reader:    bufio.NewReader(conn),
		connected: true,
	}, nil
}

// Close closes the session and releases resources
func (s *NativeSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

// sendCommand sends a command frame message to the host
func (s *NativeSession) sendCommand(msg *FrameMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return fmt.Errorf("session not connected")
	}

	encoded := msg.Encode()
	_, err := s.conn.Write(encoded)
	return err
}

// receiveMessages receives and processes messages until the handler returns true or timeout occurs
// Similar to receiveMessages in C++ lib_memory_play_controller.cpp:32
func (s *NativeSession) receiveMessages(handler func(key, value string) bool, timeoutMs int) error {
	if timeoutMs == 0 {
		timeoutMs = 500 // Default timeout from C++ implementation
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	lastRecv := time.Now()

	for {
		// Set read timeout
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("timeout waiting for response")
		}

		s.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		// Try to read a message
		msg, err := ParseFrameMessage(s.reader)
		if err != nil {
			// Check if it's a timeout error (need to unwrap to find net.Error)
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				// Check if we've exceeded the total timeout
				if time.Since(lastRecv) >= time.Duration(timeoutMs)*time.Millisecond {
					return fmt.Errorf("timeout waiting for response")
				}
				continue
			}
			return fmt.Errorf("connection error: %w", err)
		}

		// Successfully received a message
		lastRecv = time.Now()

		// Process all headers in the message
		for key, value := range msg.Headers {
			if handler(key, value) {
				return nil
			}
		}
	}
}

// ConnectTarget connects to a specific Diretta target device
// targetAddress should be "IP,PORT" format
func (s *NativeSession) ConnectTarget(targetAddress string, interfaceNumber uint32) error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderConnect, fmt.Sprintf("%s %d", targetAddress, interfaceNumber))

	return s.sendCommand(msg)
}

// Play starts or resumes playback
func (s *NativeSession) Play() error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderPlay, "")

	return s.sendCommand(msg)
}

// Pause pauses playback
func (s *NativeSession) Pause() error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderPause, "")

	return s.sendCommand(msg)
}

// Seek seeks forward or backward by seconds
// Positive values seek forward, negative values seek backward
func (s *NativeSession) Seek(offsetSeconds int64) error {
	msg := NewFrameMessage()

	var seekValue string
	if offsetSeconds > 0 {
		seekValue = fmt.Sprintf("+%d", offsetSeconds)
	} else {
		seekValue = fmt.Sprintf("%d", offsetSeconds)
	}

	msg.AddHeader(HeaderSeek, seekValue)

	return s.sendCommand(msg)
}

// SeekToStart seeks to the beginning of the playlist
func (s *NativeSession) SeekToStart() error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderSeek, SeekFront)

	return s.sendCommand(msg)
}

// SeekAbsolute seeks to an absolute position in seconds
func (s *NativeSession) SeekAbsolute(positionSeconds int64) error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderSeek, fmt.Sprintf("%d", positionSeconds))

	return s.sendCommand(msg)
}

// Quit stops playback and disconnects from the target
func (s *NativeSession) Quit() error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderSeek, SeekQuit)

	return s.sendCommand(msg)
}

// GetPlayStatus returns the current playback status
func (s *NativeSession) GetPlayStatus() (PlaybackStatus, error) {
	// Request status from host
	msg := NewFrameMessage()
	msg.AddHeader(HeaderRequest, RequestStatus)

	if err := s.sendCommand(msg); err != nil {
		return StatusDisconnected, err
	}

	// Process status response
	var status PlaybackStatus = StatusDisconnected
	handler := func(key, value string) bool {
		if key == HeaderStatus {
			switch value {
			case StatusDisconnect:
				status = StatusDisconnected
			case StatusPlay:
				status = StatusPlaying
			case StatusPause:
				status = StatusPaused
			}
			return true
		}
		return false
	}

	if err := s.receiveMessages(handler, 500); err != nil {
		return StatusDisconnected, err
	}

	return status, nil
}

// GetCurrentTime returns the current playback time in seconds
func (s *NativeSession) GetCurrentTime() (int64, error) {
	// Request status from host
	msg := NewFrameMessage()
	msg.AddHeader(HeaderRequest, RequestStatus)

	if err := s.sendCommand(msg); err != nil {
		return -1, err
	}

	// Process time response
	var timeSeconds int64 = -1
	handler := func(key, value string) bool {
		if key == HeaderStatus {
			if value == StatusDisconnect || value == StatusPause {
				return true
			}
		}
		if key == HeaderLastTime {
			// Parse the time value
			if t, err := strconv.ParseInt(value, 10, 64); err == nil {
				timeSeconds = t
			}
			return true
		}
		return false
	}

	// Use longer timeout for time queries
	if err := s.receiveMessages(handler, 1500); err != nil {
		return -1, err
	}

	return timeSeconds, nil
}

// GetTagList returns the list of tags from the current playlist
func (s *NativeSession) GetTagList() ([]TagInfo, error) {
	// Request status from host
	msg := NewFrameMessage()
	msg.AddHeader(HeaderRequest, RequestStatus)

	if err := s.sendCommand(msg); err != nil {
		return nil, err
	}

	// Process tag responses
	var tags []TagInfo
	handler := func(key, value string) bool {
		if key == HeaderTag {
			tags = append(tags, TagInfo{Tag: value})
			return false // Continue collecting tags
		}
		return true // Stop on other messages
	}

	if err := s.receiveMessages(handler, 500); err != nil {
		return nil, err
	}

	return tags, nil
}