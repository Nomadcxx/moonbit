package scanner

import (
	"os"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewScanner(t *testing.T) {
	cfg := &config.Config{
		Scan: struct {
			MaxDepth       int      `toml:"max_depth"`
			IgnorePatterns []string `toml:"ignore_patterns"`
			EnableAll      bool     `toml:"enable_all"`
			DryRunDefault  bool     `toml:"dry_run_default"`
		}{
			IgnorePatterns: []string{"node_modules", ".git", ".svn", ".hg"},
		},
	}

	s := NewScanner(cfg)

	assert.NotNil(t, s)
	assert.Equal(t, cfg, s.cfg)
	assert.NotNil(t, s.filter)
	assert.Equal(t, 4, s.workers)
}

func TestExpandPathPattern(t *testing.T) {
	// Test with absolute paths that don't match (returns original pattern)
	result, err := expandPathPattern("/tmp/test/*")
	assert.NoError(t, err)
	assert.Contains(t, result, "/tmp/test/*") // Should return original pattern when no matches

	// Test with pattern that doesn't match
	result, err = expandPathPattern("/nonexistent/*")
	assert.NoError(t, err)
	assert.Contains(t, result, "/nonexistent/*")
}

func TestGetDefaultPaths(t *testing.T) {
	paths := GetDefaultPaths()

	assert.NotEmpty(t, paths)
	assert.Contains(t, paths, "/tmp")
	assert.Contains(t, paths, "/var/tmp")

	// Check for home directory path
	homeDir := os.Getenv("HOME")
	assert.Contains(t, paths, homeDir+"/.cache")
}

func TestScanProgressStruct(t *testing.T) {
	// Test that ScanProgress can be created and used
	progress := ScanProgress{
		Path:         "/tmp/test.log",
		Bytes:        1024,
		FilesScanned: 10,
		DirsScanned:  5,
		CurrentDir:   "/tmp",
	}

	assert.Equal(t, "/tmp/test.log", progress.Path)
	assert.Equal(t, uint64(1024), progress.Bytes)
	assert.Equal(t, 10, progress.FilesScanned)
}

func TestScanCompleteStruct(t *testing.T) {
	// Test that ScanComplete can be created and used
	category := &config.Category{
		Name:  "Test",
		Paths: []string{"/tmp"},
	}

	complete := ScanComplete{
		Category: "Test Category",
		Stats:    category,
		Duration: 2 * time.Second,
	}

	assert.Equal(t, "Test Category", complete.Category)
	assert.Equal(t, category, complete.Stats)
	assert.Equal(t, 2*time.Second, complete.Duration)
}

func TestConfigValidation(t *testing.T) {
	cfg := config.DefaultConfig()

	// Test default configuration is valid
	err := cfg.Validate()
	assert.NoError(t, err)

	// Test invalid configuration
	invalidCfg := &config.Config{}
	invalidCfg.Scan.MaxDepth = 15                 // Invalid value
	invalidCfg.Categories = []config.Category{{}} // Empty category name

	err = invalidCfg.Validate()
	assert.Error(t, err)
}
