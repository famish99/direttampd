package slimproto

import (
	"bufio"
	"crypto/sha1"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PlaybackState mirrors player.PlaybackState so slimproto does not depend on
// the player package (which transitively requires the CGo Diretta SDK).
type PlaybackState int

const (
	StateStopped PlaybackState = iota
	StatePlaying
	StatePaused
)

// PlaybackTiming mirrors player.PlaybackTiming. Seconds everywhere.
type PlaybackTiming struct {
	Elapsed   int64
	Duration  int64
	Remaining int64
}

// Controller is the slice of *player.Player that slimproto uses. An adapter
// in cmd/direttampd wraps the real player to satisfy it.
type Controller interface {
	ClearPlaylist()
	AddURLs(urls []string)
	Play() error
	Pause() error
	Resume() error
	Stop() error
	GetState() PlaybackState
	GetPlaybackTiming() *PlaybackTiming
}

const (
	defaultControlPort = "3483"
	discoveryTimeout   = 3 * time.Second
	statInterval       = time.Second
)

// Client is a Slimproto TCP client that presents direttampd to LMS as a
// Squeezebox-compatible player and drives the local Controller in response
// to LMS commands.
type Client struct {
	player Controller
	server string
	name   string
	mac    [6]byte

	conn net.Conn
	bw   *bufio.Writer

	writeMu sync.Mutex

	stopCh    chan struct{}
	stoppedCh chan struct{}

	// streamActive is true between STRM-start and either STRM-flush, STRM-quit,
	// or natural end-of-stream. It gates sending spurious STMd/STMu frames when
	// the player stops for reasons not driven by LMS.
	streamActive atomic.Bool

	// stageDir is the on-disk directory into which LMS HTTP streams are fetched
	// before handing them to player.Player.
	stageDir string

	// startTime is set when Start() succeeds; elapsed time since start is used
	// for the STAT "jiffies" field.
	startTime time.Time

	// bytesReceived is the running total of audio bytes staged from LMS (for
	// STAT reporting).
	bytesReceived atomic.Uint64

	// serverTimestamp is echoed back to LMS in STAT frames when the server
	// sends a timed 't' command.
	serverTimestamp atomic.Uint32
}

// NewClient builds a Slimproto client. If server is empty the LMS server is
// auto-discovered over UDP broadcast. mac may be empty; a stable MAC is then
// derived from the hostname.
func NewClient(p Controller, server, name, mac string) (*Client, error) {
	if p == nil {
		return nil, fmt.Errorf("slimproto: player is required")
	}

	resolved := server
	if resolved == "" {
		addr, err := DiscoverServer(discoveryTimeout)
		if err != nil {
			return nil, fmt.Errorf("auto-discover LMS: %w", err)
		}
		log.Printf("slimproto: discovered LMS at %s", addr)
		resolved = addr
	} else if !strings.Contains(resolved, ":") {
		resolved = net.JoinHostPort(resolved, defaultControlPort)
	}

	macBytes, err := parseOrDeriveMAC(mac)
	if err != nil {
		return nil, err
	}

	return &Client{
		player:    p,
		server:    resolved,
		name:      name,
		mac:       macBytes,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
		stageDir:  filepath.Join(os.TempDir(), "direttampd-slimproto"),
	}, nil
}

// Name returns the player name registered with LMS.
func (c *Client) Name() string { return c.name }

// Server returns the resolved LMS server address (host:port).
func (c *Client) Server() string { return c.server }

// Start dials LMS, sends HELO, and launches the read loop and STAT ticker.
// It returns once the connection is established; subsequent protocol errors
// are logged and will terminate the background goroutines.
func (c *Client) Start() error {
	conn, err := net.DialTimeout("tcp", c.server, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial LMS: %w", err)
	}
	c.conn = conn
	c.bw = bufio.NewWriter(conn)
	c.startTime = time.Now()

	if err := c.sendHELO(); err != nil {
		conn.Close()
		return fmt.Errorf("send HELO: %w", err)
	}
	log.Printf("slimproto: sent HELO as %q (%02x:%02x:%02x:%02x:%02x:%02x) to %s",
		c.name, c.mac[0], c.mac[1], c.mac[2], c.mac[3], c.mac[4], c.mac[5], c.server)

	go c.readLoop()
	go c.statLoop()
	return nil
}

// Stop tears down the client. Safe to call multiple times.
func (c *Client) Stop() {
	select {
	case <-c.stopCh:
		return
	default:
	}
	close(c.stopCh)
	if c.conn != nil {
		// Best-effort BYE! before closing.
		_ = c.writeFrame(OpBYE, []byte{0})
		_ = c.conn.Close()
	}
	<-c.stoppedCh
}

func (c *Client) sendHELO() error {
	hello := HelloInfo{
		DeviceID:     12, // "Squeezeplay" class — software player
		Revision:     1,
		MAC:          c.mac,
		Language:     [2]byte{'e', 'n'},
		Capabilities: "Model=direttampd,ModelName=direttampd,flc,mp3,aac,ogg,pcm",
	}
	return c.writeFrame(OpHELO, EncodeHELO(hello))
}

func (c *Client) writeFrame(op Op, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.bw == nil {
		return fmt.Errorf("slimproto: not connected")
	}
	if err := WriteClientFrame(c.bw, op, payload); err != nil {
		return err
	}
	return c.bw.Flush()
}

func (c *Client) readLoop() {
	defer close(c.stoppedCh)

	br := bufio.NewReader(c.conn)
	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		frame, err := ReadServerFrame(br)
		if err != nil {
			log.Printf("slimproto: read error: %v", err)
			return
		}

		switch frame.Op {
		case OpStrm:
			c.handleStrm(frame.Payload)
		case OpAudg, OpAude, OpSetd, OpServ, OpVers:
			log.Printf("slimproto: ignoring %s frame (%d bytes)", string(frame.Op[:]), len(frame.Payload))
		default:
			log.Printf("slimproto: unknown op %q (%d bytes)", string(frame.Op[:]), len(frame.Payload))
		}
	}
}

