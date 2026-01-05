package validation

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateFilePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"Empty path", "", true},
		{"Valid relative path", "./test.txt", false},
		{"Valid absolute path", "/tmp/test.txt", false},
		{"Path with traversal", "../../etc/passwd", true},
		{"Path with traversal in clean", "/tmp/../bin/ls", true}, // Cleaned to /bin/ls which is protected
		{"Protected path /bin", "/bin/ls", true},
		{"Protected path /usr/bin", "/usr/bin/git", true},
		{"Protected path /sbin", "/sbin/init", true},
		{"Protected path /boot", "/boot/vmlinuz", true},
		{"Protected path /sys", "/sys/kernel", true},
		{"Protected path /proc", "/proc/1", true},
		{"Protected path /dev", "/dev/null", true},
		{"Safe path /tmp", "/tmp/test", false},
		{"Safe path /home", "/home/user/test", false},
		{"Safe path /var", "/var/log/test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilePath(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePackage(t *testing.T) {
	tests := []struct {
		name    string
		pkg     string
		wantErr bool
	}{
		{"Empty package", "", true},
		{"Valid package", "test-package", false},
		{"Valid with dots", "test.package", false},
		{"Valid with underscores", "test_package", false},
		{"Valid with plus", "test+package", false},
		{"Invalid with spaces", "test package", true},
		{"Invalid with special chars", "test@package", true},
		{"Invalid with slashes", "test/package", true},
		{"Too long", strings.Repeat("a", 256), true},
		{"Max length", strings.Repeat("a", 255), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePackage(tt.pkg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSize(t *testing.T) {
	tests := []struct {
		name    string
		size    uint64
		maxSize uint64
		wantErr bool
	}{
		{"Within limit", 100, 1000, false},
		{"At limit", 1000, 1000, false},
		{"Exceeds limit", 1001, 1000, true},
		{"Zero size", 0, 1000, false},
		{"Large size within limit", 1000000, 2000000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSize(tt.size, tt.maxSize)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateMode(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{"Empty mode", "", false},
		{"Quick mode", "quick", false},
		{"Deep mode", "deep", false},
		{"Invalid mode", "invalid", true},
		{"Uppercase quick", "QUICK", true},
		{"Mixed case", "Quick", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMode(tt.mode)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func createTempFile(t *testing.T) string {
	tmpFile, err := os.CreateTemp("", "moonbit-test-file-*")
	require.NoError(t, err)
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	return tmpFile.Name()
}

func TestValidateDirExists(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "moonbit-test-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tmpFile := createTempFile(t)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"Existing directory", tmpDir, false},
		{"Non-existent directory", "/nonexistent/dir/path", true},
		{"Path is a file", tmpFile, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDirExists(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateFileExists(t *testing.T) {
	// Create a temporary file for testing
	tmpFile, err := os.CreateTemp("", "moonbit-test-*")
	assert.NoError(t, err)
	tmpFilePath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpFilePath)

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "moonbit-test-dir-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"Existing file", tmpFilePath, false},
		{"Non-existent file", "/nonexistent/file/path", true},
		{"Path is a directory", tmpDir, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFileExists(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
