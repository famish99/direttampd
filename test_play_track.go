package main

import (
	"flag"
	"log"

	"github.com/famish99/direttampd/internal/config"
	"github.com/famish99/direttampd/internal/player"
	"github.com/famish99/direttampd/internal/playlist"
)

var (
	configPath = flag.String("config", "./direttampd.yaml", "Path to configuration file")
	audioFile  = flag.String("file", "", "Audio file or URL to play (required)")
	trackTitle = flag.String("title", "Test Track", "Track title")
)

func main() {
	flag.Parse()

	// Validate required arguments
	if *audioFile == "" {
		log.Fatal("Error: -file argument is required\n\nUsage: go run test_play_track.go -file <path_or_url> [-title <title>] [-config <config_path>]\n\nExample:\n  go run test_play_track.go -file /path/to/audio.flac\n  go run test_play_track.go -file /path/to/audio.flac -title \"My Song\"")
	}

	log.Printf("=== Testing PlayTrack with single track ===")
	log.Printf("File: %s", *audioFile)
	log.Printf("Title: %s", *trackTitle)

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Loaded config from: %s\n", *configPath)

	// Create player (will auto-discover host and target)
	p, err := player.NewPlayer(cfg)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}
	defer p.Close()
	log.Printf("Player created successfully\n")

	// Create a single track
	track := &playlist.Track{
		URL:      *audioFile,
		Title:    *trackTitle,
		Index:    0,
		Metadata: make(map[string]string),
	}
	log.Printf("Created track: %+v", track)

	// Call PlayTrack with the track
	log.Printf("Starting playback...")
	startChunkCount := uint32(0)
	finalChunkCount, err := p.PlayTrack(track, startChunkCount)
	if err != nil {
		log.Fatalf("PlayTrack failed: %v", err)
	}

// 	// Connect to MemoryPlay session
// 	if err := p.Connect(); err != nil {
// 		log.Fatalf("Failed to connect to MemoryPlay: %v", err)
// 	}
// 	defer p.Disconnect()

	log.Printf("Playback completed successfully!")
	log.Printf("Chunks sent: %d", finalChunkCount)
}
