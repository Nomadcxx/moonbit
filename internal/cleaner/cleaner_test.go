package cleaner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewCleaner(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	assert.NotNil(t, c)
	assert.Equal(t, cfg, c.cfg)
	assert.NotNil(t, c.safetyConfig)
	assert.True(t, c.backupEnabled)
	assert.True(t, c.safetyConfig.SafeMode)
}

func TestGetDefaultSafetyConfig(t *testing.T) {
	safetyCfg := GetDefaultSafetyConfig()

	assert.NotNil(t, safetyCfg)
	assert.True(t, safetyCfg.RequireConfirmation)
	assert.True(t, safetyCfg.SafeMode)
	assert.Equal(t, uint64(1024), safetyCfg.MaxDeletionSize)
	assert.Greater(t, len(safetyCfg.ProtectedPaths), 0)
}

func TestIsProtectedPath(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"System bin", "/bin/ls", true},
		{"User bin", "/usr/bin/cat", true},
		{"Etc config", "/etc/passwd", true},
		{"Var lib", "/var/lib/mysql", true},
		{"Temp file", "/tmp/test.txt", false},
		{"Home cache", "/home/user/.cache/test", false},
		{"Var tmp", "/var/tmp/test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.isProtectedPath(tt.path)
			assert.Equal(t, tt.expected, result, "Path: %s", tt.path)
		})
	}
}

func TestPerformSafetyChecks(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	t.Run("Safe category", func(t *testing.T) {
		category := &config.Category{
			Name: "Test",
			Files: []config.FileInfo{
				{Path: "/tmp/test.txt", Size: 1024},
			},
			Size: 1024,
			Risk: config.Low,
		}

		err := c.performSafetyChecks(category, false)
		assert.NoError(t, err)
	})

	t.Run("High risk category in safe mode", func(t *testing.T) {
		category := &config.Category{
			Name: "Risky",
			Files: []config.FileInfo{
				{Path: "/tmp/important.txt", Size: 1024},
			},
			Size: 1024,
			Risk: config.High,
		}

		err := c.performSafetyChecks(category, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "high-risk")
	})

	t.Run("Protected path", func(t *testing.T) {
		category := &config.Category{
			Name: "Protected",
			Files: []config.FileInfo{
				{Path: "/bin/ls", Size: 1024},
			},
			Size: 1024,
			Risk: config.Low,
		}

		err := c.performSafetyChecks(category, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "protected path")
	})

	t.Run("Size too large", func(t *testing.T) {
		category := &config.Category{
			Name: "Large",
			Files: []config.FileInfo{
				{Path: "/tmp/huge.txt", Size: 2 * 1024 * 1024 * 1024},
			},
			Size: 2 * 1024 * 1024 * 1024,
			Risk: config.Low,
		}

		err := c.performSafetyChecks(category, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum")
	})

	t.Run("Dry run bypasses some checks", func(t *testing.T) {
		category := &config.Category{
			Name: "Test",
			Files: []config.FileInfo{
				{Path: "/tmp/test.txt", Size: 1024},
			},
			Size: 1024,
			Risk: config.High,
		}

		err := c.performSafetyChecks(category, true)
		// Dry run still checks size and protected paths but not risk
		assert.NoError(t, err)
	})
}

func TestCleanCategoryDryRun(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	// Create test category
	category := &config.Category{
		Name: "Test",
		Files: []config.FileInfo{
			{Path: "/tmp/test1.txt", Size: 1024},
			{Path: "/tmp/test2.txt", Size: 2048},
		},
		Size:     3072,
		Risk:     config.Low,
		Selected: true,
	}

	progressCh := make(chan CleanMsg, 10)
	ctx := context.Background()

	go c.CleanCategory(ctx, category, true, progressCh)

	var complete *CleanComplete
	for msg := range progressCh {
		if msg.Complete != nil {
			complete = msg.Complete
			break
		}
	}

	assert.NotNil(t, complete)
	assert.Equal(t, 2, complete.FilesDeleted)
	assert.Equal(t, uint64(3072), complete.BytesFreed)
	assert.Empty(t, complete.Errors)
}

func TestCleanCategoryWithRealFiles(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	// Create temporary test files
	tempDir := t.TempDir()
	file1 := filepath.Join(tempDir, "test1.txt")
	file2 := filepath.Join(tempDir, "test2.txt")

	os.WriteFile(file1, []byte("test content 1"), 0644)
	os.WriteFile(file2, []byte("test content 2"), 0644)

	// Verify files exist
	_, err := os.Stat(file1)
	assert.NoError(t, err)

	// Create category
	category := &config.Category{
		Name: "Test",
		Files: []config.FileInfo{
			{Path: file1, Size: 14},
			{Path: file2, Size: 14},
		},
		Size:     28,
		Risk:     config.Low,
		Selected: true,
	}

	progressCh := make(chan CleanMsg, 10)
	ctx := context.Background()

	go c.CleanCategory(ctx, category, false, progressCh)

	var complete *CleanComplete
	for msg := range progressCh {
		if msg.Complete != nil {
			complete = msg.Complete
			break
		}
	}

	assert.NotNil(t, complete)
	assert.Equal(t, 2, complete.FilesDeleted)
	assert.Equal(t, uint64(28), complete.BytesFreed)

	// Verify files are deleted
	_, err = os.Stat(file1)
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(file2)
	assert.True(t, os.IsNotExist(err))
}

func TestCleanProgressStruct(t *testing.T) {
	progress := CleanProgress{
		FilesProcessed: 10,
		BytesFreed:     1024000,
		CurrentFile:    "/tmp/test.txt",
		TotalFiles:     100,
		TotalBytes:     10240000,
	}

	assert.Equal(t, 10, progress.FilesProcessed)
	assert.Equal(t, uint64(1024000), progress.BytesFreed)
	assert.Equal(t, "/tmp/test.txt", progress.CurrentFile)
}

func TestCleanCompleteStruct(t *testing.T) {
	complete := CleanComplete{
		Category:      "Test Category",
		FilesDeleted:  50,
		BytesFreed:    5120000,
		Duration:      5 * time.Second,
		BackupCreated: true,
		BackupPath:    "/backup/test.tar.gz",
		Errors:        []string{"error1", "error2"},
	}

	assert.Equal(t, "Test Category", complete.Category)
	assert.Equal(t, 50, complete.FilesDeleted)
	assert.True(t, complete.BackupCreated)
	assert.Len(t, complete.Errors, 2)
}

func TestDeleteFile(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	t.Run("Delete regular file", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test.txt")
		os.WriteFile(testFile, []byte("test"), 0644)

		err := c.deleteFile(testFile, false)
		assert.NoError(t, err)

		_, err = os.Stat(testFile)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("Protected path rejection", func(t *testing.T) {
		err := c.deleteFile("/bin/ls", false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "protected path")
	})

	t.Run("Nonexistent file", func(t *testing.T) {
		err := c.deleteFile("/tmp/nonexistent.txt", false)
		assert.Error(t, err)
	})
}
