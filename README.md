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
- `ffmpeg` and `ffprobe` installed and in PATH

## Installation

```bash
# Clone the repository
git clone https://github.com/famish99/direttampd
cd direttampd

# Build
go build -o direttampd ./cmd/direttampd

# Install (optional)
go install ./cmd/direttampd
```

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
2. **Cache Check**: Looks for decoded audio in disk cache
3. **Decode**: If not cached, uses ffmpeg to decode to native PCM format
4. **Stream**: Sends audio to MemoryPlay target via TCP/IPv6
5. **Cache**: Asynchronously writes decoded audio to disk for future use

### Cache Format

Cached files include a 20-byte header:
- Magic: "DPCA" (4 bytes)
- Version: 0x01 (1 byte)
- Sample Rate: uint32 (4 bytes)
- Bits Per Sample: uint32 (4 bytes)
- Channels: uint32 (4 bytes)
- Reserved: padding (3 bytes)
- Followed by raw PCM audio data

## Architecture

```
MPD Client → MPD Server → Player → Decoder (ffmpeg) → Cache → MemoryPlay Client → Diretta Target
     ↓           ↓                        ↓                ↓
   mpc      localhost:6600            Playlist      Disk Cache (LRU)
```

### Components

- **`internal/mpd`**: MPD protocol server implementation
- **`internal/memoryplay`**: MemoryPlay protocol client implementation
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
│   └── direttampd/          # Main application
├── internal/
│   ├── cache/               # Disk cache implementation
│   ├── config/              # Configuration handling
│   ├── decoder/             # Audio decoding (ffmpeg)
│   ├── memoryplay/          # MemoryPlay protocol
│   ├── mpd/                 # MPD protocol server
│   ├── player/              # Playback coordinator
│   └── playlist/            # Playlist management
├── config.example.yaml      # Example configuration
└── go.mod                   # Go module definition
```

### Building

```bash
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
