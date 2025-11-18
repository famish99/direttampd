package mpd

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/famish99/direttampd/internal/player"
)

// Server implements MPD protocol server
type Server struct {
	mu       sync.Mutex
	listener net.Listener
	player   *player.Player
	addr     string
	running  bool
}

// NewServer creates a new MPD protocol server
func NewServer(addr string, p *player.Player) *Server {
	return &Server{
		addr:   addr,
		player: p,
	}
}

// Start starts the MPD server
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to start MPD server: %w", err)
	}

	s.listener = listener
	s.running = true

	log.Printf("MPD server listening on %s", s.addr)

	go s.acceptLoop()

	return nil
}

// Stop stops the MPD server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// acceptLoop accepts incoming connections
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			running := s.running
			s.mu.Unlock()
			if !running {
				return
			}
			log.Printf("Accept error: %v", err)
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single MPD client connection
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	log.Printf("New MPD client connected: %s", conn.RemoteAddr())

	// Send MPD greeting
	fmt.Fprintf(conn, "OK MPD 0.23.0\n")

	scanner := bufio.NewScanner(conn)
	inCommandList := false
	commandListOk := false // Track if we need list_OK after each command
	var commandListResponses strings.Builder

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		log.Printf("MPD command: %s", line)

		// Handle command list mode
		if line == "command_list_begin" {
			inCommandList = true
			commandListOk = false
			commandListResponses.Reset()
			continue
		}

		if line == "command_list_ok_begin" {
			inCommandList = true
			commandListOk = true
			commandListResponses.Reset()
			continue
		}

		if line == "command_list_end" {
			if inCommandList {
				// Send all buffered responses
				fmt.Fprint(conn, commandListResponses.String())
				fmt.Fprint(conn, "OK\n")
				inCommandList = false
				commandListOk = false
				commandListResponses.Reset()
			}
			continue
		}

		// Process command
		response := s.handleCommand(line)

		if inCommandList {
			// Buffer response (strip the final OK)
			if strings.HasSuffix(response, "OK\n") {
				response = strings.TrimSuffix(response, "OK\n")
			}
			commandListResponses.WriteString(response)

			// For command_list_ok_begin, add list_OK after each command
			if commandListOk {
				commandListResponses.WriteString("list_OK\n")
			}
		} else {
			// Send response immediately
			fmt.Fprint(conn, response)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Connection error: %v", err)
	}

	log.Printf("MPD client disconnected: %s", conn.RemoteAddr())
}

// handleCommand processes a single MPD command
func (s *Server) handleCommand(line string) string {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "OK\n"
	}

	command := strings.ToLower(parts[0])
	args := parts[1:]

	switch command {
	case "ping":
		return "OK\n"

	case "add":
		return s.cmdAdd(args)

	case "play":
		return s.cmdPlay(args)

	case "pause":
		return s.cmdPause(args)

	case "stop":
		return s.cmdStop(args)

	case "next":
		return s.cmdNext(args)

	case "previous":
		return s.cmdPrevious(args)

	case "status":
		return s.cmdStatus(args)

	case "playlistinfo":
		return s.cmdPlaylistInfo(args)

	case "clear":
		return s.cmdClear(args)

	case "currentsong":
		return s.cmdCurrentSong(args)

	case "close":
		return "" // Client will close connection

	case "idle":
		// Simple idle implementation - just return OK immediately
		// Real implementation would block until state changes
		return "changed: playlist\nOK\n"

	default:
		return fmt.Sprintf("ACK [5@0] {%s} unknown command\n", command)
	}
}

// cmdAdd handles the 'add' command
func (s *Server) cmdAdd(args []string) string {
	if len(args) == 0 {
		return "ACK [2@0] {add} missing URI\n"
	}

	uri := strings.Join(args, " ")

	// Strip surrounding quotes if present (MPD protocol supports quoted strings)
	if len(uri) >= 2 && uri[0] == '"' && uri[len(uri)-1] == '"' {
		uri = uri[1 : len(uri)-1]
	}

	s.player.AddURLs([]string{uri})

	return "OK\n"
}

// cmdPlay handles the 'play' command
func (s *Server) cmdPlay(args []string) string {
	if err := s.player.Play(); err != nil {
		return fmt.Sprintf("ACK [50@0] {play} %s\n", err.Error())
	}
	return "OK\n"
}

