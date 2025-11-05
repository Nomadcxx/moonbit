package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

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
	MinAgeDays   int        `toml:"min_age_days,omitempty"` // Only clean files older than this many days
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

// SessionCache stores scan results for the current session
type SessionCache struct {
	ScanResults *Category `json:"scan_results"`
	TotalSize   uint64    `json:"total_size"`
	TotalFiles  int       `json:"total_files"`
	ScannedAt   time.Time `json:"scanned_at"`
}

// getRealUserHome returns the actual user's home directory, even when running as root
func getRealUserHome() string {
	// When running with sudo, SUDO_USER contains the original user
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && os.Geteuid() == 0 {
		// Try to get home from /etc/passwd or common patterns
		userHome := "/home/" + sudoUser
		if stat, err := os.Stat(userHome); err == nil && stat.IsDir() {
			return userHome
		}
	}
	
	// Fallback to HOME or os.UserHomeDir
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	
	home, _ := os.UserHomeDir()
	return home
}

// DefaultConfig returns a comprehensive configuration with real cleaning targets
func DefaultConfig() *Config {
	userHome := getRealUserHome()
	cfg := &Config{
		Scan: struct {
			MaxDepth       int      `toml:"max_depth"`
			IgnorePatterns []string `toml:"ignore_patterns"`
			EnableAll      bool     `toml:"enable_all"`
			DryRunDefault  bool     `toml:"dry_run_default"`
		}{
			MaxDepth:       3, // Deeper scanning for comprehensive detection
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
				Name:         "Yay Cache",
				Paths:        []string{userHome + "/.cache/yay"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Paru Cache",
				Paths:        []string{userHome + "/.cache/paru"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Pamac Cache",
				Paths:        []string{userHome + "/.cache/pamac"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "User Cache",
				Paths:        []string{userHome + "/.cache"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
				Filters:      []string{`\.(tmp|temp|bak|backup)$`, `(^|/)(cache|thumbnails|browsers|applications|dotnet|gstreamer-1\.0|recently-used\.xbel)$`},
			},
			{
				Name:         "System Temp",
				Paths:        []string{"/var/tmp"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Thumbnails",
				Paths:        []string{userHome + "/.cache/thumbnails"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
				MinAgeDays:   30, // Only delete thumbnails older than 30 days
			},
			{
				Name:         "Browser Cache (Safe)",
				Paths: []string{
					userHome + "/.cache/mozilla",
					userHome + "/.cache/firefox",
					userHome + "/.cache/zen",
					userHome + "/.cache/BraveSoftware",
					userHome + "/.cache/google-chrome",
					userHome + "/.cache/chromium",
				},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "System Logs",
				Paths:        []string{"/var/log"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
				Filters:      []string{`\.(log|old|backup)$`, `^syslog`, `^messages`, `^daemon\.log`},
			},
			{
				Name:         "Recent Files",
				Paths:        []string{userHome + "/.recently-used.xbel"},
				Risk:         Medium,
				Selected:     false,
				ShredEnabled: false,
			},
			// Cross-distro Package Managers
			{
				Name:         "APT Cache (Debian/Ubuntu)",
				Paths:        []string{"/var/cache/apt/archives"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
				Filters:      []string{`\.deb$`},
			},
			{
				Name:         "DNF Cache (Fedora/RHEL)",
				Paths:        []string{"/var/cache/dnf"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Zypper Cache (openSUSE)",
				Paths:        []string{"/var/cache/zypp"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			// Additional User Caches
			{
				Name:         "Font Cache",
				Paths:        []string{userHome + "/.cache/fontconfig"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Mesa Shader Cache",
				Paths:        []string{userHome + "/.cache/mesa_shader_cache"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "WebKit Cache",
				Paths:        []string{userHome + "/.cache/webkit", userHome + "/.cache/webkitgtk"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			// .local/share cleanup
			{
				Name:         "Trash",
				Paths:        []string{userHome + "/.local/share/Trash"},
				Risk:         Low,
				Selected:     false,
				ShredEnabled: false,
			},
			{
				Name:         "Application Logs",
				Paths:        []string{userHome + "/.local/share/xorg"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
				Filters:      []string{`\.log$`, `\.old$`},
			},
			// Media Server Cleanup
			{
				Name:         "Plex Transcode",
				Paths:        []string{"/var/lib/plexmediaserver/Library/Application Support/Plex Media Server/Cache/Transcode"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Jellyfin Transcode",
				Paths:        []string{"/var/lib/jellyfin/transcodes"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Jellyfin Cache",
				Paths:        []string{"/var/lib/jellyfin/cache"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			// System Cleanup
			{
				Name:         "Crash Reports",
				Paths:        []string{"/var/crash", userHome + "/.local/share/apport/coredump"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			// Development Tools Caches
			{
				Name:         "pip Cache",
				Paths:        []string{userHome + "/.cache/pip"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "npm Cache",
				Paths:        []string{userHome + "/.npm", userHome + "/.cache/npm"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Cargo Cache",
				Paths:        []string{userHome + "/.cargo/registry/cache"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Gradle Cache",
				Paths:        []string{userHome + "/.gradle/caches"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name:         "Maven Cache",
				Paths:        []string{userHome + "/.m2/repository"},
				Risk:         Medium,
				Selected:     false,
				ShredEnabled: false,
			},
			{
				Name:         "Go Build Cache",
				Paths:        []string{userHome + "/.cache/go-build"},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			// Docker (NOTE: Better to use 'docker system prune' commands)
			{
				Name:         "Docker Temp Files",
				Paths:        []string{"/var/lib/docker/tmp"},
				Risk:         Low,
				Selected:     false,
				ShredEnabled: false,
			},
			{
				Name:         "Docker Container Logs",
				Paths:        []string{"/var/lib/docker/containers"},
				Risk:         Medium,
				Selected:     false,
				ShredEnabled: false,
				Filters:      []string{`\.log$`},
			},
			// System caches
			{
				Name:         "Flatpak Cache",
				Paths:        []string{userHome + "/.var/app"},
				Risk:         Medium,
				Selected:     false,
				ShredEnabled: false,
				Filters:      []string{`/cache/`, `/\.cache/`},
			},
			{
				Name:         "Systemd Journal",
				Paths:        []string{"/var/log/journal"},
				Risk:         Medium,
				Selected:     false,
				ShredEnabled: false,
				Filters:      []string{`\.journal$`},
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
