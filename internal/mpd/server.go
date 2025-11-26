package mpd

import (
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/famish99/direttampd/internal/player"
)

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