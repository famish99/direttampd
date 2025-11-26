package mpd

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/famish99/direttampd/internal/player"
	"github.com/famish99/direttampd/internal/playlist"
)

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