// cmdPause handles the 'pause' command
func (s *Server) cmdPause(args []string) string {
	if err := s.player.Pause(); err != nil {
		return fmt.Sprintf("ACK [50@0] {pause} %s\n", err.Error())
	}
	return "OK\n"
}

// cmdStop handles the 'stop' command
func (s *Server) cmdStop(args []string) string {
	if err := s.player.Stop(); err != nil {
		return fmt.Sprintf("ACK [50@0] {stop} %s\n", err.Error())
	}
	return "OK\n"
}

// cmdNext handles the 'next' command
func (s *Server) cmdNext(args []string) string {
	// Player will automatically advance to next track
	// This is a simplified implementation
	return "OK\n"
}

// cmdPrevious handles the 'previous' command
func (s *Server) cmdPrevious(args []string) string {
	// Would need to implement in player
	return "OK\n"
}

// cmdStatus handles the 'status' command
func (s *Server) cmdStatus(args []string) string {
	pl := s.player.GetPlaylist()

	var status strings.Builder
	status.WriteString("volume: 100\n")
	status.WriteString("repeat: 0\n")
	status.WriteString("random: 0\n")
	status.WriteString("single: 0\n")
	status.WriteString("consume: 0\n")
	status.WriteString(fmt.Sprintf("playlist: %d\n", pl.Length()))
	status.WriteString(fmt.Sprintf("playlistlength: %d\n", pl.Length()))

	// Get actual playback state
	state := s.player.GetState()
	var stateStr string
	switch state {
	case player.StatePlaying:
		stateStr = "play"
	case player.StatePaused:
		stateStr = "pause"
	case player.StateStopped:
		stateStr = "stop"
	default:
		stateStr = "stop"
	}
	status.WriteString(fmt.Sprintf("state: %s\n", stateStr))

	status.WriteString(fmt.Sprintf("song: %d\n", pl.CurrentIndex()))
	status.WriteString(fmt.Sprintf("songid: %d\n", pl.CurrentIndex()))

	// Add timing information if available
	timing := s.player.GetPlaybackTiming()
	if timing != nil {
		// Add legacy "time" field for compatibility (format: elapsed:total)
		status.WriteString(fmt.Sprintf("time: %d:%d\n", int(timing.Elapsed), int(timing.Duration)))

		// MPD protocol uses "elapsed" for current position and "duration" for total length
		status.WriteString(fmt.Sprintf("elapsed: %d\n", int(timing.Elapsed)))
		status.WriteString(fmt.Sprintf("duration: %d\n", int(timing.Duration)))
	}

	status.WriteString("OK\n")

	return status.String()
}

// cmdPlaylistInfo handles the 'playlistinfo' command
func (s *Server) cmdPlaylistInfo(args []string) string {
	pl := s.player.GetPlaylist()
	tracks := pl.GetAll()

	var info strings.Builder
	for i, track := range tracks {
		info.WriteString(fmt.Sprintf("file: %s\n", track.URL))
		info.WriteString(fmt.Sprintf("Pos: %d\n", i))
		info.WriteString(fmt.Sprintf("Id: %d\n", i))
		if track.Title != "" {
			info.WriteString(fmt.Sprintf("Title: %s\n", track.Title))
		}
	}
	info.WriteString("OK\n")

	return info.String()
}

// cmdClear handles the 'clear' command
func (s *Server) cmdClear(args []string) string {
	s.player.GetPlaylist().Clear()
	return "OK\n"
}

// cmdCurrentSong handles the 'currentsong' command
func (s *Server) cmdCurrentSong(args []string) string {
	track, err := s.player.GetPlaylist().Current()
	if err != nil {
		return "OK\n" // No current song
	}

	var info strings.Builder
	info.WriteString(fmt.Sprintf("file: %s\n", track.URL))
	info.WriteString(fmt.Sprintf("Pos: %d\n", track.Index))
	info.WriteString(fmt.Sprintf("Id: %d\n", track.Index))
	if track.Title != "" {
		info.WriteString(fmt.Sprintf("Title: %s\n", track.Title))
	}
	info.WriteString("OK\n")

	return info.String()
}
