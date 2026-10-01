package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/paths"
)

const maxCacheSize int64 = 64 << 20

// Manager handles session cache operations
type Manager struct {
	cachePath string
}

// NewManager creates a new session cache manager
func NewManager() (*Manager, error) {
	cachePath, err := paths.CacheFile()
	if err != nil {
		return nil, fmt.Errorf("failed to determine session cache path: %w", err)
	}
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
	if err := paths.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := paths.AtomicWriteFile(m.cachePath, 0600, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	}); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// Root-run (sudo/pkexec) results must stay readable by the invoking user's
	// panel plugin; chown dir+file back to them.
	if uid, gid, ok := paths.OwnerID(); ok {
		_ = os.Chown(cacheDir, uid, gid)
		_ = os.Chown(m.cachePath, uid, gid)
	}

	return nil
}

// Load reads the session cache from disk
func (m *Manager) Load() (*config.SessionCache, error) {
	file, err := paths.OpenFile(m.cachePath, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat cache file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cache path is not a regular file: %s", m.cachePath)
	}
	if info.Size() > maxCacheSize {
		return nil, fmt.Errorf("cache file exceeds maximum size of 64 MiB: %s", m.cachePath)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCacheSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}
	if int64(len(data)) > maxCacheSize {
		return nil, fmt.Errorf("cache file exceeds maximum size of 64 MiB: %s", m.cachePath)
	}

	var cache config.SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	return &cache, nil
}

// Clear removes the session cache file
func (m *Manager) Clear() error {
	if err := paths.Remove(m.cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cache file: %w", err)
	}
	return nil
}

// Exists checks if the cache file exists
func (m *Manager) Exists() bool {
	file, err := paths.OpenFile(m.cachePath, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular()
}
