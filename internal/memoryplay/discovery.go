package memoryplay

import (
	"fmt"
	"log"
)

// DiscoverHosts discovers all available MemoryPlay hosts.
// Returns a list of all discovered hosts without selection.
func DiscoverHosts() ([]HostInfo, error) {
	log.Printf("Discovering MemoryPlay hosts...")
	hosts, err := ListHosts()
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
func DiscoverTargets(hostIP string, hostIfNum uint32) ([]TargetInfo, error) {
	log.Printf("Discovering available targets...")
	targets, err := ListTargets(hostIP, hostIfNum)
	if err != nil {
		return nil, fmt.Errorf("failed to discover targets: %w", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets found on host")
	}
	return targets, nil
}
