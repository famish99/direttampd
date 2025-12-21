package mpd

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/famish99/direttampd/internal/player"
)

// cmdStatus handles the 'status' command
func (s *Server) cmdStatus(_ []string) string {
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

// cmdOutputs handles the 'outputs' command
// Returns the list of audio outputs (in this case, the Diretta target)
func (s *Server) cmdOutputs(_ []string) string {
	// Get output name from player
	outputName := s.player.GetOutputName()

	// If no output name is available, use a default
	if outputName == "" {
		outputName = "Diretta Output"
	}

	var response strings.Builder
	response.WriteString("outputid: 0\n")
	response.WriteString(fmt.Sprintf("outputname: %s\n", outputName))
	response.WriteString("outputenabled: 1\n")
	response.WriteString("OK\n")

	return response.String()
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