func (c *Client) handleStrm(payload []byte) {
	cmd, err := DecodeStrm(payload)
	if err != nil {
		log.Printf("slimproto: decode strm: %v", err)
		return
	}

	switch cmd.Command {
	case 's': // start
		c.handleStrmStart(cmd)
	case 'p': // pause
		log.Printf("slimproto: pause")
		if err := c.player.Pause(); err != nil {
			log.Printf("slimproto: player.Pause: %v", err)
		}
		c.sendSimpleStat(StatSTMp)
	case 'u': // unpause / resume
		log.Printf("slimproto: resume")
		if err := c.player.Resume(); err != nil {
			log.Printf("slimproto: player.Resume: %v", err)
		}
		c.sendSimpleStat(StatSTMr)
	case 'q': // quit/stop
		log.Printf("slimproto: quit")
		c.streamActive.Store(false)
		if err := c.player.Stop(); err != nil {
			log.Printf("slimproto: player.Stop: %v", err)
		}
		c.sendSimpleStat(StatSTMf)
	case 'f': // flush
		log.Printf("slimproto: flush")
		c.streamActive.Store(false)
		if err := c.player.Stop(); err != nil {
			log.Printf("slimproto: player.Stop: %v", err)
		}
		c.sendSimpleStat(StatSTMf)
	case 't': // timestamp echo
		c.serverTimestamp.Store(cmd.ReplayGain)
		c.sendSimpleStat(StatSTMt)
	case 'a': // skip-ahead (seek) – we simply restart the stream; LMS will send the new strm
		log.Printf("slimproto: skip-ahead")
	default:
		log.Printf("slimproto: unhandled strm cmd %q", cmd.Command)
	}
}

