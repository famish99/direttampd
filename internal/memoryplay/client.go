package memoryplay

import (
	"fmt"
	"sync"
)

// Target represents a MemoryPlay audio output target
type Target struct {
	IP        string
	Port      string
	Interface string
	Name      string
}

// ControlSession defines the interface for MemoryPlay session control
type ControlSession interface {
	Close()
	ConnectTarget(targetAddress string, interfaceNumber uint32) error
	Play() error
	Pause() error
	Seek(offsetSeconds int64) error
	SeekAbsolute(positionSeconds int64) error
	Quit() error
	GetPlayStatus() (PlaybackStatus, error)
	GetCurrentTime() (int64, error)
	GetTagList() ([]TagInfo, error)
}

// Client manages connection to a MemoryPlayHost
type Client struct {
	hostIP    string  // MemoryPlay host IP to connect to
	target    *Target // Target device for audio output
	mu        sync.Mutex
	connected bool

	// Session handle (either CGo or native implementation)
	session   ControlSession
	useNative bool

	// Callbacks
	onStatus func(status string)
}

// NewClient creates a new MemoryPlay client
// hostIP specifies the MemoryPlay host to connect to
//   - For CGo implementation: IP address (port auto-discovered by C library)
//   - For native implementation: "IP,PORT%INTERFACE_NUM" format (e.g., "::1,34133%0")
// target specifies the audio output device the host should use
// useNative specifies whether to use native Go implementation (true) or CGo (false)
func NewClient(hostIP string, target *Target, useNative bool) *Client {
	return &Client{
		hostIP:    hostIP,
		target:    target,
		useNative: useNative,
	}
}

// Connect establishes connection to the MemoryPlayHost and selects target
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Parse interface number from target interface string
	// The interface string is typically a number like "0", "1", etc.
	var interfaceNum uint32
	fmt.Sscanf(c.target.Interface, "%d", &interfaceNum)

	// Create session to the host (either CGo or native)
	var session ControlSession
	var err error

	if c.useNative {
		session, err = CreateNativeSession(c.hostIP, interfaceNum)
	} else {
		session, err = CreateSession(c.hostIP, interfaceNum)
	}

	if err != nil {
		return fmt.Errorf("failed to create session to MemoryPlay host: %w", err)
	}

	c.session = session
	c.connected = true

	return nil
}

// SelectTarget connects the session to the audio output target
// Should be called AFTER uploading audio data to trigger playback
func (c *Client) SelectTarget() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return fmt.Errorf("not connected")
	}

	var interfaceNum uint32
	fmt.Sscanf(c.target.Interface, "%d", &interfaceNum)

	// ConnectTarget expects "IP,PORT" format
	targetAddr := fmt.Sprintf("%s,%s", c.target.IP, c.target.Port)
	if err := c.session.ConnectTarget(targetAddr, interfaceNum); err != nil {
		return fmt.Errorf("failed to connect to target: %w", err)
	}

	return nil
}

// Disconnect closes the connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	c.connected = false
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
	return nil
}

// Play sends play command
func (c *Client) Play() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return fmt.Errorf("not connected")
	}

	return c.session.Play()
}

// Pause sends pause command
func (c *Client) Pause() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return fmt.Errorf("not connected")
	}

	return c.session.Pause()
}

// sends Quit command
func (c *Client) Quit() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return fmt.Errorf("not connected")
	}

	return c.session.Quit()
}

// Seek sends seek command
func (c *Client) Seek(position string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return fmt.Errorf("not connected")
	}

	// Parse position as seconds offset
	var offsetSeconds int64
	fmt.Sscanf(position, "%d", &offsetSeconds)

	return c.session.Seek(offsetSeconds)
}

// SeekAbsolute seeks to an absolute position in seconds
func (c *Client) SeekAbsolute(positionSeconds int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return fmt.Errorf("not connected")
	}

	return c.session.SeekAbsolute(positionSeconds)
}

// GetPlayStatus returns current playback status
func (c *Client) GetPlayStatus() (PlaybackStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return StatusDisconnected, fmt.Errorf("not connected")
	}

	return c.session.GetPlayStatus()
}

// GetCurrentTime returns current playback time in seconds
func (c *Client) GetCurrentTime() (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return 0, fmt.Errorf("not connected")
	}

	return c.session.GetCurrentTime()
}

// GetTagList returns the list of tags from the playlist
func (c *Client) GetTagList() ([]TagInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.session == nil {
		return nil, fmt.Errorf("not connected")
	}

	return c.session.GetTagList()
}

// SetStatusCallback sets callback for status updates
func (c *Client) SetStatusCallback(fn func(status string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onStatus = fn
}

// GetTargetList requests and returns the list of available targets from the host
func (c *Client) GetTargetList() ([]string, error) {
	c.mu.Lock()
	hostIP := c.hostIP
	c.mu.Unlock()

	var interfaceNum uint32
	if c.target != nil {
		fmt.Sscanf(c.target.Interface, "%d", &interfaceNum)
	}

	targets, err := ListTargets(hostIP, interfaceNum)
	if err != nil {
		return nil, err
	}

	// Convert to string format expected by callers: "IP%IFNO NAME"
	result := make([]string, len(targets))
	for i, t := range targets {
		result[i] = fmt.Sprintf("%s%%%d %s", t.IPAddress, t.InterfaceNumber, t.TargetName)
	}

	return result, nil
}