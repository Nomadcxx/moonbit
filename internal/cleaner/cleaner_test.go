package cleaner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type restoreReadError struct{}

func (restoreReadError) Read([]byte) (int, error) { return 0, errors.New("restore source failed") }

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
	t.Setenv("SUDO_USER", "")
	t.Setenv("PKEXEC_UID", "")
	t.Setenv("MOONBIT_HOME", "/home/user")
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

func TestCleanCategoryReportsTruncatedFilesSeparately(t *testing.T) {
	c := NewCleaner(config.DefaultConfig())
	path := filepath.Join(t.TempDir(), "active.log")
	require.NoError(t, os.WriteFile(path, []byte("daemon output"), 0600))

	category := &config.Category{
		Name: "Logs",
		Files: []config.FileInfo{{
			Path: path, Size: uint64(len("daemon output")), CategoryAction: config.ActionTruncate,
		}},
		Risk: config.Low,
	}

	progress := make(chan CleanMsg, 10)
	go c.CleanCategory(context.Background(), category, false, progress)

	var complete *CleanComplete
	for msg := range progress {
		if msg.Complete != nil {
			complete = msg.Complete
		}
	}
	require.NotNil(t, complete)
	assert.Equal(t, 0, complete.FilesDeleted)
	assert.Equal(t, 1, complete.FilesTruncated)
	assert.Equal(t, uint64(len("daemon output")), complete.BytesFreed)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Zero(t, info.Size())
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

func TestCleanCategoryReturnsPartialFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	c := NewCleaner(cfg)

	tempDir := t.TempDir()
	missingFile := filepath.Join(tempDir, "missing.txt")

	category := &config.Category{
		Name: "Partial Failure",
		Files: []config.FileInfo{
			{Path: missingFile, Size: 10},
		},
		Size: 10,
		Risk: config.Low,
	}

	progressCh := make(chan CleanMsg, 10)
	err := c.CleanCategory(context.Background(), category, false, progressCh)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
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

		freed, err := c.deleteFile(testFile, false)
		assert.NoError(t, err)
		assert.Equal(t, uint64(4), freed, "should report the bytes actually reclaimed")

		_, err = os.Stat(testFile)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("Protected path rejection", func(t *testing.T) {
		_, err := c.deleteFile("/bin/ls", false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "protected")
	})

	t.Run("Nonexistent file", func(t *testing.T) {
		_, err := c.deleteFile("/tmp/nonexistent.txt", false)
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
	backupDir, err := paths.DataDir("backups")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	backupPath := c.createBackup(category)
	assert.NotEmpty(t, backupPath)
	assert.Contains(t, backupPath, "Test_Category")
	assert.Contains(t, backupPath, ".backup")

	// Verify backup files directory exists
	backupFilesDir := backupPath + ".files"
	filesDirInfo, err := os.Stat(backupFilesDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), filesDirInfo.Mode().Perm())
	backupDirInfo, err := os.Stat(backupDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), backupDirInfo.Mode().Perm())
	backupFiles, err := os.ReadDir(backupFilesDir)
	require.NoError(t, err)
	require.NotEmpty(t, backupFiles)
	backupFileInfo, err := backupFiles[0].Info()
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), backupFileInfo.Mode().Perm())

	// Verify metadata file exists
	metaPath := backupPath + ".json"
	metaInfo, err := os.Stat(metaPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), metaInfo.Mode().Perm())

	backups, err := ListBackups()
	require.NoError(t, err)
	assert.Contains(t, backups, filepath.Base(backupPath))
}

