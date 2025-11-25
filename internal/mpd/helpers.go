package mpd

import (
	"log"
)

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