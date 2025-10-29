package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// RiskLevel represents the risk level of a cleaning category
type RiskLevel int

const (
	Low RiskLevel = iota
	Medium
	High
)

// String returns the string representation of RiskLevel
func (r RiskLevel) String() string {
	switch r {
	case Low:
		return "Low"
	case Medium:
		return "Medium"
	case High:
		return "High"
	default:
		return "Unknown"
	}
}

// ParseRiskLevel parses a string into a RiskLevel
func ParseRiskLevel(s string) (RiskLevel, error) {
	switch s {
	case "Low":
		return Low, nil
	case "Medium":
		return Medium, nil
	case "High":
		return High, nil
	default:
		return 0, fmt.Errorf("invalid risk level: %s", s)
	}
}

// FileInfo represents information about a file
type FileInfo struct {
	Path    string
	Size    uint64
	ModTime string
}

// Category represents a cleaning category
type Category struct {
	Name         string     `toml:"name"`
	Paths        []string   `toml:"paths"`
	Filters      []string   `toml:"filters"`
	Risk         RiskLevel  `toml:"risk"`
	Size         uint64     `toml:"size,omitempty"`
	FileCount    int        `toml:"file_count,omitempty"`
	Files        []FileInfo `toml:"files,omitempty"`
	Selected     bool       `toml:"selected,omitempty"`
	ShredEnabled bool       `toml:"shred,omitempty"`
}

// Config represents the main configuration
type Config struct {
	Scan struct {
		MaxDepth       int      `toml:"max_depth"`
		IgnorePatterns []string `toml:"ignore_patterns"`
		EnableAll      bool     `toml:"enable_all"`
		DryRunDefault  bool     `toml:"dry_run_default"`
	} `toml:"scan"`
	Categories []Category `toml:"categories"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	cfg := &Config{
		Scan: struct {
			MaxDepth       int      `toml:"max_depth"`
			IgnorePatterns []string `toml:"ignore_patterns"`
			EnableAll      bool     `toml:"enable_all"`
			DryRunDefault  bool     `toml:"dry_run_default"`
		}{
			MaxDepth:       5,
			IgnorePatterns: []string{"node_modules", ".git", ".svn", ".hg"},
			EnableAll:      true,
			DryRunDefault:  true,
		},
		Categories: []Category{
			{
				Name:         "Pacman Cache",
				Paths:        []string{"/var/cache/pacman/pkg"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "System Temporary Files",
				Paths:        []string{"/tmp"},
				Filters:      []string{".*\\.(tmp|temp|bak|backup)$"},
				Risk:         Medium,
				Selected:     true,
				ShredEnabled: true,
			},
			{
				Name:         "Browser Cache",
				Paths:        []string{"/home/*/.cache"},
				Filters:      []string{".*cache.*", ".*\\.(cache|webcache|webloc)$"},
				Risk:         Medium,
				Selected:     false,
				ShredEnabled: false,
			},
			{
				Name:         "Thumbnail Cache",
				Paths:        []string{"/home/*/.cache/thumbnails"},
				Risk:         Low,
				Selected:     false,
				ShredEnabled: false,
			},
			{
				Name:         "Application Logs",
				Paths:        []string{"/home/*/.local/share", "/home/*/.log"},
				Filters:      []string{".*\\.(log|out)$"},
				Risk:         High,
				Selected:     false,
				ShredEnabled: true,
			},
		},
	}
	return cfg
}

// Load loads configuration from file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		// Use default config path
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(homeDir, ".config", "moonbit", "config.toml")
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Create default config file
		if err := Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save default config: %w", err)
		}
		return cfg, nil
	}

	// Load config file
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	return cfg, nil
}

// Save saves configuration to file
func Save(cfg *Config, path string) error {
	// Create directory if it doesn't exist
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}

	// Create file
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	// Encode config
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}

	return nil
}

// Validate validates the configuration
func (cfg *Config) Validate() error {
	if cfg.Scan.MaxDepth < 1 || cfg.Scan.MaxDepth > 10 {
		return fmt.Errorf("max_depth must be between 1 and 10, got %d", cfg.Scan.MaxDepth)
	}

	for i, cat := range cfg.Categories {
		if cat.Name == "" {
			return fmt.Errorf("category %d has empty name", i)
		}
		if len(cat.Paths) == 0 {
			return fmt.Errorf("category %s has no paths", cat.Name)
		}
	}

	return nil
}
