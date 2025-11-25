package mpd

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/famish99/direttampd/internal/player"
	"github.com/famish99/direttampd/internal/playlist"
)

// metadataFields maps internal tag names to MPD field names
var metadataFields = map[string]string{
	"artist":      "Artist",
	"album":       "Album",
	"albumartist": "AlbumArtist",
	"title":       "Title",
	"track":       "Track",
	"date":        "Date",
	"genre":       "Genre",
	"composer":    "Composer",
	"performer":   "Performer",
	"disc":        "Disc",
}

// decoderInfo represents a decoder plugin with its supported formats
type decoderInfo struct {
	plugin    string
	suffixes  []string
	mimeTypes []string
}

// supportedDecoders lists all audio formats supported via ffmpeg
var supportedDecoders = []decoderInfo{
	{
		plugin:    "flac",
		suffixes:  []string{"flac"},
		mimeTypes: []string{"audio/flac", "audio/x-flac"},
	},
	{
		plugin:    "mp3",
		suffixes:  []string{"mp3", "mp2"},
		mimeTypes: []string{"audio/mpeg"},
	},
	{
		plugin:    "aac",
		suffixes:  []string{"aac", "m4a", "mp4"},
		mimeTypes: []string{"audio/aac", "audio/mp4", "audio/x-m4a"},
	},
	{
		plugin:    "vorbis",
		suffixes:  []string{"ogg", "oga"},
		mimeTypes: []string{"audio/ogg", "audio/vorbis", "application/ogg"},
	},
	{
		plugin:    "opus",
		suffixes:  []string{"opus"},
		mimeTypes: []string{"audio/opus"},
	},
	{
		plugin:    "wav",
		suffixes:  []string{"wav"},
		mimeTypes: []string{"audio/wav", "audio/x-wav"},
	},
	{
		plugin:    "aiff",
		suffixes:  []string{"aiff", "aif"},
		mimeTypes: []string{"audio/aiff", "audio/x-aiff"},
	},
	{
		plugin:    "ape",
		suffixes:  []string{"ape"},
		mimeTypes: []string{"audio/ape", "audio/x-ape"},
	},
	{
		plugin:    "wma",
		suffixes:  []string{"wma"},
		mimeTypes: []string{"audio/x-ms-wma"},
	},
	{
		plugin:    "alac",
		suffixes:  []string{"m4a"},
		mimeTypes: []string{"audio/mp4"},
	},
	{
		plugin:    "dsd",
		suffixes:  []string{"dsf", "dff"},
		mimeTypes: []string{"audio/dsd", "audio/x-dsd"},
	},
}

// Server implements MPD protocol server
type Server struct {
	mu           sync.Mutex
	listener     net.Listener
	player       *player.Player
	addr         string
	running      bool
	enabledTags  map[string]bool // Track which tag types are enabled
	tagTypesMu   sync.RWMutex    // Protects enabledTags

	// Idle connection management
	idleMu      sync.RWMutex
	idleConns   map[*idleConnection]bool
	idleCounter uint64
}

// NewServer creates a new MPD protocol server
func NewServer(addr string, p *player.Player) *Server {
	// Initialize with all tags enabled by default
	enabledTags := map[string]bool{
		"artist":      true,
		"albumartist": true,
		"album":       true,
		"title":       true,
		"track":       true,
		"name":        true,
		"genre":       true,
		"date":        true,
		"composer":    true,
		"performer":   true,
		"disc":        true,
	}

	s := &Server{
		addr:        addr,
		player:      p,
		enabledTags: enabledTags,
		idleConns:   make(map[*idleConnection]bool),
	}

	// Set up player notification callback for idle connections
	p.SetNotifySubsystem(s.NotifySubsystemChange)
	p.Quit()

	return s
}

