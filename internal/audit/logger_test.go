package audit

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger(t *testing.T) {
	// Use a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	assert.NotNil(t, logger)
	assert.NotEmpty(t, logger.filePath)
	assert.Contains(t, logger.filePath, ".local")
	assert.Contains(t, logger.filePath, "share")
	assert.Contains(t, logger.filePath, "moonbit")
	assert.Contains(t, logger.filePath, "logs")
	assert.Contains(t, logger.filePath, "audit.log")
	assert.NotNil(t, logger.file)

	// Cleanup
	logger.Close()
}

func TestLogger_Log(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	entry := LogEntry{
		Operation: "test_operation",
		Args:      []string{"arg1", "arg2"},
		Result:    "success",
	}

	err = logger.Log(entry)
	assert.NoError(t, err)

	// Verify log file was written
	logPath := logger.filePath
	info, err := os.Stat(logPath)
	assert.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestLogger_Log_AutoTimestamp(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	entry := LogEntry{
		Timestamp: time.Time{}, // Zero time
		Operation: "test",
		Result:    "success",
	}

	err = logger.Log(entry)
	assert.NoError(t, err)
	// Timestamp should be set automatically
}

func TestLogger_Log_AutoUser(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	entry := LogEntry{
		User:      "", // Empty user
		Operation: "test",
		Result:    "success",
	}

	err = logger.Log(entry)
	assert.NoError(t, err)
	// User should be set automatically from environment
}

func TestLogger_Log_WithError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	entry := LogEntry{
		Operation: "test",
		Result:    "failed",
		Error:     assert.AnError,
	}

	err = logger.Log(entry)
	assert.NoError(t, err)
}

func TestLogger_LogPackageOperation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	err = logger.LogPackageOperation("install", []string{"package1", "package2"}, "success", nil)
	assert.NoError(t, err)
}

func TestLogger_LogSystemdOperation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	err = logger.LogSystemdOperation("enable", "moonbit-scan.timer", "success", nil)
	assert.NoError(t, err)
}

func TestLogger_LogDockerOperation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	err = logger.LogDockerOperation("prune_images", []string{"-a", "-f"}, "success", nil)
	assert.NoError(t, err)
}

func TestLogger_LogCleanOperation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	err = logger.LogCleanOperation(10, 1024000, nil)
	assert.NoError(t, err)
}

func TestLogger_Close(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)

	err = logger.Close()
	assert.NoError(t, err)

	// Closing again should not error
	err = logger.Close()
	assert.NoError(t, err)
}

func TestLogger_ConcurrentLogging(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "moonbit-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	os.Setenv("HOME", tmpDir)

	logger, err := NewLogger()
	require.NoError(t, err)
	defer logger.Close()

	// Test concurrent logging (thread safety)
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			entry := LogEntry{
				Operation: "concurrent_test",
				Result:    "success",
			}
			err := logger.Log(entry)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
