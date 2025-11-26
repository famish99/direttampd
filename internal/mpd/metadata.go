package mpd

import (
	"fmt"
	"strconv"
	"strings"

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