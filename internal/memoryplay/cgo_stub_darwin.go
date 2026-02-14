//go:build !linux

package memoryplay

import "fmt"

var errUnsupported = fmt.Errorf("CGo MemoryPlay bindings are only supported on Linux")

// InitLibrary initializes the MemoryPlay controller library
func InitLibrary(enableLogging, verboseMode bool) error {
	return errUnsupported
}

// CleanupLibrary releases library resources
func CleanupLibrary() {}

// ListHosts discovers available MemoryPlay hosts on the network
func ListHosts() ([]HostInfo, error) {
	return nil, errUnsupported
}

// ListTargets lists available Diretta targets from a host
func ListTargets(hostAddress string, interfaceNumber uint32) ([]TargetInfo, error) {
	return nil, errUnsupported
}

// WavFile represents an opened audio file
type WavFile struct{}

// OpenWavFile opens an audio file (WAV/FLAC/DSF/DFF/AIFF)
func OpenWavFile(filename string) (*WavFile, error) {
	return nil, errUnsupported
}

// Close closes the audio file
func (w *WavFile) Close() {}

// GetTitle returns the title/metadata
func (w *WavFile) GetTitle() string { return "" }

// GetIndex returns the track index
func (w *WavFile) GetIndex() int { return 0 }

// GetFormat returns the audio format handle
func (w *WavFile) GetFormat() (*FormatHandle, error) {
	return nil, errUnsupported
}

// FreeFormat releases a format handle
func FreeFormat(fh *FormatHandle) {}

// UploadAudio uploads audio files to a MemoryPlay host
func UploadAudio(hostAddress string, interfaceNumber uint32, wavFiles []*WavFile, fh *FormatHandle, loopMode bool) error {
	return errUnsupported
}

// Session represents a control session to a MemoryPlay host
type Session struct{}

// CreateSession creates a control session
func CreateSession(hostAddress string, interfaceNumber uint32) (*Session, error) {
	return nil, errUnsupported
}

// Close closes the session
func (s *Session) Close() {}

// ConnectTarget connects to a specific Diretta target
func (s *Session) ConnectTarget(targetAddress string, interfaceNumber uint32) error {
	return errUnsupported
}

// Play starts or resumes playback
func (s *Session) Play() error { return errUnsupported }

// Pause pauses playback
func (s *Session) Pause() error { return errUnsupported }

// Seek seeks forward or backward by seconds
func (s *Session) Seek(offsetSeconds int64) error { return errUnsupported }

// SeekAbsolute seeks to an absolute position in seconds
func (s *Session) SeekAbsolute(positionSeconds int64) error { return errUnsupported }

// SeekToStart seeks to the beginning
func (s *Session) SeekToStart() error { return errUnsupported }

// Quit stops playback and disconnects
func (s *Session) Quit() error { return errUnsupported }

// GetPlayStatus returns current playback status
func (s *Session) GetPlayStatus() (PlaybackStatus, error) {
	return StatusDisconnected, errUnsupported
}

// GetCurrentTime returns current playback time in seconds
func (s *Session) GetCurrentTime() (int64, error) {
	return 0, errUnsupported
}

// GetTagList returns the list of tags
func (s *Session) GetTagList() ([]TagInfo, error) {
	return nil, errUnsupported
}
