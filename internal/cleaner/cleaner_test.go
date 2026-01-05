package cleaner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCleaner(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	assert.NotNil(t, c)
	assert.Equal(t, cfg, c.cfg)
	assert.NotNil(t, c.safetyConfig)
	assert.False(t, c.backupEnabled) // Disabled for performance
	assert.True(t, c.safetyConfig.SafeMode)
}

func TestGetDefaultSafetyConfig(t *testing.T) {
	safetyCfg := GetDefaultSafetyConfig()

	assert.NotNil(t, safetyCfg)
	assert.True(t, safetyCfg.RequireConfirmation)
	assert.True(t, safetyCfg.SafeMode)
	assert.Equal(t, uint64(512000), safetyCfg.MaxDeletionSize)
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
		{"Var lib", "/var/lib/mysql", false}, // /var/lib removed to allow Docker cleanup
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
		assert.Contains(t, err.Error(), "protected")
	})

	t.Run("Size too large", func(t *testing.T) {
		category := &config.Category{
			Name: "Large",
			Files: []config.FileInfo{
				{Path: "/tmp/huge.txt", Size: 600 * 1024 * 1024 * 1024},
			},
			Size: 600 * 1024 * 1024 * 1024,
			Risk: config.Low,
		}

		err := c.performSafetyChecks(category, false)
		assert.Error(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "exceeds maximum")
		}
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
		assert.Contains(t, err.Error(), "protected")
	})

	t.Run("Nonexistent file", func(t *testing.T) {
		err := c.deleteFile("/tmp/nonexistent.txt", false)
		assert.Error(t, err)
	})
}

func TestShredFile(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	t.Run("Shred regular file", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "test.txt")
		content := []byte("sensitive data that should be shredded")
		os.WriteFile(testFile, content, 0644)

		info, err := os.Stat(testFile)
		require.NoError(t, err)
		originalSize := info.Size()

		err = c.shredFile(testFile, originalSize)
		assert.NoError(t, err)

		// File should still exist (shredFile only overwrites, doesn't delete)
		info, err = os.Stat(testFile)
		assert.NoError(t, err)
		assert.Equal(t, originalSize, info.Size())

		// Verify file content was overwritten (should be random data, not original)
		shreddedContent, err := os.ReadFile(testFile)
		assert.NoError(t, err)
		assert.NotEqual(t, content, shreddedContent) // Content should be different
		assert.Equal(t, originalSize, int64(len(shreddedContent)))
	})

	t.Run("Shred nonexistent file", func(t *testing.T) {
		err := c.shredFile("/tmp/nonexistent.txt", 1024)
		assert.Error(t, err)
	})
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Simple name", "Test", "Test"},
		{"With spaces", "Test Category", "Test_Category"},
		{"With slashes", "Test/Category", "Test_Category"},
		{"With backslashes", "Test\\Category", "Test_Category"},
		{"Complex", "Test/Category Name\\Path", "Test_Category_Name_Path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateBackup(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	// Enable backup for testing
	c.backupEnabled = true

	tempDir := t.TempDir()
	testFile1 := filepath.Join(tempDir, "test1.txt")
	testFile2 := filepath.Join(tempDir, "test2.txt")

	os.WriteFile(testFile1, []byte("test content 1"), 0644)
	os.WriteFile(testFile2, []byte("test content 2"), 0644)

	category := &config.Category{
		Name: "Test Category",
		Files: []config.FileInfo{
			{Path: testFile1, Size: 14},
			{Path: testFile2, Size: 14},
		},
		Size: 28,
	}

	// Override backup directory to use temp dir
	originalDataHome := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", originalDataHome)

	os.Setenv("XDG_DATA_HOME", tempDir)

	backupPath := c.createBackup(category)
	assert.NotEmpty(t, backupPath)
	assert.Contains(t, backupPath, "Test_Category")
	assert.Contains(t, backupPath, ".backup")

	// Verify backup files directory exists
	backupFilesDir := backupPath + ".files"
	_, err := os.Stat(backupFilesDir)
	assert.NoError(t, err)

	// Verify metadata file exists
	metaPath := backupPath + ".json"
	_, err = os.Stat(metaPath)
	assert.NoError(t, err)
}

