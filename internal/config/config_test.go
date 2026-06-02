package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRiskLevel_String(t *testing.T) {
	tests := []struct {
		name     string
		risk     RiskLevel
		expected string
	}{
		{"Low", Low, "Low"},
		{"Medium", Medium, "Medium"},
		{"High", High, "High"},
		{"Unknown", RiskLevel(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.risk.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseRiskLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected RiskLevel
		wantErr  bool
	}{
		{"Low", "Low", Low, false},
		{"Medium", "Medium", Medium, false},
		{"High", "High", High, false},
		{"Invalid", "Invalid", 0, true},
		{"Empty", "", 0, true},
		{"Lowercase", "low", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseRiskLevel(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.NotNil(t, cfg)
	assert.NotNil(t, cfg.Categories)
	assert.Greater(t, len(cfg.Categories), 0)
	assert.Greater(t, cfg.Scan.MaxDepth, 0)
}

func TestLoad_NonExistent(t *testing.T) {
	// Use a temporary directory that doesn't exist
	tmpDir, err := os.MkdirTemp("", "moonbit-config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Try to load from non-existent path
	cfg, err := Load(filepath.Join(tmpDir, "nonexistent", "config.toml"))

	// Should create default config
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestLoadDefaultPathWithoutHomeEnv(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", "")
	t.Setenv("MOONBIT_HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg, err := Load("")

	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.FileExists(t, filepath.Join(tmpDir, ".config", "moonbit", "config.toml"))
}

func TestLoadNormalizesExistingConfigDefaults(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	configData := []byte(`
[scan]
max_depth = 3
ignore_patterns = []
enable_all = true
dry_run_default = true
worker_count = 0

[[categories]]
name = "Browser Cache (Safe)"
paths = ["/home/user/.cache/chromium"]
risk = 0
selected = true

[[categories]]
name = "WebKit Cache"
paths = ["/home/user/.cache/webkit"]
risk = 0
selected = true

[[categories]]
name = "Flatpak Cache"
paths = ["/home/user/.var/app/*/cache"]
risk = 1
selected = false

[[categories]]
name = "Custom Project Cache"
paths = ["/tmp/custom-project-cache"]
risk = 0
selected = true
`)
	require.NoError(t, os.WriteFile(configPath, configData, 0644))

	cfg, err := Load(configPath)

	require.NoError(t, err)
	names := categoryNames(cfg.Categories)
	assert.NotContains(t, names, "Browser Cache (Safe)")
	assert.NotContains(t, names, "WebKit Cache")
	assert.NotContains(t, names, "Flatpak Cache")
	assert.Contains(t, names, "Custom Project Cache")
	assert.Contains(t, names, "IDE App Caches")
	assert.Contains(t, names, "opencode Caches")
	assert.Contains(t, names, "Bottles App Cache")
}

func TestSessionCache(t *testing.T) {
	cache := &SessionCache{
		ScanResults: &Category{
			Name:  "Test",
			Files: []FileInfo{{Path: "/test", Size: 1024}},
		},
		TotalSize:  1024,
		TotalFiles: 1,
	}

	assert.NotNil(t, cache.ScanResults)
	assert.Equal(t, uint64(1024), cache.TotalSize)
	assert.Equal(t, 1, cache.TotalFiles)
}

func categoryNames(categories []Category) []string {
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		names = append(names, category.Name)
	}
	return names
}
