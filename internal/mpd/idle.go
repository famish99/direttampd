package mpd

import (
	"log"
)

// idleConnection represents a connection waiting in idle mode
type idleConnection struct {
	subsystems map[string]bool // Subsystems to watch (empty = all)
	notify     chan string     // Channel to send subsystem changes
	cancel     chan struct{}   // Channel to cancel idle wait
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