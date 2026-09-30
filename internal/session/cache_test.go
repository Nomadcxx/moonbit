package session

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err)
	assert.NotNil(t, manager)
	assert.NotEmpty(t, manager.cachePath)
	assert.Contains(t, manager.cachePath, ".cache")
	assert.Contains(t, manager.cachePath, "moonbit")
	assert.Contains(t, manager.cachePath, "scan_results.json")
}

func TestNewManagerWithoutHomeEnv(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", "")
	t.Setenv("MOONBIT_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", "")

	manager, err := NewManager()

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmpDir, ".cache", "moonbit", "scan_results.json"), manager.Path())
}

func TestManager_Path(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err)

	path := manager.Path()
	assert.Equal(t, manager.cachePath, path)
}

func TestManager_SaveAndLoad(t *testing.T) {
	// Use a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "moonbit-session-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Override cache path for testing
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	manager, err := NewManager()
	require.NoError(t, err)

	// Create test cache
	testCache := &config.SessionCache{
		ScanResults: &config.Category{
			Name:  "Test Category",
			Files: []config.FileInfo{{Path: "/test/file.txt", Size: 1024}},
		},
		TotalSize:  1024,
		TotalFiles: 1,
		ScannedAt:  time.Now(),
	}

	// Test Save
	err = manager.Save(testCache)
	require.NoError(t, err)

	// Verify file exists
	assert.True(t, manager.Exists())

	// Test Load
	loadedCache, err := manager.Load()
	require.NoError(t, err)
	assert.NotNil(t, loadedCache)
	assert.Equal(t, testCache.TotalSize, loadedCache.TotalSize)
	assert.Equal(t, testCache.TotalFiles, loadedCache.TotalFiles)
	assert.Equal(t, testCache.ScanResults.Name, loadedCache.ScanResults.Name)
	assert.Len(t, loadedCache.ScanResults.Files, 1)
	assert.Equal(t, testCache.ScanResults.Files[0].Path, loadedCache.ScanResults.Files[0].Path)
}

func TestManager_Save_NilCache(t *testing.T) {
	manager, err := NewManager()
	require.NoError(t, err)

	err = manager.Save(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
}

func TestManager_Load_NonExistent(t *testing.T) {
	// Use a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "moonbit-session-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	manager, err := NewManager()
	require.NoError(t, err)

	// Try to load non-existent cache
	_, err = manager.Load()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read cache file")
}

func TestManager_LoadRejectsOversizedCache(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	manager, err := NewManager()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(manager.Path()), 0700))

	file, err := os.Create(manager.Path())
	require.NoError(t, err)
	require.NoError(t, file.Truncate((64<<20)+1))
	require.NoError(t, file.Close())

	_, err = manager.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum size")
}

func TestManager_Clear(t *testing.T) {
	// Use a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "moonbit-session-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	manager, err := NewManager()
	require.NoError(t, err)

	// Create and save a cache
	testCache := &config.SessionCache{
		ScanResults: &config.Category{Name: "Test"},
		TotalSize:   0,
		TotalFiles:  0,
		ScannedAt:   time.Now(),
	}

	err = manager.Save(testCache)
	require.NoError(t, err)
	assert.True(t, manager.Exists())

	// Clear the cache
	err = manager.Clear()
	require.NoError(t, err)
	assert.False(t, manager.Exists())

	// Clearing non-existent cache should not error
	err = manager.Clear()
	assert.NoError(t, err)
}

func TestManager_Exists(t *testing.T) {
	// Use a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "moonbit-session-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	manager, err := NewManager()
	require.NoError(t, err)

	// Initially doesn't exist
	assert.False(t, manager.Exists())

	// Create cache
	testCache := &config.SessionCache{
		ScanResults: &config.Category{Name: "Test"},
		TotalSize:   0,
		TotalFiles:  0,
		ScannedAt:   time.Now(),
	}

	err = manager.Save(testCache)
	require.NoError(t, err)

	// Now exists
	assert.True(t, manager.Exists())
}

func TestManager_Save_CreatesDirectory(t *testing.T) {
	// Use a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "moonbit-session-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	manager, err := NewManager()
	require.NoError(t, err)

	// Remove cache directory if it exists
	cacheDir := filepath.Dir(manager.cachePath)
	os.RemoveAll(cacheDir)

	// Save should create the directory
	testCache := &config.SessionCache{
		ScanResults: &config.Category{Name: "Test"},
		TotalSize:   0,
		TotalFiles:  0,
		ScannedAt:   time.Now(),
	}

	err = manager.Save(testCache)
	require.NoError(t, err)

	// Directory should exist
	_, err = os.Stat(cacheDir)
	assert.NoError(t, err)
}

func TestManager_SaveRejectsSymlinkedCacheDirectory(t *testing.T) {
	base := t.TempDir()
	cacheHome := filepath.Join(base, "xdg-cache")
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(cacheHome, 0700))
	require.NoError(t, os.Mkdir(outside, 0700))
	require.NoError(t, os.Symlink(outside, filepath.Join(cacheHome, "moonbit")))
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	manager, err := NewManager()
	require.NoError(t, err)
	err = manager.Save(&config.SessionCache{ScanResults: &config.Category{Name: "Test"}})
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(outside, "scan_results.json"))
}

func TestManager_SaveRejectsFIFO(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "xdg-cache"))
	manager, err := NewManager()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(manager.Path()), 0700))
	require.NoError(t, syscall.Mkfifo(manager.Path(), 0600))
	reader, err := os.OpenFile(manager.Path(), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	require.NoError(t, err)
	defer reader.Close()

	err = manager.Save(&config.SessionCache{ScanResults: &config.Category{Name: "Test"}})
	require.Error(t, err)
	buf := make([]byte, 4096)
	n, _ := reader.Read(buf)
	assert.Zero(t, n, "cache contents must not be written to a FIFO")
}

func TestManager_LoadRejectsSymlinkedCacheFile(t *testing.T) {
	base := t.TempDir()
	cacheHome := filepath.Join(base, "xdg-cache")
	outside := filepath.Join(base, "external.json")
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	manager, err := NewManager()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(manager.Path()), 0700))
	require.NoError(t, os.WriteFile(outside, []byte(`{"scan_results":{"name":"external"}}`), 0600))
	require.NoError(t, os.Symlink(outside, manager.Path()))

	_, err = manager.Load()
	require.Error(t, err)
}

func TestManager_ClearDoesNotFollowSymlinkedCacheDirectory(t *testing.T) {
	base := t.TempDir()
	cacheHome := filepath.Join(base, "xdg-cache")
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(cacheHome, 0700))
	require.NoError(t, os.Mkdir(outside, 0700))
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	manager, err := NewManager()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(outside, "scan_results.json"), []byte("precious"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(cacheHome, "moonbit")))

	err = manager.Clear()
	require.Error(t, err)
	assert.FileExists(t, filepath.Join(outside, "scan_results.json"))
}

func TestManager_ExistsRejectsSymlinkedCacheFile(t *testing.T) {
	base := t.TempDir()
	cacheHome := filepath.Join(base, "xdg-cache")
	outside := filepath.Join(base, "external.json")
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	manager, err := NewManager()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(manager.Path()), 0700))
	require.NoError(t, os.WriteFile(outside, []byte(`{"scan_results":{"name":"external"}}`), 0600))
	require.NoError(t, os.Symlink(outside, manager.Path()))

	assert.False(t, manager.Exists())
}
