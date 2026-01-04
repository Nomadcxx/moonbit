package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Nomadcxx/moonbit/internal/config"
)

// Manager handles session cache operations
type Manager struct {
	cachePath string
}

// NewManager creates a new session cache manager
func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	cachePath := filepath.Join(homeDir, ".cache", "moonbit", "scan_results.json")
	return &Manager{cachePath: cachePath}, nil
}

// Path returns the cache file path
func (m *Manager) Path() string {
	return m.cachePath
}

// Save writes the session cache to disk
func (m *Manager) Save(cache *config.SessionCache) error {
	if cache == nil {
		return fmt.Errorf("cache cannot be nil")
	}

	cacheDir := filepath.Dir(m.cachePath)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(m.cachePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// Load reads the session cache from disk
func (m *Manager) Load() (*config.SessionCache, error) {
	data, err := os.ReadFile(m.cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache config.SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	return &cache, nil
}

// Clear removes the session cache file
func (m *Manager) Clear() error {
	if err := os.Remove(m.cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cache file: %w", err)
	}
	return nil
}

// Exists checks if the cache file exists
func (m *Manager) Exists() bool {
	_, err := os.Stat(m.cachePath)
	return err == nil
}
