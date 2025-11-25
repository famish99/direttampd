package player

import (
	"fmt"
	"log"
	"strings"

	"github.com/famish99/direttampd/internal/config"
	"github.com/famish99/direttampd/internal/memoryplay"
)

// DiscoverHosts discovers all available MemoryPlay hosts.
// Returns a list of all discovered hosts without selection.
func DiscoverHosts() ([]memoryplay.HostInfo, error) {
	log.Printf("Discovering MemoryPlay hosts...")
	hosts, err := memoryplay.ListHosts()
	if err != nil {
		return nil, fmt.Errorf("failed to discover hosts: %w", err)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no MemoryPlay hosts found")
	}
	return hosts, nil
}

// DiscoverTargets discovers all available targets from a MemoryPlay host.
// Returns a list of all discovered targets without selection.
func DiscoverTargets(hostIP string, hostIfNum uint32) ([]memoryplay.TargetInfo, error) {
	log.Printf("Discovering available targets...")
	targets, err := memoryplay.ListTargets(hostIP, hostIfNum)
	if err != nil {
		return nil, fmt.Errorf("failed to discover targets: %w", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets found on host")
	}
	return targets, nil
}

// DiscoverAndSelectHost discovers available MemoryPlay hosts and selects one based on config.
// This function always performs discovery and returns the selected host info.
func DiscoverAndSelectHost(cfg *config.Config) (*memoryplay.HostInfo, error) {
	log.Printf("Discovering MemoryPlay hosts...")
	hosts, err := memoryplay.ListHosts()
	if err != nil || len(hosts) == 0 {
		return nil, fmt.Errorf("no MemoryPlay hosts found: %w", err)
	}

	// Select host: prefer config IP if specified, otherwise prefer loopback, otherwise first
	var selectedHost *memoryplay.HostInfo
	if cfg.Host.IP != "" {
		// Look for host matching config IP
		// Host IP format is "IP,PORT%IFNO" so we need to extract just the IP part
		for i := range hosts {
			hostIP := hosts[i].IPAddress
			// Strip port and interface: "::1,43425%0" -> "::1"
			if idx := strings.Index(hostIP, ","); idx != -1 {
				hostIP = hostIP[:idx]
			}
			if hostIP == cfg.Host.IP {
				selectedHost = &hosts[i]
				break
			}
		}
		if selectedHost == nil {
			return nil, fmt.Errorf("configured host IP %s not found", cfg.Host.IP)
		}
	} else {
		// Auto-select: prefer loopback
		for i := range hosts {
			if hosts[i].IsLoopback {
				selectedHost = &hosts[i]
				break
			}
		}
		if selectedHost == nil {
			selectedHost = &hosts[0]
		}
	}

	log.Printf("Discovered MemoryPlay host: %s%%%d (%s - %s)",
		selectedHost.IPAddress,
		selectedHost.InterfaceNumber,
		selectedHost.TargetName,
		selectedHost.OutputName)

	return selectedHost, nil
}

// DiscoverAndSelectTarget discovers available targets from a host and selects one based on config.
// This function always performs discovery and returns the selected target info.
func DiscoverAndSelectTarget(hostIP string, hostIfNum uint32, cfg *config.Config) (*memoryplay.TargetInfo, error) {
	log.Printf("Discovering available targets...")
	targets, err := memoryplay.ListTargets(hostIP, hostIfNum)
	if err != nil || len(targets) == 0 {
		return nil, fmt.Errorf("no targets found on host: %w", err)
	}

	// Select target: prefer config preferred_target if specified, otherwise first
	var selectedTarget *memoryplay.TargetInfo
	if cfg.PreferredTarget != "" {
		// Look for target matching config name
		for i := range targets {
			if targets[i].TargetName == cfg.PreferredTarget {
				selectedTarget = &targets[i]
				break
			}
		}
		if selectedTarget == nil {
			return nil, fmt.Errorf("configured target %s not found", cfg.PreferredTarget)
		}
	} else {
		selectedTarget = &targets[0]
	}

	return selectedTarget, nil
}