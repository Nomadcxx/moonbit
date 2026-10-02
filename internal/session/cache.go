package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/paths"
)

// maxCacheSize guards root against a runaway user-writable cache. A deep
// scan of a developer's home lists hundreds of thousands of files (a 2026
// bench scan: 428,078 entries, 134 MB as indented JSON), so the bound sits
// well above that.
const maxCacheSize int64 = 512 << 20

// summaryName is the per-category rollup saved beside the cache.
const summaryName = "scan_summary.json"

// Summary is the last scan rolled up per category, saved beside the cache so
// a desktop panel can show it without parsing every file entry.
type Summary struct {
	TotalSize  uint64            `json:"total_size"`
	TotalFiles int               `json:"total_files"`
	ScannedAt  time.Time         `json:"scanned_at"`
	Categories []CategorySummary `json:"categories"`
}

// CategorySummary is one category's share of a scan.
type CategorySummary struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes uint64 `json:"bytes"`
}

// Summarize rolls a cache up per category, in the order categories appear.
func Summarize(cache *config.SessionCache) Summary {
	s := Summary{TotalSize: cache.TotalSize, TotalFiles: cache.TotalFiles, ScannedAt: cache.ScannedAt, Categories: []CategorySummary{}}
	if cache.ScanResults == nil {
		return s
	}
	index := map[string]int{}
	for _, f := range cache.ScanResults.Files {
		i, ok := index[f.CategoryName]
		if !ok {
			i = len(s.Categories)
			index[f.CategoryName] = i
			s.Categories = append(s.Categories, CategorySummary{Name: f.CategoryName})
		}
		s.Categories[i].Files++
		s.Categories[i].Bytes += f.Size
	}
	return s
}

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

	// Compact: a deep scan's cache is mostly file entries, and indentation
	// alone added a third to its size.
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := paths.AtomicWriteFile(m.cachePath, 0600, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	}); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// A summary that disagrees with the cache would mislead the panel, so a
	// failed write removes the old one instead.
	summaryPath := filepath.Join(cacheDir, summaryName)
	summary, err := json.Marshal(Summarize(cache))
	if err == nil {
		err = paths.AtomicWriteFile(summaryPath, 0600, func(file *os.File) error {
			_, err := file.Write(summary)
			return err
		})
	}
	if err != nil {
		_ = paths.Remove(summaryPath)
	}

	// Root-run (sudo/pkexec) results must stay readable by the invoking user's
	// panel plugin; chown dir+files back to them.
	if uid, gid, ok := paths.OwnerID(); ok {
		_ = os.Chown(cacheDir, uid, gid)
		_ = os.Chown(m.cachePath, uid, gid)
		_ = os.Chown(summaryPath, uid, gid)
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
		return nil, fmt.Errorf("cache file exceeds maximum size of %d MiB: %s", maxCacheSize>>20, m.cachePath)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCacheSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}
	if int64(len(data)) > maxCacheSize {
		return nil, fmt.Errorf("cache file exceeds maximum size of %d MiB: %s", maxCacheSize>>20, m.cachePath)
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
	if err := paths.Remove(filepath.Join(filepath.Dir(m.cachePath), summaryName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove scan summary: %w", err)
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
