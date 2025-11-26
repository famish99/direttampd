
# Direttampd

A Go-based audio player that streams to Diretta MemoryPlay targets, with MPD protocol support, intelligent caching, and native format preservation.

## Features

- **MPD Protocol Support**: Control via any MPD client (mpc, ncmpcpp, etc.)
- **Native Format Preservation**: Audio is decoded to its native sample rate, bit depth, and channels - no transcoding or quality loss
- **Intelligent Disk Cache**: LRU-based persistent cache with configurable size limits
- **MemoryPlay Protocol**: Full support for streaming to Diretta audio targets
- **Multiple Formats**: Supports MP3, FLAC, AAC, WAV, Opus, Vorbis, and all formats supported by ffmpeg
- **Async Caching**: Cache writes don't block playback
- **Dual Mode**: Run as MPD daemon or use directly from command line

## Requirements

- Go 1.21 or later
- C++ compiler (g++ or clang++)
- GNU Make
- `ffmpeg` and `ffprobe` installed and in PATH
- FLAC development libraries (libFLAC++)
- Diretta ACQUA and Find libraries (included in MemoryPlayController)

## Installation

```bash
# Clone the repository
git clone https://github.com/famish99/direttampd
cd direttampd

# Build the MemoryPlayController C++ shared library
cd MemoryPlayController
make -f Makefile.lib
cd ..

# Build the Go application (uses CGO to link with the C++ library)
go build -o direttampd ./cmd/direttampd

# Install (optional)
go install ./cmd/direttampd
```

**Note**: The MemoryPlayController library must be built first as it provides the core Diretta protocol implementation and device discovery functionality through CGO bindings.

## Configuration

Create a configuration file at one of these locations (checked in order):
- `./direttampd.yaml`
- `./config.yaml`
- `~/.config/direttampd/config.yaml`
- `/etc/direttampd/config.yaml`

Example configuration:

```yaml
# MemoryPlay output targets
targets:
  - name: living-room
    ip: "fe80::1234:5678:9abc:def0"
    interface: "eth0"

  - name: bedroom
    ip: "fe80::abcd:ef01:2345:6789"
    interface: "eth0"

# Preferred target
preferred_target: living-room

# Cache settings
cache:
  directory: "/tmp/direttampd-cache"
  max_size_gb: 10

# Playback settings
playback:
  silence_buffer_seconds: 3
```

See `config.example.yaml` for a complete example.

## Usage

### MPD Daemon Mode

Run as an MPD server that any MPD client can connect to:

```bash
# Start daemon (default: localhost:6600)
direttampd --daemon

# In another terminal, use MPD clients:
mpc add http://radio.example.com/stream.mp3
mpc add file:///music/album/track.flac
mpc play

# Or use ncmpcpp, ario, cantata, etc.
```

### Direct Mode

Play audio files or streams directly:

```bash
# Play a single file
direttampd file:///music/track.flac

# Play multiple files
direttampd file:///music/album/*.flac

# Stream from HTTP
direttampd http://radio.example.com/stream.mp3

# Mix local and remote
direttampd file:///intro.wav http://stream.com/main.mp3
```

### Options

```bash
# Specify config file
direttampd --config /path/to/config.yaml --daemon

# Override target
direttampd --target bedroom --daemon

# Custom MPD listen address
direttampd --mpd-addr 0.0.0.0:6600 --daemon

# List configured targets
direttampd --list-targets
```

## MPD Protocol Support

Supported MPD commands:

| Command | Description |
|---------|-------------|
| `add <uri>` | Add URL to playlist |
| `play` | Start playback |
| `pause` | Pause playback |
| `stop` | Stop playback |
| `next` | Next track |
| `previous` | Previous track |
| `status` | Get player status |
| `playlistinfo` | List all tracks in playlist |
| `currentsong` | Get current track info |
| `clear` | Clear playlist |
| `ping` | Keep-alive |

## How It Works

1. **URL Processing**: Accepts file:// or http(s):// URLs via MPD or CLI
2. **Cache Check**: Looks for decoded WAV file in disk cache
3. **Decode**: If not cached, uses ffmpeg to decode to WAV format (preserving native sample rate/bit depth)
4. **Stream**: MemoryPlayController C++ library uploads WAV to MemoryPlay host via TCP/IPv6
5. **Cache**: WAV file is stored in disk cache for future use

### Cache Format

Cached files are stored as standard WAV files with their original native format preserved (sample rate, bit depth, and channels). The MemoryPlayController C++ library can read WAV, FLAC, DSF, DFF, and AIFF formats directly, so decoded files are saved as WAV for maximum compatibility.

## Architecture

```
MPD Client → MPD Server → Player → Decoder (ffmpeg) → Cache → MemoryPlay Client (Go)
     ↓           ↓                        ↓                ↓              ↓
   mpc      localhost:6600            Playlist      Disk Cache (LRU)    CGO Bindings
                                                                           ↓
                                                          MemoryPlayController (C++)
                                                          - Diretta protocol
                                                          - ACQUA TCP (IPv6)
                                                          - Device discovery
                                                                           ↓
                                                              Diretta Audio Device
                                                                           ↓
                                                                    Audio Targets
                                                                (Speakers, DACs, etc.)
```

### Components

- **`internal/mpd`**: MPD protocol server implementation
- **`internal/memoryplay`**: MemoryPlay protocol client with CGO bindings
  - `cgo_bindings.go`: C library interface with session management
  - `client.go`: High-level Go wrapper for device control
  - `protocol.go`: Diretta wire protocol implementation
- **`MemoryPlayController/`**: C++ shared library for Diretta protocol
  - Implements device discovery via IPv6 multicast
  - ACQUA TCP protocol for audio streaming
  - FLAC audio format support
  - Session control (play, pause, seek, status)
- **`internal/decoder`**: FFmpeg wrapper for audio decoding
- **`internal/cache`**: LRU disk cache with format headers
- **`internal/config`**: Configuration management
- **`internal/playlist`**: Playlist/queue management
- **`internal/player`**: Main playback coordinator

## Development

### Project Structure

```
direttampd/
├── cmd/
│   └── direttampd/              # Main application
├── internal/
│   ├── cache/                   # Disk cache implementation
│   ├── config/                  # Configuration handling
│   ├── decoder/                 # Audio decoding (ffmpeg)
│   ├── memoryplay/              # MemoryPlay protocol & CGO bindings
│   │   ├── cgo_bindings.go      # C library interface
│   │   ├── client.go            # High-level client wrapper
│   │   └── protocol.go          # Diretta wire protocol
│   ├── mpd/                     # MPD protocol server
│   ├── player/                  # Playback coordinator
│   └── playlist/                # Playlist management
├── MemoryPlayController/        # C++ shared library
│   ├── lib_memory_play_controller.h    # C API header
│   ├── lib_memory_play_controller.cpp  # Implementation
│   ├── Makefile.lib             # Library build system
│   └── test_*.c                 # Test programs
├── config.example.yaml          # Example configuration
└── go.mod                       # Go module definition
```

### Building

```bash
# Build C++ library first
cd MemoryPlayController && make -f Makefile.lib && cd ..

# Build Go application
go build ./cmd/direttampd
```

### Testing

```bash
go test ./...
```

## Protocol Documentation

- See `MPD_PROTOCOL_ANALYSIS.md` for MPD protocol reference
- See `MemoryPlayHost_Protocol.md` for MemoryPlay protocol details

## License

[Your License Here]

## Contributing

Contributions welcome! Please open an issue or pull request.
