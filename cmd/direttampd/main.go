package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/famish99/direttampd/internal/config"
	"github.com/famish99/direttampd/internal/memoryplay"
	"github.com/famish99/direttampd/internal/mpd"
	"github.com/famish99/direttampd/internal/player"
)

var (
	configPath  = flag.String("config", getDefaultConfigPath(), "Path to configuration file")
	host        = flag.String("host", "", "MemoryPlay host IP address (overrides config, default: ::1 for localhost)")
	targetName  = flag.String("target", "", "Override preferred target by name")
	listTargets = flag.Bool("list-targets", false, "List available targets from MemoryPlay host and exit")
	mpdAddr     = flag.String("mpd-addr", "localhost:6600", "MPD server listen address")
	daemonMode  = flag.Bool("daemon", false, "Run as MPD server daemon (otherwise play URLs and exit)")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Handle list-targets command
	if *listTargets {
		if err := listAvailableTargets(*host); err != nil {
			log.Fatalf("Failed to list targets: %v", err)
		}
		return
	}

	// Override target if specified
	if *targetName != "" {
		if err := cfg.SetPreferredTarget(*targetName); err != nil {
			log.Fatalf("Invalid target: %v", err)
		}
	}

	// Create player
	p, err := player.NewPlayer(cfg)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}

	// Connect to MemoryPlay target
	if err := p.Connect(); err != nil {
		log.Fatalf("Failed to connect to MemoryPlay: %v", err)
	}
	defer p.Disconnect()

	// Daemon mode: run MPD server
	if *daemonMode {
		runDaemon(p)
		return
	}

	// Direct mode: play URLs from command line
	urls := flag.Args()
	if len(urls) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <url1> [url2] ...\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Play URLs directly\n")
		fmt.Fprintf(os.Stderr, "  %s http://stream.example.com/radio.mp3\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s file:///music/album/track01.flac file:///music/album/track02.flac\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Run as MPD server daemon\n")
		fmt.Fprintf(os.Stderr, "  %s --daemon\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  mpc add http://stream.example.com/radio.mp3\n")
		fmt.Fprintf(os.Stderr, "  mpc play\n")
		os.Exit(1)
	}

	runDirect(p, urls)
}

// runDaemon runs the MPD server daemon
func runDaemon(p *player.Player) {
	// Create and start MPD server
	server := mpd.NewServer(*mpdAddr, p)
	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start MPD server: %v", err)
	}
	defer server.Stop()

	log.Printf("Direttampd running in daemon mode")
	log.Printf("Connect with MPD clients to %s", *mpdAddr)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Printf("\nShutting down...")
}

// runDirect plays URLs directly and exits
func runDirect(p *player.Player, urls []string) {
	// Add URLs to playlist
	p.AddURLs(urls)

	// Start playback
	log.Printf("Starting playback of %d tracks...", len(urls))
	if err := p.Play(); err != nil {
		log.Fatalf("Failed to start playback: %v", err)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Printf("\nShutting down...")

	// Stop playback
	if err := p.Stop(); err != nil {
		log.Printf("Error stopping playback: %v", err)
	}
}

func getDefaultConfigPath() string {
	// Check common locations
	locations := []string{
		"./direttampd.yaml",
		"./config.yaml",
		filepath.Join(os.Getenv("HOME"), ".config", "direttampd", "config.yaml"),
		"/etc/direttampd/config.yaml",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	// Default to first location if none exist
	return locations[0]
}

func listAvailableTargets(hostIP string) error {
	// Default to localhost if not specified
	if hostIP == "" {
		hostIP = "::1"
	}

	fmt.Printf("Connecting to MemoryPlay host at %s...\n", hostIP)

	// Create a temporary target to connect to the host
	target := &memoryplay.Target{
		IP:        hostIP,
		Interface: "",
		Name:      "query",
	}

	// Create client and connect
	client := memoryplay.NewClient(target)
	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to MemoryPlay host: %w", err)
	}
	defer client.Disconnect()

	// Query target list with 5 second timeout
	targets, err := client.GetTargetList(5 * time.Second)
	if err != nil {
		return err
	}

	// Display results
	if len(targets) == 0 {
		fmt.Println("No targets available")
		return nil
	}

	fmt.Println("\nAvailable targets:")
	for _, t := range targets {
		fmt.Printf("  - %s\n", t)
	}

	return nil
}
