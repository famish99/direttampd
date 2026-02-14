package memoryplay

// HostInfo represents a discovered MemoryPlay host
type HostInfo struct {
	IPAddress       string
	InterfaceNumber uint32
	TargetName      string
	OutputName      string
	IsLoopback      bool
}

// TargetInfo represents a Diretta target device
type TargetInfo struct {
	IPAddress       string
	InterfaceNumber uint32
	TargetName      string
}

// PlaybackStatus represents playback status
type PlaybackStatus int

const (
	StatusDisconnected PlaybackStatus = 0
	StatusPlaying      PlaybackStatus = 1
	StatusPaused       PlaybackStatus = 2
)

// TagInfo represents a tag entry
type TagInfo struct {
	Tag string // Format: "INDEX:TIME:NAME"
}

// FormatHandle is an opaque handle for audio format information.
// On Linux it wraps the C MPCFormatHandle; on other platforms it is unused.
type FormatHandle struct {
	handle interface{}
}