// idleConnection represents a connection waiting in idle mode
type idleConnection struct {
	subsystems map[string]bool // Subsystems to watch (empty = all)
	notify     chan string     // Channel to send subsystem changes
	cancel     chan struct{}   // Channel to cancel idle wait
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
    s.player.Quit()
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
	fmt.Fprintf(conn, "OK MPD 0.25.0\n")

	// Create connection-specific idle state
	var currentIdle *idleConnection
	var idleMu sync.Mutex

	scanner := bufio.NewScanner(conn)
	inCommandList := false
	commandListOk := false // Track if we need list_OK after each command
	var commandListResponses strings.Builder

	// Cleanup idle connection on disconnect
	defer func() {
		idleMu.Lock()
		if currentIdle != nil {
			s.unregisterIdle(currentIdle)
		}
		idleMu.Unlock()
	}()

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

		// Check for idle/noidle commands which need special handling
		parts := strings.Fields(line)
		var response string

		if len(parts) > 0 {
			cmd := strings.ToLower(parts[0])
			args := parts[1:]

			if cmd == "idle" {
				// Enter idle mode
				idleMu.Lock()
				if currentIdle != nil {
					// Already in idle mode, ignore
					idleMu.Unlock()
					continue
				}

				// Parse subsystems to watch
				subsystems := make(map[string]bool)
				for _, arg := range args {
					subsystems[strings.ToLower(arg)] = true
				}

				// Create idle connection
				idle := &idleConnection{
					subsystems: subsystems,
					notify:     make(chan string, 10),
					cancel:     make(chan struct{}),
				}
				currentIdle = idle
				s.registerIdle(idle)
				idleMu.Unlock()

				// Wait for notification or cancel
				select {
				case subsystem := <-idle.notify:
					response = fmt.Sprintf("changed: %s\nOK\n", subsystem)
				case <-idle.cancel:
					response = "OK\n"
				}

				// Cleanup
				idleMu.Lock()
				s.unregisterIdle(idle)
				currentIdle = nil
				idleMu.Unlock()

			} else if cmd == "noidle" {
				// Exit idle mode
				idleMu.Lock()
				if currentIdle != nil {
					close(currentIdle.cancel)
					// Don't send response here - idle will send it
					idleMu.Unlock()
					continue
				}
				idleMu.Unlock()
				response = "OK\n"

			} else {
				// Normal command processing
				response = s.handleCommand(line)
			}
		} else {
			response = s.handleCommand(line)
		}

		log.Printf(response)

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

	case "addid":
		return s.cmdAddId(args)

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

	case "plchanges":
		return s.cmdPlChanges(args)

	case "tagtypes":
		return s.cmdTagTypes(args)

	case "outputs":
		return s.cmdOutputs(args)

	case "decoders":
		return s.cmdDecoders(args)

	case "single":
		return s.cmdSingle(args)

	case "consume":
		return s.cmdConsume(args)

	case "repeat":
		return s.cmdRepeat(args)

	case "random":
		return s.cmdRandom(args)

	case "close":
		return "" // Client will close connection

	default:
// 		log.Fatalf("Unknown MPD command received: %s (full line: %s)", command, line)
		return fmt.Sprintf("ACK [5@0] {%s} unknown command\n", command)
	}
}

// addTrackToPlaylist adds a track to either the pending or current playlist
// Returns the song ID
func (s *Server) addTrackToPlaylist(uri string, position *int) int {
	// Check if we're in transition
	pending := s.player.GetPendingPlaylist()
	if pending != nil {
		// Add to pending playlist
		pending.AddMultiple([]string{uri})
		s.player.BackgroundCacheTrack(uri)
		log.Printf("Added track to pending playlist: %s", uri)
		return pending.Length() - 1
	}

	// Add to current playlist
	if position != nil {
		return s.player.AddURLAt(uri, *position)
	}

	s.player.AddURLs([]string{uri})
	return s.player.GetPlaylist().Length() - 1
}

// cmdAdd handles the 'add' command
func (s *Server) cmdAdd(args []string) string {
	if len(args) == 0 {
		return "ACK [2@0] {add} missing URI\n"
	}

	uri := strings.Join(args, " ")

	// Try to unquote if it's a quoted string, otherwise use as-is
	if unquoted, err := strconv.Unquote(uri); err == nil {
		uri = unquoted
	}

	s.addTrackToPlaylist(uri, nil)

	// Notify idle connections of playlist change
	s.NotifySubsystemChange("playlist")

	return "OK\n"
}