func (c *Client) handleStrmStart(cmd StrmCmd) {
	serverHost, _, err := net.SplitHostPort(c.server)
	if err != nil {
		serverHost = c.server
	}
	port := cmd.ServerPort
	if port == 0 {
		port = 9000
	}
	path := cmd.HTTPRequestPath()
	url := fmt.Sprintf("http://%s:%d%s", serverHost, port, path)

	log.Printf("slimproto: STRM-start %s (format=%c)", url, printableOrDot(cmd.FormatCode))

	c.sendSimpleStat(StatSTMc)

	// Stage synchronously: LMS will happily wait for STMs while we download the
	// body, and the existing player pipeline wants a local file anyway.
	base := strings.ReplaceAll(strings.Trim(path, "/"), "/", "_")
	if base == "" {
		base = fmt.Sprintf("stream-%d", time.Now().UnixNano())
	}
	// Append a format-friendly extension so ffprobe can detect the codec.
	if ext := formatExtension(cmd.FormatCode); ext != "" && !strings.HasSuffix(base, ext) {
		base += ext
	}

	res, err := stageToFile(url, c.stageDir, base)
	if err != nil {
		log.Printf("slimproto: stage: %v", err)
		c.sendSimpleStat(StatSTMn)
		return
	}
	c.bytesReceived.Add(uint64(res.BytesRead))
	c.sendSimpleStat(StatSTMh)
	c.sendSimpleStat(StatSTMe)

	// Clear the playlist and queue exactly the new track.
	c.player.ClearPlaylist()
	c.player.AddURLs([]string{"file://" + res.Path})
	c.streamActive.Store(true)

	if err := c.player.Play(); err != nil {
		log.Printf("slimproto: player.Play: %v", err)
		c.sendSimpleStat(StatSTMn)
		c.streamActive.Store(false)
		return
	}

	c.sendSimpleStat(StatSTMs)
}

// statLoop periodically emits STMt heartbeats with current elapsed time, and
// watches for natural end-of-track so it can tell LMS to move on.
func (c *Client) statLoop() {
	ticker := time.NewTicker(statInterval)
	defer ticker.Stop()

	var lastState PlaybackState
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
		}

		state := c.player.GetState()

		// Detect natural end-of-track: previously Playing, now Stopped, and we
		// were expecting to be playing.
		if lastState == StatePlaying && state == StateStopped && c.streamActive.Load() {
			log.Printf("slimproto: track ended naturally, notifying LMS")
			c.streamActive.Store(false)
			c.sendSimpleStat(StatSTMd)
			c.sendSimpleStat(StatSTMu)
		}
		lastState = state

		if state == StatePlaying || state == StatePaused {
			c.sendSimpleStat(StatSTMt)
		}
	}
}

func (c *Client) sendSimpleStat(event StatEvent) {
	timing := c.player.GetPlaybackTiming()
	var elapsedSec, elapsedMs uint32
	if timing != nil {
		elapsedSec = uint32(timing.Elapsed)
		elapsedMs = uint32(timing.Elapsed * 1000)
	}
	stat := StatInfo{
		Event:           event,
		BytesReceived:   c.bytesReceived.Load(),
		JiffiesMs:       uint32(time.Since(c.startTime) / time.Millisecond),
		ElapsedSeconds:  elapsedSec,
		ElapsedMs:       elapsedMs,
		ServerTimestamp: c.serverTimestamp.Load(),
	}
	if err := c.writeFrame(OpSTAT, EncodeSTAT(stat)); err != nil {
		log.Printf("slimproto: send STAT %s: %v", string(event[:]), err)
	}
}

// parseOrDeriveMAC returns a 6-byte MAC. If s is empty, a stable MAC is
// derived from the hostname (local-admin bit set).
func parseOrDeriveMAC(s string) ([6]byte, error) {
	var out [6]byte
	if s == "" {
		host, err := os.Hostname()
		if err != nil {
			host = "direttampd"
		}
		sum := sha1.Sum([]byte(host))
		copy(out[:], sum[:6])
		out[0] = (out[0] | 0x02) & 0xFE // locally administered, unicast
		return out, nil
	}

	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return out, fmt.Errorf("slimproto: invalid MAC %q: need 6 colon-separated octets", s)
	}
	for i, p := range parts {
		if len(p) == 0 || len(p) > 2 {
			return out, fmt.Errorf("slimproto: invalid MAC octet %q", p)
		}
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil {
			return out, fmt.Errorf("slimproto: invalid MAC octet %q: %w", p, err)
		}
		out[i] = byte(v)
	}
	return out, nil
}

func formatExtension(code byte) string {
	switch code {
	case 'f':
		return ".flac"
	case 'm':
		return ".mp3"
	case 'a':
		return ".aac"
	case 'o':
		return ".ogg"
	case 'p':
		return ".pcm"
	default:
		return ""
	}
}

func printableOrDot(b byte) rune {
	if b >= 0x20 && b < 0x7f {
		return rune(b)
	}
	return '.'
}

