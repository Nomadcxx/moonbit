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

func TestScannerFilterLogic(t *testing.T) {
	// Test that ignore patterns are correctly filtered
	cfg := &config.Config{
		Scan: struct {
			MaxDepth       int      `toml:"max_depth"`
			IgnorePatterns []string `toml:"ignore_patterns"`
			EnableAll      bool     `toml:"enable_all"`
			DryRunDefault  bool     `toml:"dry_run_default"`
		}{
			IgnorePatterns: []string{"node_modules", "\\.git", "\\.cache"},
		},
	}

	s := NewScanner(cfg)

	// Test paths that should match ignore patterns
	assert.True(t, s.filter.MatchString("/home/user/project/node_modules/package"))
	assert.True(t, s.filter.MatchString("/home/user/.git/config"))
	assert.True(t, s.filter.MatchString("/home/user/.cache/file"))

	// Test paths that should NOT match ignore patterns
	assert.False(t, s.filter.MatchString("/home/user/project/src/main.go"))
	assert.False(t, s.filter.MatchString("/tmp/test.log"))
}

func TestShouldIncludeFile(t *testing.T) {
	cfg := &config.Config{
		Scan: struct {
			MaxDepth       int      `toml:"max_depth"`
			IgnorePatterns []string `toml:"ignore_patterns"`
			EnableAll      bool     `toml:"enable_all"`
			DryRunDefault  bool     `toml:"dry_run_default"`
		}{
			IgnorePatterns: []string{},
		},
	}

	s := NewScanner(cfg)

	// Create mock file info
	fileInfo := &mockFileInfo{name: "test.log", isDir: false}
	dirInfo := &mockFileInfo{name: "testdir", isDir: true}

	// Test 1: Directories should always be excluded
	assert.False(t, s.shouldIncludeFile("/tmp/testdir", dirInfo, nil))

	// Test 2: Files with no filters should be included
	assert.True(t, s.shouldIncludeFile("/tmp/test.log", fileInfo, nil))

	// Test 3: Files matching at least one filter should be included (OR logic)
	filters := []string{`\.log$`, `\.tmp$`}
	assert.True(t, s.shouldIncludeFile("/tmp/test.log", fileInfo, filters))
	
	fileInfo2 := &mockFileInfo{name: "test.tmp", isDir: false}
	assert.True(t, s.shouldIncludeFile("/tmp/test.tmp", fileInfo2, filters))

	// Test 4: Files not matching any filter should be excluded
	fileInfo3 := &mockFileInfo{name: "test.txt", isDir: false}
	assert.False(t, s.shouldIncludeFile("/tmp/test.txt", fileInfo3, filters))

	// Test 5: Multiple filters with OR logic
	filters2 := []string{`\.log$`, `\.txt$`, `\.bak$`}
	assert.True(t, s.shouldIncludeFile("/tmp/test.log", fileInfo, filters2))
	assert.True(t, s.shouldIncludeFile("/tmp/test.txt", fileInfo3, filters2))
	
	fileInfo4 := &mockFileInfo{name: "test.bak", isDir: false}
	assert.True(t, s.shouldIncludeFile("/tmp/test.bak", fileInfo4, filters2))
	
	fileInfo5 := &mockFileInfo{name: "test.dat", isDir: false}
	assert.False(t, s.shouldIncludeFile("/tmp/test.dat", fileInfo5, filters2))
}

// mockFileInfo implements os.FileInfo for testing
type mockFileInfo struct {
	name  string
	isDir bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return 1024 }
func (m *mockFileInfo) Mode() os.FileMode  { return 0644 }
func (m *mockFileInfo) ModTime() time.Time { return time.Now() }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Sys() interface{}   { return nil }