func TestCleanCategoryAbortsWhenBackupFileFails(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))

	firstPath := filepath.Join(base, "first.txt")
	secondTarget := filepath.Join(base, "second-target.txt")
	secondPath := filepath.Join(base, "second-link.txt")
	firstContent := []byte("keep until the whole backup succeeds")
	require.NoError(t, os.WriteFile(firstPath, firstContent, 0600))
	require.NoError(t, os.WriteFile(secondTarget, []byte("target"), 0600))
	require.NoError(t, os.Symlink(secondTarget, secondPath))

	c := NewCleaner(config.DefaultConfig())
	c.backupEnabled = true
	category := &config.Category{
		Name: "Backup failure",
		Files: []config.FileInfo{
			{Path: firstPath, Size: uint64(len(firstContent))},
			{Path: secondPath, Size: uint64(len("target"))},
		},
		Risk: config.Low,
	}

	progress := make(chan CleanMsg, 8)
	err := c.CleanCategory(context.Background(), category, false, progress)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup creation failed")

	got, readErr := os.ReadFile(firstPath)
	require.NoError(t, readErr)
	assert.Equal(t, firstContent, got, "cleaning must not start after any backup copy fails")
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

func TestCreateBackupMetadataRejectsFIFO(t *testing.T) {
	backupPath := filepath.Join(t.TempDir(), "test.backup")
	metaPath := backupPath + ".json"
	require.NoError(t, syscall.Mkfifo(metaPath, 0600))
	reader, err := os.OpenFile(metaPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	require.NoError(t, err)
	defer reader.Close()

	c := &Cleaner{}
	err = c.createBackupMetadata(backupPath, &config.Category{Name: "Test"}, "20250105_120000")
	require.Error(t, err)
	buf := make([]byte, 4096)
	if n, _ := reader.Read(buf); n != 0 {
		t.Fatalf("backup metadata was written to a FIFO: %q", buf[:n])
	}
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

	_, err := c.backupFile(srcFile, backupDir)
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

func TestBackupFileRejectsFIFODestination(t *testing.T) {
	base := t.TempDir()
	srcPath := filepath.Join(base, "source.txt")
	backupDir := filepath.Join(base, "backup")
	require.NoError(t, os.WriteFile(srcPath, []byte("secret backup content"), 0600))
	require.NoError(t, os.Mkdir(backupDir, 0700))
	hash := sha256.Sum256([]byte(srcPath))
	dstPath := filepath.Join(backupDir, fmt.Sprintf("%x", hash)[:16])
	require.NoError(t, syscall.Mkfifo(dstPath, 0600))
	reader, err := os.OpenFile(dstPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	require.NoError(t, err)
	defer reader.Close()

	c := &Cleaner{}
	_, err = c.backupFile(srcPath, backupDir)
	require.Error(t, err)
	buf := make([]byte, 4096)
	if n, _ := reader.Read(buf); n != 0 {
		t.Fatalf("backup data was written to a FIFO: %q", buf[:n])
	}
}

func TestListBackups(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempDir)

	backupDir := filepath.Join(tempDir, "moonbit", "backups")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	for _, name := range []string{"test1_20250105.backup", "test2_20250105.backup"} {
		require.NoError(t, os.Mkdir(filepath.Join(backupDir, name+".files"), 0700))
		require.NoError(t, os.WriteFile(filepath.Join(backupDir, name+".json"), []byte("{}"), 0600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "ghost.backup.json"), []byte("{}"), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(backupDir, "stray.backup.files"), 0700))

	backups, err := ListBackups()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"test1_20250105.backup", "test2_20250105.backup"}, backups)
}

func TestRestoreBackupDoesNotCreateDefaultConfig(t *testing.T) {
	f := newRestoreTestFixture(t)
	cfg := config.DefaultConfig()
	var userCache *config.Category
	for i := range cfg.Categories {
		if cfg.Categories[i].Name == "User Cache" {
			userCache = &cfg.Categories[i]
			break
		}
	}
	require.NotNil(t, userCache)
	target := filepath.Join(userCache.Paths[0], "restore-test.tmp")
	f.writeBackupForCategory(t, userCache.Name, target, []byte("restore"))

	configPath, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.Remove(configPath))
	require.NoError(t, RestoreBackup(f.backupPath))
	assert.NoFileExists(t, configPath)
}

