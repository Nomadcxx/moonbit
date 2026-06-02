package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Nomadcxx/moonbit/internal/paths"
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

// MarshalJSON implements json.Marshaler for RiskLevel
func (r RiskLevel) MarshalJSON() ([]byte, error) {
	return []byte(`"` + r.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler for RiskLevel
func (r *RiskLevel) UnmarshalJSON(data []byte) error {
	// Remove quotes
	s := string(data)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	parsed, err := ParseRiskLevel(s)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
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
	Path             string    `json:"path"`
	Size             uint64    `json:"size"`
	ModTime          string    `json:"mod_time"`
	CategoryName     string    `json:"category_name,omitempty"`
	CategoryRisk     RiskLevel `json:"category_risk,omitempty"`
	CategorySelected bool      `json:"category_selected,omitempty"`
}

// Category represents a cleaning category
type Category struct {
	Name            string     `toml:"name" json:"name"`
	Paths           []string   `toml:"paths" json:"paths,omitempty"`
	Filters         []string   `toml:"filters" json:"filters,omitempty"`
	ExcludePatterns []string   `toml:"exclude_patterns" json:"exclude_patterns,omitempty"`
	Risk            RiskLevel  `toml:"risk" json:"risk"`
	Size            uint64     `toml:"size,omitempty" json:"size,omitempty"`
	FileCount       int        `toml:"file_count,omitempty" json:"file_count,omitempty"`
	Files           []FileInfo `toml:"files,omitempty" json:"files,omitempty"`
	Selected        bool       `toml:"selected,omitempty" json:"selected,omitempty"`
	ShredEnabled    bool       `toml:"shred,omitempty" json:"shred,omitempty"`
	MinAgeDays      int        `toml:"min_age_days,omitempty" json:"min_age_days,omitempty"` // Only clean files older than this many days
}

// Config represents the main configuration
type Config struct {
	Scan struct {
		MaxDepth       int      `toml:"max_depth"`
		IgnorePatterns []string `toml:"ignore_patterns"`
		EnableAll      bool     `toml:"enable_all"`
		DryRunDefault  bool     `toml:"dry_run_default"`
		WorkerCount    int      `toml:"worker_count"` // Number of parallel workers (0 = auto-detect)
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
	home, err := paths.HomeDir()
	if err == nil {
		return home
	}
	return "/root"
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
			WorkerCount    int      `toml:"worker_count"`
		}{
			MaxDepth:       3, // Deeper scanning for comprehensive detection
			IgnorePatterns: []string{"node_modules", ".git", ".svn", ".hg"},
			EnableAll:      true,
			DryRunDefault:  true,
			WorkerCount:    0, // 0 = auto-detect based on CPU count
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
				Name: "Yay Cache",
				Paths: []string{
					userHome + "/.cache/yay",
					userHome + "/.cache/yay/*",
					userHome + "/.config/yay/cache",
				},
				Risk:         Low,
				Selected:     true,
				ShredEnabled: false,
			},
			{
				Name: "Paru Cache",
				Paths: []string{
					userHome + "/.cache/paru",
					userHome + "/.cache/paru/clone",
					userHome + "/.config/paru/cache",
				},
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
				Name:            "User Cache",
				Paths:           []string{userHome + "/.cache"},
				Risk:            Low,
				Selected:        true,
				ShredEnabled:    false,
				Filters:         []string{`\.(tmp|temp|bak|backup)$`, `(^|/)(cache|thumbnails|applications|dotnet|gstreamer-1\.0|recently-used\.xbel)$`},
				ExcludePatterns: protectedCachePatterns(),
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
				Name: "pip Cache",
				Paths: []string{
					userHome + "/.cache/pip",
					userHome + "/.local/share/pip",
					userHome + "/.pip/cache",
				},
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
				Name: "Cargo Cache",
				Paths: []string{
					userHome + "/.cargo/registry/cache",
					userHome + "/.cargo/registry/index",
					userHome + "/.cargo/git/db",
				},
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
				Name: "Maven Cache",
				Paths: []string{
					userHome + "/.m2/repository",
					userHome + "/.m2/wrapper",
				},
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
				Name:         "Systemd Journal",
				Paths:        []string{"/var/log/journal"},
				Risk:         Medium,
				Selected:     false,
				ShredEnabled: false,
				Filters:      []string{`\.journal$`},
			},
		},
	}
	cfg.Categories = append(cfg.Categories, AppCacheCategories(userHome)...)
	return cfg
}

// Load loads configuration from file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		// Use default config path
		configPath, err := paths.ConfigFile()
		if err != nil {
			return nil, fmt.Errorf("failed to determine config path: %w", err)
		}
		path = configPath
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
	cfg.Normalize()

	return cfg, nil
}

func (cfg *Config) Normalize() {
	deprecatedDefaults := map[string]bool{
		"Browser Cache (Safe)": true,
		"WebKit Cache":         true,
		"Flatpak Cache":        true,
	}

	normalized := make([]Category, 0, len(cfg.Categories))
	seen := make(map[string]bool)
	for _, category := range cfg.Categories {
		if deprecatedDefaults[category.Name] {
			continue
		}
		if category.Name == "User Cache" {
			category.ExcludePatterns = mergeStrings(category.ExcludePatterns, protectedCachePatterns())
			for i, filter := range category.Filters {
				category.Filters[i] = strings.ReplaceAll(filter, "browsers|", "")
				category.Filters[i] = strings.ReplaceAll(category.Filters[i], "|browsers", "")
			}
		}
		normalized = append(normalized, category)
		seen[category.Name] = true
	}

	for _, category := range AppCacheCategories(getRealUserHome()) {
		if seen[category.Name] {
			continue
		}
		normalized = append(normalized, category)
		seen[category.Name] = true
	}

	cfg.Categories = normalized
}

func mergeStrings(existing, additions []string) []string {
	seen := make(map[string]bool, len(existing)+len(additions))
	merged := make([]string, 0, len(existing)+len(additions))
	for _, value := range existing {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		merged = append(merged, value)
	}
	for _, value := range additions {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		merged = append(merged, value)
	}
	return merged
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
