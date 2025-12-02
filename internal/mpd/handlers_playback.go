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
func (s *Server) cmdStop(_ []string) string {
	if err := s.player.Stop(); err != nil {
		return fmt.Sprintf("ACK [50@0] {stop} %s\n", err.Error())
	}

	return "OK\n"
}

// cmdNext handles the 'next' command
func (s *Server) cmdNext(_ []string) string {
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
func (s *Server) cmdPrevious(_ []string) string {
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

// cmdSeek handles the 'seek' command
// seek {SONGPOS} {TIME} - seek to TIME (in seconds) within song SONGPOS
func (s *Server) cmdSeek(args []string) string {
	if len(args) < 2 {
		return "ACK [2@0] {seek} missing arguments\n"
	}

	// Parse song position
	songPos := args[0]
	if unquoted, err := strconv.Unquote(songPos); err == nil {
		songPos = unquoted
	}
	pos64, err := strconv.ParseInt(songPos, 10, 32)
	if err != nil {
		return "ACK [2@0] {seek} invalid song position\n"
	}

	// Parse time argument (can be float, e.g., "120.5")
	timeArg := args[1]
	if unquoted, err := strconv.Unquote(timeArg); err == nil {
		timeArg = unquoted
	}
	timeFloat, err := strconv.ParseFloat(timeArg, 64)
	if err != nil {
		return "ACK [2@0] {seek} invalid time\n"
	}
	timeSeconds := int64(timeFloat)

	// Verify that the requested song position matches the current position
	currentPos := s.player.GetPlaylist().CurrentIndex()
	if int(pos64) != currentPos {
		return fmt.Sprintf("ACK [2@0] {seek} can only seek within current song (current: %d, requested: %d)\n", currentPos, pos64)
	}

	// Perform the seek
	if err := s.player.Seek(timeSeconds); err != nil {
		return fmt.Sprintf("ACK [50@0] {seek} %s\n", err.Error())
	}

	return "OK\n"
}

// cmdSeekCur handles the 'seekcur' command
// seekcur {TIME} - seek to TIME within the current song
// TIME can be:
//   - absolute: "120" = seek to 120 seconds
//   - relative positive: "+10" = seek forward 10 seconds
//   - relative negative: "-10" = seek backward 10 seconds
func (s *Server) cmdSeekCur(args []string) string {
	if len(args) < 1 {
		return "ACK [2@0] {seekcur} missing argument\n"
	}

	// Parse time argument
	timeArg := args[0]
	if unquoted, err := strconv.Unquote(timeArg); err == nil {
		timeArg = unquoted
	}

	// Check if it's relative (starts with + or -)
	isRelative := len(timeArg) > 0 && (timeArg[0] == '+' || timeArg[0] == '-')

	// Parse as float (can be "120.5")
	timeFloat, err := strconv.ParseFloat(timeArg, 64)
	if err != nil {
		return "ACK [2@0] {seekcur} invalid time\n"
	}
	timeSeconds := int64(timeFloat)

	var seekErr error
	if isRelative {
		// Relative seek
		seekErr = s.player.SeekCur(timeSeconds)
	} else {
		// Absolute seek
		seekErr = s.player.Seek(timeSeconds)
	}

	if seekErr != nil {
		return fmt.Sprintf("ACK [50@0] {seekcur} %s\n", seekErr.Error())
	}

	return "OK\n"
}
