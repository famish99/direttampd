package mpd

import (
	"fmt"
	"log"
	"strconv"

	"github.com/famish99/direttampd/internal/player"
)

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