func TestRestoreBackup(t *testing.T) {
	f := newRestoreTestFixture(t)
	testFile := filepath.Join(f.allowedDir, "original.txt")
	content := []byte("original content")
	f.writeBackup(t, testFile, content)

	// Restore backup
	err := RestoreBackup(f.backupPath)
	assert.NoError(t, err)

	// Verify file was restored
	restoredContent, err := os.ReadFile(testFile)
	assert.NoError(t, err)
	assert.Equal(t, content, restoredContent)
	info, err := os.Stat(testFile)
	assert.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "legacy backups restore with restrictive permissions")
}

func TestRestoreCreatedBackupRestoresOriginalModeAndOwnership(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.allowedDir, "private.log")
	content := []byte("private backup content")
	require.NoError(t, os.WriteFile(target, content, 0600))
	require.NoError(t, os.Chmod(target, 0640))
	originalInfo, err := os.Stat(target)
	require.NoError(t, err)

	category := &config.Category{
		Name:  "Test Cache",
		Paths: []string{f.allowedDir},
		Files: []config.FileInfo{{Path: target, Size: uint64(len(content)), CategoryName: "Test Cache"}},
		Size:  uint64(len(content)),
	}
	backupPath := (&Cleaner{}).createBackup(category)
	require.NotEmpty(t, backupPath)
	require.NoError(t, os.WriteFile(target, []byte("new content"), 0600))
	require.NoError(t, os.Chmod(target, 0644))
	require.NoError(t, RestoreBackup(backupPath))

	restoredContent, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, content, restoredContent)
	restoredInfo, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, originalInfo.Mode().Perm(), restoredInfo.Mode().Perm())
	if os.Geteuid() == 0 {
		originalOwner := originalInfo.Sys().(*syscall.Stat_t)
		restoredOwner := restoredInfo.Sys().(*syscall.Stat_t)
		assert.Equal(t, originalOwner.Uid, restoredOwner.Uid)
		assert.Equal(t, originalOwner.Gid, restoredOwner.Gid)
	}
}

func TestRestoreBackupRejectsCorruptedBlob(t *testing.T) {
	for _, mutation := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{name: "truncated", edit: func(data []byte) []byte { return data[:len(data)-1] }},
		{name: "same size", edit: func(data []byte) []byte {
			copy := append([]byte(nil), data...)
			copy[0] ^= 0xff
			return copy
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			f := newRestoreTestFixture(t)
			target := filepath.Join(f.allowedDir, "original.txt")
			content := []byte("backup content with enough bytes")
			require.NoError(t, os.WriteFile(target, content, 0600))
			category := &config.Category{
				Name:  "Test Cache",
				Paths: []string{f.allowedDir},
				Files: []config.FileInfo{{Path: target, Size: uint64(len(content)), CategoryName: "Test Cache"}},
				Size:  uint64(len(content)),
			}
			backupPath := (&Cleaner{}).createBackup(category)
			require.NotEmpty(t, backupPath)
			require.NoError(t, os.Remove(target))

			hash := sha256.Sum256([]byte(target))
			blobPath := filepath.Join(backupPath+".files", fmt.Sprintf("%x", hash)[:16])
			blob, err := os.ReadFile(blobPath)
			require.NoError(t, err)
			require.NotEmpty(t, blob)
			require.NoError(t, os.WriteFile(blobPath, mutation.edit(blob), 0600))

			err = RestoreBackup(backupPath)
			require.Error(t, err)
			assert.NoFileExists(t, target, "damaged backup data must not be installed")
		})
	}
}

func TestRestoreFilePreservesExistingContentOnCopyError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restore-target.txt")
	original := []byte("keep this file")
	require.NoError(t, os.WriteFile(path, original, 0600))
	source := io.MultiReader(strings.NewReader("partial restore"), restoreReadError{})

	err := restoreFile(path, source)
	require.Error(t, err)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestRestoreFileReplacesDestinationAndPreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restore-target.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0600))
	require.NoError(t, os.Chmod(path, 0640))

	require.NoError(t, restoreFile(path, strings.NewReader("restored")))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "restored", string(got))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0640), info.Mode().Perm())
}

