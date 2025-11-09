package memoryplay

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Target represents a MemoryPlay audio output target
type Target struct {
	IP        string
	Port      string // Port for MemoryPlayHost connection (default: 19640)
	Interface string
	Name      string
}

// Client manages connection to a MemoryPlayHost
type Client struct {
	target     *Target
	conn       net.Conn
	mu         sync.Mutex
	connected  bool
	reader     *bufio.Reader
	writer     *bufio.Writer

	// Audio format currently being streamed
	currentFormat *FormatID

	// Callbacks
	onStatus func(status string)
}

// NewClient creates a new MemoryPlay client
func NewClient(target *Target) *Client {
	return &Client{
		target: target,
	}
}

// Connect establishes connection to the MemoryPlayHost
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Connect using TCP/IPv6
	// Default port 19640 is the MemoryPlayHost control port
	port := "19640"
	if c.target.Port != "" {
		port = c.target.Port
	}
	addr := fmt.Sprintf("[%s]:%s", c.target.IP, port)
	conn, err := net.DialTimeout("tcp6", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = bufio.NewWriter(conn)
	c.connected = true

	// Send initial connect command
	msg := NewFrameMessage()
	msg.AddHeader(HeaderConnect, fmt.Sprintf("%s %s", c.target.IP, c.target.Interface))

	return c.sendFrameMessage(msg)
}

// Disconnect closes the connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	c.connected = false
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// sendFrameMessage sends a control message (must hold lock)
func (c *Client) sendFrameMessage(msg *FrameMessage) error {
	if !c.connected {
		return fmt.Errorf("not connected")
	}

	data := msg.Encode()
	n, err := c.writer.Write(data)
	if err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	if n != len(data) {
		return io.ErrShortWrite
	}

	// Flush to ensure data is sent immediately
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("flush failed: %w", err)
	}

	return nil
}

// SendFrameMessage sends a control message (public version)
func (c *Client) SendFrameMessage(msg *FrameMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	data := msg.Encode()
	n, err := c.writer.Write(data)
	if err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	if n != len(data) {
		return io.ErrShortWrite
	}

	// Flush to ensure data is sent immediately
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("flush failed: %w", err)
	}

	return nil
}

// SendAudioData sends audio data with format header
func (c *Client) SendAudioData(format *FormatID, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	// Update current format if changed
	c.currentFormat = format

	msg := &AudioDataMessage{
		Format: format,
		Data:   data,
	}

	encoded := msg.Encode()
	n, err := c.writer.Write(encoded)
	if err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	if n != len(encoded) {
		return io.ErrShortWrite
	}

	// For audio data, we may want to flush periodically rather than every packet
	// But for now, flush to ensure delivery
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("flush failed: %w", err)
	}

	return nil
}

// Play sends play command
func (c *Client) Play() error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderPlay, "")
	return c.SendFrameMessage(msg)
}

// Pause sends pause command
func (c *Client) Pause() error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderPause, "")
	return c.SendFrameMessage(msg)
}

// Seek sends seek command
func (c *Client) Seek(position string) error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderSeek, position)
	return c.SendFrameMessage(msg)
}

// SendTag sends track metadata
func (c *Client) SendTag(index int, timestamp int64, title string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	// Tag messages use type 2 (Tag) with data payload containing the title string
	tagMsg := &TagMessage{
		Data: []byte(title),
	}

	encoded := tagMsg.Encode()
	n, err := c.writer.Write(encoded)
	if err != nil {
		return fmt.Errorf("write failed: %w", err)
	}
	if n != len(encoded) {
		return io.ErrShortWrite
	}

	// Flush to ensure data is sent immediately
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("flush failed: %w", err)
	}

	return nil
}

// SendSilence sends silence buffer for synchronization
func (c *Client) SendSilence(format *FormatID, durationSec int) error {
	// Calculate buffer size for silence
	bytesPerSample := format.BitsPerSample / 8
	samplesPerSec := format.SampleRate
	bufferSize := int(bytesPerSample * samplesPerSec * format.Channels) * durationSec

	silence := make([]byte, bufferSize)
	// PCM silence is zeros

	return c.SendAudioData(format, silence)
}

// RequestStatus requests current playback status
func (c *Client) RequestStatus() error {
	msg := NewFrameMessage()
	msg.AddHeader(HeaderRequest, RequestStatus)
	return c.SendFrameMessage(msg)
}

// ReadResponse reads a response from the host (non-blocking)
func (c *Client) ReadResponse() (*FrameMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected")
	}

	return ParseFrameMessage(c.reader)
}

// SetStatusCallback sets callback for status updates
func (c *Client) SetStatusCallback(fn func(status string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onStatus = fn
}

// GetTargetList requests and returns the list of available targets from the host
// The host sends multiple TargetList messages, one per target, in format: "IP IFNO NAME"
func (c *Client) GetTargetList(timeout time.Duration) ([]string, error) {
	// Send request
	msg := NewFrameMessage()
	msg.AddHeader(HeaderRequest, RequestTargetList)
	if err := c.SendFrameMessage(msg); err != nil {
		return nil, fmt.Errorf("failed to send target list request: %w", err)
	}

	// Read multiple responses with timeout
	// The host sends one TargetList message per available target
	type result struct {
		msg *FrameMessage
		err error
	}

	var targets []string
	deadline := time.Now().Add(timeout)
	readTimeout := 500 * time.Millisecond // Short timeout between messages

	for {
		responseChan := make(chan result, 1)
		go func() {
			msg, err := c.ReadResponse()
			responseChan <- result{msg: msg, err: err}
		}()

		remaining := time.Until(deadline)
		if remaining < 0 {
			if len(targets) == 0 {
				return nil, fmt.Errorf("timeout waiting for target list response")
			}
			break
		}

		select {
		case res := <-responseChan:
			if res.err != nil {
				// If we already got some targets, return them
				if len(targets) > 0 {
					return targets, nil
				}
				return nil, fmt.Errorf("failed to read target list response: %w", res.err)
			}

			// Check if this is a TargetList message by looking in the Headers map
			if value, ok := res.msg.Headers[HeaderTargetList]; ok {
				// Parse format: "IP_ADDRESS INTERFACE_NO NAME"
				// Replace first space with % for display format: "IP%IFNO NAME"
				value = strings.TrimSpace(value)
				if value != "" {
					parts := strings.SplitN(value, " ", 3)
					if len(parts) >= 3 {
						// Format as "IP%IFNO NAME" to match reference implementation
						formatted := fmt.Sprintf("%s%%%s %s", parts[0], parts[1], parts[2])
						targets = append(targets, formatted)
					} else {
						// Fallback to original format if parsing fails
						targets = append(targets, value)
					}
				}
				// Continue reading more TargetList messages
				readTimeout = 500 * time.Millisecond
				continue
			} else {
				// Non-TargetList message means we're done
				return targets, nil
			}

		case <-time.After(readTimeout):
			// No more messages - return what we got
			if len(targets) == 0 {
				return nil, fmt.Errorf("timeout waiting for target list response")
			}
			return targets, nil
		}
	}

	return targets, nil
}
