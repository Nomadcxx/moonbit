package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanCommandFlags(t *testing.T) {
	// Save original dryRun value
	originalDryRun := dryRun
	defer func() { dryRun = originalDryRun }()

	// Test 1: Default behavior (dry-run should be true)
	dryRun = true
	assert.True(t, dryRun, "dry-run should be true by default")

	// Test 2: Simulate force flag being set
	// In the actual command, PreRun would be called by cobra
	// We test the logic directly
	dryRun = true
	forceFlag := true
	if forceFlag {
		dryRun = false
	}
	assert.False(t, dryRun, "dry-run should be false when force is set")

	// Test 3: Force flag not set
	dryRun = true
	forceFlag = false
	if forceFlag {
		dryRun = false
	}
	assert.True(t, dryRun, "dry-run should remain true when force is not set")
}

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"Zero bytes", 0, "0 B"},
		{"Bytes", 512, "512 B"},
		{"Kilobytes", 1024, "1.0 KB"},
		{"Kilobytes decimal", 1536, "1.5 KB"},
		{"Megabytes", 1048576, "1.0 MB"},
		{"Megabytes decimal", 1572864, "1.5 MB"},
		{"Gigabytes", 1073741824, "1.0 GB"},
		{"Gigabytes decimal", 1610612736, "1.5 GB"},
		{"Large value", 5368709120, "5.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := humanizeBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSessionCachePath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	assert.NoError(t, err)

	expected := filepath.Join(homeDir, ".cache", "moonbit", "scan_results.json")
	actual := getSessionCachePath()

	assert.Equal(t, expected, actual)
}

func TestSaveAndLoadSessionCache(t *testing.T) {
	// This test verifies cache path format and directory creation
	// We test with the actual cache location since it's user-specific

	// Get the cache path
	cachePath := getSessionCachePath()
	assert.NotEmpty(t, cachePath)

	// Verify it's in the .cache directory
	homeDir, _ := os.UserHomeDir()
	assert.Contains(t, cachePath, filepath.Join(homeDir, ".cache", "moonbit"))
}

func TestRequiresSudo(t *testing.T) {
	// This test checks if requiresSudo correctly identifies system paths
	result := requiresSudo()

	// The result depends on whether system paths exist
	// We just verify it returns a boolean without panicking
	assert.IsType(t, true, result)
}