func TestRestoreBackupReturnsErrorForMissingBackupFile(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.allowedDir, "restore-target.txt")
	f.writeBackup(t, target, []byte("backup data"))
	hash := sha256.Sum256([]byte(target))
	blobPath := filepath.Join(f.backupPath+".files", fmt.Sprintf("%x", hash)[:16])
	require.NoError(t, os.Remove(blobPath))

	err := RestoreBackup(f.backupPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore incomplete")
}

type restoreTestFixture struct {
	backupPath string
	allowedDir string
	outsideDir string
}

func newRestoreTestFixture(t *testing.T) restoreTestFixture {
	t.Helper()
	base := t.TempDir()
	dataHome := filepath.Join(base, "xdg-data")
	configHome := filepath.Join(base, "xdg-config")
	homeDir := filepath.Join(base, "home")
	allowedDir := filepath.Join(homeDir, "allowed-cache")
	outsideDir := filepath.Join(base, "outside")
	t.Setenv("MOONBIT_HOME", homeDir)
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "xdg-cache"))
	require.NoError(t, os.MkdirAll(allowedDir, 0700))
	require.NoError(t, os.MkdirAll(outsideDir, 0700))

	cfg := config.DefaultConfig()
	cfg.Categories = append(cfg.Categories, config.Category{
		Name:  "Test Cache",
		Paths: []string{allowedDir},
	})
	configPath, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, config.Save(cfg, configPath))

	backupDir, err := paths.DataDir("backups")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(backupDir, 0700))
	backupPath := filepath.Join(backupDir, "test.backup")
	return restoreTestFixture{backupPath: backupPath, allowedDir: allowedDir, outsideDir: outsideDir}
}

func (f restoreTestFixture) writeBackup(t *testing.T, target string, content []byte) {
	f.writeBackupForCategory(t, "Test Cache", target, content)
}

func (f restoreTestFixture) writeBackupForCategory(t *testing.T, category string, target string, content []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(f.backupPath+".files", 0700))
	hash := sha256.Sum256([]byte(target))
	blobPath := filepath.Join(f.backupPath+".files", fmt.Sprintf("%x", hash)[:16])
	require.NoError(t, os.WriteFile(blobPath, content, 0600))
	metadata := struct {
		Category string            `json:"category"`
		Files    []config.FileInfo `json:"files"`
	}{
		Category: category,
		Files: []config.FileInfo{{
			Path:         target,
			Size:         uint64(len(content)),
			CategoryName: category,
		}},
	}
	data, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.backupPath+".json", data, 0600))
}

func TestRestoreBackupRejectsTraversalBackupPath(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.allowedDir, "restored.txt")
	f.writeBackup(t, target, []byte("payload"))
	// The CLI joins user input to the backup directory before calling RestoreBackup.
	traversalPath := filepath.Join(filepath.Dir(f.backupPath), "..", "evil.backup")
	require.NoError(t, os.Rename(f.backupPath+".json", traversalPath+".json"))
	require.NoError(t, os.Rename(f.backupPath+".files", traversalPath+".files"))

	err := RestoreBackup(traversalPath)
	require.Error(t, err)
	assert.NoFileExists(t, target)
}

func TestRestoreBackupRejectsManifestPathOutsideCategory(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.outsideDir, "victim.txt")
	f.writeBackup(t, target, []byte("attacker data"))
	require.NoError(t, os.WriteFile(target, []byte("precious"), 0600))

	err := RestoreBackup(f.backupPath)
	require.Error(t, err)
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "precious", string(data))
}

func TestRestoreBackupRejectsUserConfiguredSystemRoot(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.outsideDir, "service.conf")
	f.writeBackupForCategory(t, "Custom System", target, []byte("attacker data"))
	require.NoError(t, os.WriteFile(target, []byte("precious"), 0600))

	configPath, err := paths.ConfigFile()
	require.NoError(t, err)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	cfg.Categories = append(cfg.Categories, config.Category{
		Name:  "Custom System",
		Paths: []string{f.outsideDir},
	})
	require.NoError(t, config.Save(cfg, configPath))

	err = RestoreBackup(f.backupPath)
	require.Error(t, err)
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "precious", string(data))
}