// cmdPlay handles the 'play' command
// play [POS] - start playback at optional position
func (s *Server) cmdPlay(args []string) string {
	var err error

	// Check if we have a pending playlist to transition to
	if s.player.GetPendingPlaylist() != nil {
		// Complete the transition (cache, swap, start playback)
		log.Printf("Completing playlist transition")
		err = s.player.CompleteTransition()
		if err != nil {
			return fmt.Sprintf("ACK [50@0] {play} transition failed: %s\n", err.Error())
		}
		return "OK\n"
	}

	if len(args) > 0 {
		// Parse position argument
		posArg := args[0]
		if unquoted, parseErr := strconv.Unquote(posArg); parseErr == nil {
			posArg = unquoted
		}

		pos64, parseErr := strconv.ParseInt(posArg, 10, 32)
		if parseErr != nil {
			return "ACK [2@0] {play} invalid position\n"
		}

		// Play at specific position
		err = s.player.PlayAt(int(pos64))
	} else {
		// Play without arguments - resume if paused, otherwise start from current position
		state := s.player.GetState()
		if state == player.StatePlaying {
			// Already playing, nothing to do
			return "OK\n"
		} else if state == player.StatePaused {
			// Resume from pause
			err = s.player.Resume()
		} else {
			// Stopped, start playback
			err = s.player.Play()
		}
	}

	if err != nil {
		return fmt.Sprintf("ACK [50@0] {play} %s\n", err.Error())
	}

	return "OK\n"
}