func TestCreateBackupMetadata(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	tempDir := t.TempDir()
	backupPath := filepath.Join(tempDir, "test.backup")

	category := &config.Category{
		Name: "Test",
		Files: []config.FileInfo{
			{Path: "/tmp/test.txt", Size: 1024},
		},
		Size: 1024,
	}

	err := c.createBackupMetadata(backupPath, category, "20250105_120000")
	assert.NoError(t, err)

	// Verify metadata file was created
	metaPath := backupPath + ".json"
	data, err := os.ReadFile(metaPath)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "Test")
	assert.Contains(t, string(data), "20250105_120000")
}

func TestBackupFile(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	tempDir := t.TempDir()
	srcFile := filepath.Join(tempDir, "source.txt")
	backupDir := filepath.Join(tempDir, "backup")

	content := []byte("test backup content")
	os.WriteFile(srcFile, content, 0644)
	os.MkdirAll(backupDir, 0755)

	err := c.backupFile(srcFile, backupDir)
	assert.NoError(t, err)

	// Verify backup file exists (hashed name)
	files, err := os.ReadDir(backupDir)
	assert.NoError(t, err)
	assert.Greater(t, len(files), 0)

	// Verify content matches
	backupFile := filepath.Join(backupDir, files[0].Name())
	backupContent, err := os.ReadFile(backupFile)
	assert.NoError(t, err)
	assert.Equal(t, content, backupContent)
}

func TestListBackups(t *testing.T) {
	tempDir := t.TempDir()

	// Override data home
	originalDataHome := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", originalDataHome)

	os.Setenv("XDG_DATA_HOME", tempDir)

	backupDir := filepath.Join(tempDir, "moonbit", "backups")
	os.MkdirAll(backupDir, 0755)

	// Create some backup files
	backup1 := filepath.Join(backupDir, "test1_20250105.backup")
	backup2 := filepath.Join(backupDir, "test2_20250105.backup")
	os.WriteFile(backup1, []byte("backup1"), 0644)
	os.WriteFile(backup2, []byte("backup2"), 0644)

	backups, err := ListBackups()
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(backups), 2)
}

func TestRestoreBackup(t *testing.T) {
	tempDir := t.TempDir()

	// Create backup structure
	backupPath := filepath.Join(tempDir, "test.backup")
	backupFilesDir := backupPath + ".files"
	os.MkdirAll(backupFilesDir, 0755)

	// Create test file to backup
	testFile := filepath.Join(tempDir, "original.txt")
	content := []byte("original content")
	os.WriteFile(testFile, content, 0644)

	// Create backup file (hashed name)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(testFile)))
	backupFile := filepath.Join(backupFilesDir, hash[:16])
	os.WriteFile(backupFile, content, 0644)

	// Create metadata
	metadata := map[string]interface{}{
		"created_at": time.Now().Format(time.RFC3339),
		"timestamp":  "20250105_120000",
		"category":   "Test",
		"file_count": 1,
		"total_size": int64(len(content)),
		"files": []config.FileInfo{
			{Path: testFile, Size: uint64(len(content))},
		},
	}

	metaData, _ := json.MarshalIndent(metadata, "", "  ")
	metaPath := backupPath + ".json"
	os.WriteFile(metaPath, metaData, 0644)

	// Delete original file
	os.Remove(testFile)

	// Restore backup
	err := RestoreBackup(backupPath)
	assert.NoError(t, err)

	// Verify file was restored
	restoredContent, err := os.ReadFile(testFile)
	assert.NoError(t, err)
	assert.Equal(t, content, restoredContent)
}