func TestRestoreCategoriesIgnoreCustomSystemPathsAndBuiltinOverrides(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	t.Setenv("PKEXEC_UID", "")
	home := "/home/moonbit-restore-test"
	t.Setenv("MOONBIT_HOME", home)
	cfg := config.DefaultConfig()
	cfg.Categories = append(cfg.Categories,
		config.Category{Name: "Local Custom", Paths: []string{filepath.Join(home, ".cache", "custom")}},
		config.Category{Name: "Remote Custom", Paths: []string{"/var/lib/private"}},
		config.Category{Name: "System Logs", Paths: []string{filepath.Join(home, "logs")}},
	)

	categories, gotHome, err := safeRestoreCategories(cfg)
	require.NoError(t, err)
	assert.Equal(t, home, gotHome)
	var localCustom, remoteCustom, systemLogs *config.Category
	for i := range categories {
		switch categories[i].Name {
		case "Local Custom":
			localCustom = &categories[i]
		case "Remote Custom":
			remoteCustom = &categories[i]
		case "System Logs":
			systemLogs = &categories[i]
		}
	}
	assert.NotNil(t, localCustom, "custom categories under the invoking user's home remain restorable")
	assert.Nil(t, remoteCustom, "user config must not authorize new system restore roots")
	require.NotNil(t, systemLogs)
	assert.Equal(t, []string{"/var/log"}, systemLogs.Paths, "user config must not replace built-in system roots")
	assert.NotEmpty(t, systemLogs.Filters, "user config must not weaken built-in system filters")
}

func TestCleanerProtectsOtherUsersHomeDirectories(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	t.Setenv("PKEXEC_UID", "")
	t.Setenv("MOONBIT_HOME", "/home/moonbit-clean-test")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	c := NewCleaner(&config.Config{})
	defer c.Close()

	assert.False(t, c.isProtectedPath("/home/moonbit-clean-test/.cache/file"))
	assert.True(t, c.isProtectedPath("/home/another-user/.ssh/authorized_keys"))
	assert.True(t, c.isProtectedPath("/root/.bashrc"))
}

func TestValidateBackupOwnerRequiresExpectedOwnerAndPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, os.WriteFile(path, []byte("backup"), 0600))
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat := info.Sys().(*syscall.Stat_t)
	require.NoError(t, validateBackupOwner(info, stat.Uid))
	require.Error(t, validateBackupOwner(info, stat.Uid+1))

	require.NoError(t, os.Chmod(path, 0644))
	info, err = os.Stat(path)
	require.NoError(t, err)
	require.Error(t, validateBackupOwner(info, stat.Uid))
}

func TestRestoreBackupRejectsInvalidCategoryRegex(t *testing.T) {
	for _, rule := range []struct {
		name  string
		apply func(*config.Category)
	}{
		{name: "filter", apply: func(c *config.Category) { c.Filters = []string{"["} }},
		{name: "exclude", apply: func(c *config.Category) { c.ExcludePatterns = []string{"["} }},
	} {
		t.Run(rule.name, func(t *testing.T) {
			f := newRestoreTestFixture(t)
			target := filepath.Join(f.allowedDir, "restored.txt")
			f.writeBackup(t, target, []byte("payload"))

			configPath, err := paths.ConfigFile()
			require.NoError(t, err)
			cfg, err := config.Load(configPath)
			require.NoError(t, err)
			var category *config.Category
			for i := range cfg.Categories {
				if cfg.Categories[i].Name == "Test Cache" {
					category = &cfg.Categories[i]
					break
				}
			}
			require.NotNil(t, category)
			rule.apply(category)
			require.NoError(t, config.Save(cfg, configPath))

			err = RestoreBackup(f.backupPath)
			require.Error(t, err)
			assert.NoFileExists(t, target)
		})
	}
}

