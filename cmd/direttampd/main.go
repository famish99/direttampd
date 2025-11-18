package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/famish99/direttampd/internal/config"
	"github.com/famish99/direttampd/internal/memoryplay"
	"github.com/famish99/direttampd/internal/mpd"
	"github.com/famish99/direttampd/internal/player"
)

var (
	configPath  = flag.String("config", getDefaultConfigPath(), "Path to configuration file")
	host        = flag.String("host", "", "MemoryPlay host IP address (default: ::1, port is auto-discovered)")
	targetName  = flag.String("target", "", "Override preferred target by name")
	listHosts   = flag.Bool("list-hosts", false, "List available MemoryPlay hosts and exit")
	listTargets = flag.Bool("list-targets", false, "List available targets from MemoryPlay host and exit")
	playFile    = flag.String("play", "", "Play a file or URL directly")
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

	// Handle list-hosts command
	if *listHosts {
		if err := listAvailableHosts(); err != nil {
			log.Fatalf("Failed to list hosts: %v", err)
		}
		return
	}

	// Handle list-targets command
	if *listTargets {
		hostIP := *host
		// If no host specified, use config
		if hostIP == "" {
			hostIP = cfg.Host.IP
		}
		if err := listAvailableTargets(hostIP); err != nil {
			log.Fatalf("Failed to list targets: %v", err)
		}
		return
	}

	// Override host if specified
	if *host != "" {
		cfg.SetHost(*host)
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

	// Add file from --play flag if specified
	if *playFile != "" {
		urls = append([]string{*playFile}, urls...)
	}

	if len(urls) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <url1> [url2] ...\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s --play <file|url>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Play a local file\n")
		fmt.Fprintf(os.Stderr, "  %s --play /path/to/music.flac\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --play ./song.mp3\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Play remote URLs\n")
		fmt.Fprintf(os.Stderr, "  %s --play http://stream.example.com/radio.mp3\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s http://stream.example.com/radio.mp3\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\n  # Play multiple files\n")
		fmt.Fprintf(os.Stderr, "  %s track01.flac track02.flac track03.flac\n", os.Args[0])
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

func listAvailableHosts() error {
	// Initialize the MemoryPlay library
	if err := memoryplay.InitLibrary(true, false); err != nil {
		return fmt.Errorf("failed to initialize MemoryPlay library: %w", err)
	}
	defer memoryplay.CleanupLibrary()

	// Use player discovery function
	hosts, err := player.DiscoverHosts()
	if err != nil {
		return err
	}

	// Display results
	fmt.Printf("\nFound %d MemoryPlay host(s):\n\n", len(hosts))
	for i, host := range hosts {
		fmt.Printf("%d. %s\n", i+1, host.TargetName)
		fmt.Printf("   IP Address:       %s\n", host.IPAddress)
		fmt.Printf("   Interface:        %d\n", host.InterfaceNumber)
		fmt.Printf("   Output:           %s\n", host.OutputName)
		fmt.Printf("   Is Loopback:      %v\n", host.IsLoopback)

		// Extract just the IP for config usage
		hostIP := host.IPAddress
		if idx := strings.Index(hostIP, ","); idx != -1 {
			hostIP = hostIP[:idx]
		}

		fmt.Printf("\n   To use this host, run:\n")
		fmt.Printf("     %s --host %s --list-targets\n", os.Args[0], hostIP)
		fmt.Println()
	}

	return nil
}

func listAvailableTargets(hostAddr string) error {
	// Initialize the MemoryPlay library
	if err := memoryplay.InitLibrary(true, false); err != nil {
		return fmt.Errorf("failed to initialize MemoryPlay library: %w", err)
	}
	defer memoryplay.CleanupLibrary()

	// If no host specified, discover hosts to find one
	var hostIP string
	var hostIfNum uint32

	if hostAddr == "" {
		// Discover hosts and use the first one
		hosts, err := player.DiscoverHosts()
		if err != nil {
			return err
		}

		// Extract IP and interface from first host
		hostIP = hosts[0].IPAddress
		hostIfNum = hosts[0].InterfaceNumber

		// Strip port from host IP if present
		if idx := strings.Index(hostIP, ","); idx != -1 {
			hostIP = hostIP[:idx]
		}

		fmt.Printf("Using discovered host: %s%%%d (%s)\n\n", hostIP, hostIfNum, hosts[0].TargetName)
	} else {
		// Use specified host - discover hosts to get interface number
		hosts, err := player.DiscoverHosts()
		if err != nil {
			return err
		}

		// Find matching host
		found := false
		for _, h := range hosts {
			testIP := h.IPAddress
			if idx := strings.Index(testIP, ","); idx != -1 {
				testIP = testIP[:idx]
			}
			if testIP == hostAddr {
				hostIP = hostAddr
				hostIfNum = h.InterfaceNumber
				found = true
				fmt.Printf("Using specified host: %s%%%d (%s)\n\n", hostIP, hostIfNum, h.TargetName)
				break
			}
		}

		if !found {
			return fmt.Errorf("host %s not found in discovered hosts", hostAddr)
		}
	}

	// Use player discovery function to list targets
	targets, err := player.DiscoverTargets(hostIP, hostIfNum)
	if err != nil {
		return err
	}

	// Display results
	fmt.Printf("Found %d target(s):\n\n", len(targets))
	for i, target := range targets {
		// Parse target info
		targetIP := target.IPAddress
		targetPort := "19644"
		if strings.Contains(targetIP, ",") {
			parts := strings.SplitN(targetIP, ",", 2)
			targetIP = parts[0]
			targetPort = parts[1]
		}

		fmt.Printf("%d. %s\n", i+1, target.TargetName)
		fmt.Printf("   IP:        %s\n", targetIP)
		fmt.Printf("   Port:      %s\n", targetPort)
		fmt.Printf("   Interface: %d\n", target.InterfaceNumber)
		fmt.Println()
	}

	// Load current config
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Update host configuration with interface number
	cfg.Host.IP = hostIP
	cfg.Host.Interface = hostIfNum

	// Add targets to config
	fmt.Println("Updating configuration...")
	addedCount := 0
	for _, target := range targets {
		// Parse target info
		targetIP := target.IPAddress
		targetPort := "19644"
		if strings.Contains(targetIP, ",") {
			parts := strings.SplitN(targetIP, ",", 2)
			targetIP = parts[0]
			targetPort = parts[1]
		}

		configTarget := &config.Target{
			Name:      target.TargetName,
			IP:        targetIP,
			Port:      targetPort,
			Interface: fmt.Sprintf("%d", target.InterfaceNumber),
		}

		// Check if target already exists
		if cfg.GetTarget(configTarget.Name) == nil {
			cfg.AddTarget(*configTarget)
			addedCount++
			fmt.Printf("  Added: %s\n", configTarget.Name)
		} else {
			fmt.Printf("  Skipped (already exists): %s\n", configTarget.Name)
		}
	}

	// Save config (always save to update host config)
	if err := config.SaveConfig(*configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if addedCount > 0 {
		fmt.Printf("\nSaved host and %d target(s) to %s\n", addedCount, *configPath)
	} else {
		fmt.Printf("\nSaved host configuration to %s (no new targets to add)\n", *configPath)
	}

	return nil
}
