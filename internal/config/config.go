package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	// MemoryPlay targets
	Targets []Target `yaml:"targets"`

	// Preferred output target name
	PreferredTarget string `yaml:"preferred_target,omitempty"`

	// Cache settings
	Cache CacheConfig `yaml:"cache"`

	// Playback settings
	Playback PlaybackConfig `yaml:"playback"`
}

// Target represents a MemoryPlay audio output target
type Target struct {
	Name      string `yaml:"name"`
	IP        string `yaml:"ip"`
	Port      string `yaml:"port,omitempty"`      // Port for MemoryPlayHost (default: 19640)
	Interface string `yaml:"interface,omitempty"`
}

// CacheConfig represents cache settings
type CacheConfig struct {
	Directory string `yaml:"directory"`
	MaxSizeGB int    `yaml:"max_size_gb"`
}

// PlaybackConfig represents playback settings
type PlaybackConfig struct {
	SilenceBufferSeconds int `yaml:"silence_buffer_seconds"`
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Targets:         []Target{},
		PreferredTarget: "",
		Cache: CacheConfig{
			Directory: "/tmp/direttampd-cache",
			MaxSizeGB: 10,
		},
		Playback: PlaybackConfig{
			SilenceBufferSeconds: 3,
		},
	}
}

// LoadConfig loads configuration from file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// If file doesn't exist, return default config
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// SaveConfig saves configuration to file
func SaveConfig(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// AddTarget adds a target to the configuration
func (c *Config) AddTarget(target Target) {
	c.Targets = append(c.Targets, target)

	// If this is the first target, make it preferred
	if len(c.Targets) == 1 {
		c.PreferredTarget = target.Name
	}
}

// GetPreferredTarget returns the preferred target, or nil if none set
func (c *Config) GetPreferredTarget() *Target {
	if c.PreferredTarget != "" {
		return c.GetTarget(c.PreferredTarget)
	}

	// If no preferred set, return first target if available
	if len(c.Targets) > 0 {
		return &c.Targets[0]
	}
	return nil
}

// GetTarget returns a target by name
func (c *Config) GetTarget(name string) *Target {
	for i := range c.Targets {
		if c.Targets[i].Name == name {
			return &c.Targets[i]
		}
	}
	return nil
}

// SetPreferredTarget sets the preferred target by name
func (c *Config) SetPreferredTarget(name string) error {
	if c.GetTarget(name) == nil {
		return fmt.Errorf("target not found: %s", name)
	}
	c.PreferredTarget = name
	return nil
}

// RemoveTarget removes a target by name
func (c *Config) RemoveTarget(name string) error {
	for i := range c.Targets {
		if c.Targets[i].Name == name {
			c.Targets = append(c.Targets[:i], c.Targets[i+1:]...)

			// If we removed the preferred target, clear it
			if c.PreferredTarget == name {
				c.PreferredTarget = ""
			}
			return nil
		}
	}
	return fmt.Errorf("target not found: %s", name)
}