func TestRestoreBackupRejectsProtectedPathEvenWhenCategoryAllowsIt(t *testing.T) {
	f := newRestoreTestFixture(t)
	configPath, err := paths.ConfigFile()
	require.NoError(t, err)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	cfg.Categories = append(cfg.Categories, config.Category{Name: "System Test", Paths: []string{"/etc"}})
	require.NoError(t, config.Save(cfg, configPath))
	f.writeBackupForCategory(t, "System Test", "/etc", []byte("payload"))

	err = RestoreBackup(f.backupPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
}

func TestRestoreBackupRejectsSymlinkedBlob(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.allowedDir, "restored.txt")
	f.writeBackup(t, target, []byte("payload"))
	hash := sha256.Sum256([]byte(target))
	blobPath := filepath.Join(f.backupPath+".files", fmt.Sprintf("%x", hash)[:16])
	source := filepath.Join(f.outsideDir, "blob")
	require.NoError(t, os.WriteFile(source, []byte("payload"), 0600))
	require.NoError(t, os.Remove(blobPath))
	require.NoError(t, os.Symlink(source, blobPath))

	err := RestoreBackup(f.backupPath)
	require.Error(t, err)
	assert.NoFileExists(t, target)
}

func TestRestoreBackupRejectsSymlinkedMetadata(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.allowedDir, "restored.txt")
	f.writeBackup(t, target, []byte("payload"))
	metadataPath := f.backupPath + ".json"
	externalMetadata := filepath.Join(f.outsideDir, "manifest.json")
	data, err := os.ReadFile(metadataPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(externalMetadata, data, 0600))
	require.NoError(t, os.Remove(metadataPath))
	require.NoError(t, os.Symlink(externalMetadata, metadataPath))

	err = RestoreBackup(f.backupPath)
	require.Error(t, err)
	assert.NoFileExists(t, target)
}

func TestRestoreBackupRejectsSymlinkDestination(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.allowedDir, "restored.txt")
	outside := filepath.Join(f.outsideDir, "victim.txt")
	f.writeBackup(t, target, []byte("payload"))
	require.NoError(t, os.WriteFile(outside, []byte("precious"), 0600))
	require.NoError(t, os.Symlink(outside, target))

	err := RestoreBackup(f.backupPath)
	require.Error(t, err)
	data, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "precious", string(data))
}

func TestRestoreBackupRejectsSymlinkedDestinationParent(t *testing.T) {
	f := newRestoreTestFixture(t)
	target := filepath.Join(f.allowedDir, "nested", "restored.txt")
	f.writeBackup(t, target, []byte("payload"))
	require.NoError(t, os.Symlink(f.outsideDir, filepath.Join(f.allowedDir, "nested")))

	err := RestoreBackup(f.backupPath)
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(f.outsideDir, "restored.txt"))
}

func TestCreateBackupRejectsSymlinkedDataDirectory(t *testing.T) {
	base := t.TempDir()
	dataHome := filepath.Join(base, "xdg-data")
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(dataHome, 0700))
	require.NoError(t, os.Mkdir(outside, 0700))
	require.NoError(t, os.Symlink(outside, filepath.Join(dataHome, "moonbit")))
	t.Setenv("XDG_DATA_HOME", dataHome)
	source := filepath.Join(base, "source.txt")
	require.NoError(t, os.WriteFile(source, []byte("secret"), 0600))

	c := &Cleaner{}
	backupPath := c.createBackup(&config.Category{
		Name:  "Test",
		Files: []config.FileInfo{{Path: source}},
	})
	assert.Empty(t, backupPath)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestBackupFileRejectsSymlinkedSource(t *testing.T) {
	base := t.TempDir()
	backupDir := filepath.Join(base, "backup")
	outside := filepath.Join(base, "outside.txt")
	source := filepath.Join(base, "cache-link")
	require.NoError(t, os.Mkdir(backupDir, 0700))
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0600))
	require.NoError(t, os.Symlink(outside, source))

	c := &Cleaner{}
	_, err := c.backupFile(source, backupDir)
	require.Error(t, err)
	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