// cmdPause handles the 'pause' command
// pause 0 = resume, pause 1 = pause, no arg = toggle
func (s *Server) cmdPause(args []string) string {
	var shouldPause bool

	if len(args) > 0 {
		// Parse argument
		arg := args[0]
		if unquoted, err := strconv.Unquote(arg); err == nil {
			arg = unquoted
		}

		// Validate argument (0 or 1)
		if arg != "0" && arg != "1" {
			return "ACK [2@0] {pause} invalid argument\n"
		}

		shouldPause = (arg == "1")
	} else {
		// No argument - toggle pause state
		state := s.player.GetState()
		shouldPause = (state != player.StatePaused)
	}

	// Execute pause or resume
	var err error
	if shouldPause {
	    log.Printf("Calling Pause")
		err = s.player.Pause()
	} else {
	    log.Printf("Calling Resume")
		err = s.player.Resume()
	}

	if err != nil {
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
	// Cancel any pending transition (user wants to navigate current playlist)
	if s.player.GetPendingPlaylist() != nil {
		s.player.CancelTransition()
		log.Printf("Cancelled transition due to next command")
	}

	if err := s.player.Next(); err != nil {
		return fmt.Sprintf("ACK [50@0] {next} %s\n", err.Error())
	}

	// Player will notify subsystem change automatically
	return "OK\n"
}

// cmdPrevious handles the 'previous' command
func (s *Server) cmdPrevious(args []string) string {
	// Cancel any pending transition (user wants to navigate current playlist)
	if s.player.GetPendingPlaylist() != nil {
		s.player.CancelTransition()
		log.Printf("Cancelled transition due to previous command")
	}

	if err := s.player.Previous(); err != nil {
		return fmt.Sprintf("ACK [50@0] {previous} %s\n", err.Error())
	}

	// Player will notify subsystem change automatically
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
	status.WriteString(fmt.Sprintf("playlist: %d\n", pl.GetVersion()))
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

// formatTrackInfo formats track information with metadata for MPD protocol
// Outputs standard MPD fields in the correct order and format
// Only outputs tags that are enabled via tagtypes command
func (s *Server) formatTrackInfo(track *playlist.Track, pos int) string {
	var info strings.Builder

	// Required fields
	info.WriteString(fmt.Sprintf("file: %s\n", track.URL))

	// Get read lock for tag types
	s.tagTypesMu.RLock()
	defer s.tagTypesMu.RUnlock()

	// Output metadata fields that are enabled
	for tag, mpdField := range metadataFields {
		// Check if this tag type is enabled
		if !s.enabledTags[tag] {
			continue
		}
		if value, ok := track.Metadata[tag]; ok && value != "" {
			info.WriteString(fmt.Sprintf("%s: %s\n", mpdField, value))
		}
	}

	// Duration (Time field in MPD) - always output
	if duration, ok := track.Metadata["duration"]; ok && duration != "" {
		// Parse duration as float and convert to integer seconds
		var durationSec float64
		if _, err := fmt.Sscanf(duration, "%f", &durationSec); err == nil {
			info.WriteString(fmt.Sprintf("Time: %d\n", int(durationSec)))
			info.WriteString(fmt.Sprintf("duration: %.3f\n", durationSec))
		}
	}

	// Position and ID - always output
	info.WriteString(fmt.Sprintf("Pos: %d\n", pos))
	info.WriteString(fmt.Sprintf("Id: %d\n", pos))

	return info.String()
}

// cmdPlaylistInfo handles the 'playlistinfo' command
func (s *Server) cmdPlaylistInfo(args []string) string {
	pl := s.player.GetPlaylist()
	tracks := pl.GetAll()

	var info strings.Builder
	for i, track := range tracks {
		info.WriteString(s.formatTrackInfo(&track, i))
	}
	info.WriteString("OK\n")

	return info.String()
}

// cmdClear handles the 'clear' command
func (s *Server) cmdClear(args []string) string {
	state := s.player.GetState()

	// If playing, create a pending playlist for transition
	if state == player.StatePlaying {
		s.player.BeginTransition()
		log.Printf("Created pending playlist due to clear during playback")
	} else {
		// Not playing, replace with new empty playlist
		s.player.ReplacePlaylist(playlist.NewPlaylist())
	}

	// Notify idle connections of playlist change
	s.NotifySubsystemChange("playlist")

	return "OK\n"
}

// cmdCurrentSong handles the 'currentsong' command
func (s *Server) cmdCurrentSong(args []string) string {
	pl := s.player.GetPlaylist()
	track, err := pl.Current()
	if err != nil {
		return "OK\n" // No current song
	}

	var info strings.Builder
	info.WriteString(s.formatTrackInfo(track, pl.CurrentIndex()))
	info.WriteString("OK\n")

	return info.String()
}

// cmdPlChanges handles the 'plchanges' command
// Returns changed songs in playlist since given version
func (s *Server) cmdPlChanges(args []string) string {
	if len(args) == 0 {
		return "ACK [2@0] {plchanges} missing playlist version argument\n"
	}

	// Parse version argument
	versionStr := args[0]

	// Try to unquote if it's a quoted string, otherwise use as-is
	if unquoted, err := strconv.Unquote(versionStr); err == nil {
		versionStr = unquoted
	}

	// Parse as uint32
	version64, err := strconv.ParseUint(versionStr, 10, 32)
	if err != nil {
		log.Printf("Invalid version argument: %s", args[0])
		return "ACK [2@0] {plchanges} invalid playlist version number\n"
	}
	requestedVersion := uint32(version64)

	pl := s.player.GetPlaylist()
	changes := pl.GetChangesSince(requestedVersion)

	var info strings.Builder
	for _, event := range changes {
		// Only return "add" events; "clear" events don't have tracks to show
		if event.Operation == "add" && event.Track != nil {
			info.WriteString(s.formatTrackInfo(event.Track, event.Position))
		}
	}
	info.WriteString("OK\n")

	return info.String()
}

// cmdTagTypes handles the 'tagtypes' command
// Controls which metadata tags are returned in responses
func (s *Server) cmdTagTypes(args []string) string {
	if len(args) == 0 {
		// List all enabled tag types
		s.tagTypesMu.RLock()
		defer s.tagTypesMu.RUnlock()

		var response strings.Builder
		for tag, enabled := range s.enabledTags {
			if enabled {
				response.WriteString(fmt.Sprintf("tagtype: %s\n", tag))
			}
		}
		response.WriteString("OK\n")
		return response.String()
	}

	// Handle subcommands
	subcommand := strings.ToLower(args[0])

	// Try to unquote if it's a quoted string, otherwise use as-is
	if unquoted, err := strconv.Unquote(subcommand); err == nil {
		subcommand = unquoted
	}

	switch subcommand {
	case "clear":
		// Disable all tag types
		s.tagTypesMu.Lock()
		for tag := range s.enabledTags {
			s.enabledTags[tag] = false
		}
		s.tagTypesMu.Unlock()
		return "OK\n"

	case "enable":
		// Enable specified tag types
		s.tagTypesMu.Lock()
		for _, tag := range args[1:] {
			tagLower := strings.ToLower(tag)
			s.enabledTags[tagLower] = true
		}
		s.tagTypesMu.Unlock()
		return "OK\n"

	case "disable":
		// Disable specified tag types
		s.tagTypesMu.Lock()
		for _, tag := range args[1:] {
			tagLower := strings.ToLower(tag)
			s.enabledTags[tagLower] = false
		}
		s.tagTypesMu.Unlock()
		return "OK\n"

	case "all":
		// Enable all tag types
		s.tagTypesMu.Lock()
		for tag := range s.enabledTags {
			s.enabledTags[tag] = true
		}
		s.tagTypesMu.Unlock()
		return "OK\n"

	default:
		return fmt.Sprintf("ACK [2@0] {tagtypes} unknown subcommand: %s\n", subcommand)
	}
}

// cmdOutputs handles the 'outputs' command
// Returns the list of audio outputs (in this case, the Diretta target)
func (s *Server) cmdOutputs(args []string) string {
	// Get target info from player
	_, _, targetName, _ := s.player.GetTargetInfo()

	// If no target name is available, use a default
	if targetName == "" {
		targetName = "Diretta Output"
	}

	var response strings.Builder
	response.WriteString("outputid: 0\n")
	response.WriteString(fmt.Sprintf("outputname: %s\n", targetName))
	response.WriteString("outputenabled: 1\n")
	response.WriteString("OK\n")

	return response.String()
}

// cmdDecoders handles the 'decoders' command
// Returns the list of supported audio decoders (based on ffmpeg capabilities)
func (s *Server) cmdDecoders(args []string) string {
	var response strings.Builder

	for _, decoder := range supportedDecoders {
		response.WriteString(fmt.Sprintf("plugin: %s\n", decoder.plugin))
		for _, suffix := range decoder.suffixes {
			response.WriteString(fmt.Sprintf("suffix: %s\n", suffix))
		}
		for _, mimeType := range decoder.mimeTypes {
			response.WriteString(fmt.Sprintf("mime_type: %s\n", mimeType))
		}
	}

	response.WriteString("OK\n")
	return response.String()
}

// registerIdle registers an idle connection to receive notifications
func (s *Server) registerIdle(idle *idleConnection) {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	s.idleConns[idle] = true
	log.Printf("Registered idle connection (total: %d)", len(s.idleConns))
}

// unregisterIdle removes an idle connection from notifications
func (s *Server) unregisterIdle(idle *idleConnection) {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	delete(s.idleConns, idle)
	log.Printf("Unregistered idle connection (total: %d)", len(s.idleConns))
}

// NotifySubsystemChange notifies all idle connections about a subsystem change
// This should be called whenever a relevant subsystem changes (playlist, player, etc.)
func (s *Server) NotifySubsystemChange(subsystem string) {
	s.idleMu.RLock()
	defer s.idleMu.RUnlock()

	log.Printf("Notifying %d idle connections of %s change", len(s.idleConns), subsystem)

	for idle := range s.idleConns {
		// Check if this connection is watching this subsystem
		if len(idle.subsystems) == 0 || idle.subsystems[subsystem] {
			// Send notification (non-blocking)
			select {
			case idle.notify <- subsystem:
			default:
				log.Printf("Warning: idle notification channel full")
			}
		}
	}
}

// cmdAddId handles the 'addid' command
// Like 'add' but returns the song ID of the added track
// Supports optional position argument: addid URI [POS]
func (s *Server) cmdAddId(args []string) string {
	if len(args) == 0 {
		return "ACK [2@0] {addid} missing URI\n"
	}

	uri := args[0]

	// Try to unquote if it's a quoted string, otherwise use as-is
	if unquoted, err := strconv.Unquote(uri); err == nil {
		uri = unquoted
	}

	var position *int
	// Check for optional position argument
	if len(args) > 1 {
		// Parse position argument
		posArg := args[1]
		if unquoted, err := strconv.Unquote(posArg); err == nil {
			posArg = unquoted
		}

		pos64, err := strconv.ParseInt(posArg, 10, 32)
		if err != nil {
			return "ACK [2@0] {addid} invalid position\n"
		}
		pos := int(pos64)
		position = &pos
	}

	songId := s.addTrackToPlaylist(uri, position)

	// Notify idle connections of playlist change
	s.NotifySubsystemChange("playlist")

	// Return the ID of the added song
	return fmt.Sprintf("Id: %d\nOK\n", songId)
}

// cmdSingle handles the 'single' command
// Sets single mode (play one song and stop)
func (s *Server) cmdSingle(args []string) string {
	if len(args) == 0 {
		return "ACK [2@0] {single} missing argument\n"
	}

	// Parse the argument (0 or 1)
	arg := args[0]
	if unquoted, err := strconv.Unquote(arg); err == nil {
		arg = unquoted
	}

	// Validate argument
	if arg != "0" && arg != "1" {
		return "ACK [2@0] {single} invalid argument\n"
	}

	// For now, accept the command but don't implement the behavior
	// Single mode would require stopping after one track completes
	log.Printf("Single mode set to: %s (not implemented)", arg)

	return "OK\n"
}

// cmdConsume handles the 'consume' command
// Sets consume mode (remove songs from playlist after playing)
func (s *Server) cmdConsume(args []string) string {
	if len(args) == 0 {
		return "ACK [2@0] {consume} missing argument\n"
	}

	// Parse the argument (0 or 1)
	arg := args[0]
	if unquoted, err := strconv.Unquote(arg); err == nil {
		arg = unquoted
	}

	// Validate argument
	if arg != "0" && arg != "1" {
		return "ACK [2@0] {consume} invalid argument\n"
	}

	// For now, accept the command but don't implement the behavior
	// Consume mode would require removing tracks from playlist after playing
	log.Printf("Consume mode set to: %s (not implemented)", arg)

	return "OK\n"
}

// cmdRepeat handles the 'repeat' command
// Sets repeat mode (repeat playlist)
func (s *Server) cmdRepeat(args []string) string {
	if len(args) == 0 {
		return "ACK [2@0] {repeat} missing argument\n"
	}

	// Parse the argument (0 or 1)
	arg := args[0]
	if unquoted, err := strconv.Unquote(arg); err == nil {
		arg = unquoted
	}

	// Validate argument
	if arg != "0" && arg != "1" {
		return "ACK [2@0] {repeat} invalid argument\n"
	}

	// For now, accept the command but don't implement the behavior
	// Repeat mode would require restarting playlist after last track
	log.Printf("Repeat mode set to: %s (not implemented)", arg)

	return "OK\n"
}

// cmdRandom handles the 'random' command
// Sets random mode (shuffle playlist)
func (s *Server) cmdRandom(args []string) string {
	if len(args) == 0 {
		return "ACK [2@0] {random} missing argument\n"
	}

	// Parse the argument (0 or 1)
	arg := args[0]
	if unquoted, err := strconv.Unquote(arg); err == nil {
		arg = unquoted
	}

	// Validate argument
	if arg != "0" && arg != "1" {
		return "ACK [2@0] {random} invalid argument\n"
	}

	// For now, accept the command but don't implement the behavior
	// Random mode would require shuffling track order
	log.Printf("Random mode set to: %s (not implemented)", arg)

	return "OK\n"
}
