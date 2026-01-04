package errors

import (
	"fmt"
	"strings"
)

// ErrorCode represents a specific error type for programmatic handling
type ErrorCode string

const (
	// File operation errors
	ErrCodePermissionDenied ErrorCode = "PERMISSION_DENIED"
	ErrCodeFileNotFound     ErrorCode = "FILE_NOT_FOUND"
	ErrCodePathProtected    ErrorCode = "PATH_PROTECTED"
	ErrCodeDiskFull         ErrorCode = "DISK_FULL"
	ErrCodeInvalidPath      ErrorCode = "INVALID_PATH"

	// Backup operation errors
	ErrCodeBackupFailed    ErrorCode = "BACKUP_FAILED"
	ErrCodeBackupCorrupted ErrorCode = "BACKUP_CORRUPTED"
	ErrCodeRestoreFailed   ErrorCode = "RESTORE_FAILED"

	// Scan operation errors
	ErrCodeScanCancelled  ErrorCode = "SCAN_CANCELLED"
	ErrCodeScanTimeout    ErrorCode = "SCAN_TIMEOUT"
	ErrCodeInvalidPattern ErrorCode = "INVALID_PATTERN"

	// Clean operation errors
	ErrCodeCleanFailed       ErrorCode = "CLEAN_FAILED"
	ErrCodeSafetyCheckFailed ErrorCode = "SAFETY_CHECK_FAILED"
	ErrCodeSizeLimitExceeded ErrorCode = "SIZE_LIMIT_EXCEEDED"

	// Configuration errors
	ErrCodeConfigInvalid    ErrorCode = "CONFIG_INVALID"
	ErrCodeCategoryNotFound ErrorCode = "CATEGORY_NOT_FOUND"
)

// MoonBitError represents a user-friendly error with context and suggestions
type MoonBitError struct {
	Code        ErrorCode
	Message     string
	Cause       error
	Context     map[string]interface{}
	Suggestions []string
}

// Error implements the error interface
func (e *MoonBitError) Error() string {
	var sb strings.Builder

	sb.WriteString(e.Message)

	if e.Cause != nil {
		sb.WriteString(fmt.Sprintf(": %v", e.Cause))
	}

	if len(e.Context) > 0 {
		sb.WriteString(" (")
		first := true
		for k, v := range e.Context {
			if !first {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s: %v", k, v))
			first = false
		}
		sb.WriteString(")")
	}

	return sb.String()
}

// Unwrap returns the underlying cause
func (e *MoonBitError) Unwrap() error {
	return e.Cause
}

// UserMessage returns a formatted message for end users with suggestions
func (e *MoonBitError) UserMessage() string {
	var sb strings.Builder

	sb.WriteString(e.Message)

	if len(e.Suggestions) > 0 {
		sb.WriteString("\n\nSuggestions:")
		for _, suggestion := range e.Suggestions {
			sb.WriteString(fmt.Sprintf("\n  • %s", suggestion))
		}
	}

	return sb.String()
}

// NewPermissionDeniedError creates an error for permission issues
func NewPermissionDeniedError(path string, cause error) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodePermissionDenied,
		Message: fmt.Sprintf("Permission denied accessing %s", path),
		Cause:   cause,
		Context: map[string]interface{}{
			"path": path,
		},
		Suggestions: []string{
			"Run with sudo if you have administrative privileges",
			"Check file/directory permissions with 'ls -l'",
			"Ensure your user has read/write access to this location",
		},
	}
}

// NewPathProtectedError creates an error for protected path access attempts
func NewPathProtectedError(path string, protectedPaths []string) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodePathProtected,
		Message: fmt.Sprintf("Cannot delete protected system path: %s", path),
		Cause:   nil,
		Context: map[string]interface{}{
			"path":            path,
			"protected_paths": strings.Join(protectedPaths, ", "),
		},
		Suggestions: []string{
			"Protected paths prevent accidental deletion of critical system files",
			"Do not attempt to clean system directories like /bin, /usr/bin, /etc",
			"If you need to clean system paths, do so manually with extreme caution",
		},
	}
}

// NewBackupFailedError creates an error for backup operation failures
func NewBackupFailedError(category string, cause error) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodeBackupFailed,
		Message: fmt.Sprintf("Failed to create backup for category '%s'", category),
		Cause:   cause,
		Context: map[string]interface{}{
			"category": category,
		},
		Suggestions: []string{
			"Ensure you have sufficient disk space in ~/.local/share/moonbit/backups",
			"Check write permissions for the backup directory",
			"Try running with --no-backup if backups are not critical",
		},
	}
}

// NewRestoreFailedError creates an error for restore operation failures
func NewRestoreFailedError(backupPath string, filesRestored int, filesFailed int, cause error) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodeRestoreFailed,
		Message: fmt.Sprintf("Failed to fully restore backup from %s", backupPath),
		Cause:   cause,
		Context: map[string]interface{}{
			"backup_path":    backupPath,
			"files_restored": filesRestored,
			"files_failed":   filesFailed,
		},
		Suggestions: []string{
			fmt.Sprintf("Partially restored %d files, but %d files failed", filesRestored, filesFailed),
			"Check that destination paths are writable",
			"Verify backup integrity with 'moonbit backup list'",
			"Some files may need manual restoration",
		},
	}
}

// NewSafetyCheckFailedError creates an error for safety check failures
func NewSafetyCheckFailedError(reason string, category string, size uint64, maxSize uint64) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodeSafetyCheckFailed,
		Message: fmt.Sprintf("Safety check failed: %s", reason),
		Cause:   nil,
		Context: map[string]interface{}{
			"category":       category,
			"size_bytes":     size,
			"max_size_bytes": maxSize,
		},
		Suggestions: []string{
			"Review what would be deleted with 'moonbit clean' (dry-run mode)",
			"Increase max deletion size in config if this is expected",
			"Clean categories individually for better control",
			"Use --mode quick for safer, smaller cleanups",
		},
	}
}

// NewFileNotFoundError creates an error for missing files
func NewFileNotFoundError(path string, cause error) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodeFileNotFound,
		Message: fmt.Sprintf("File or directory not found: %s", path),
		Cause:   cause,
		Context: map[string]interface{}{
			"path": path,
		},
		Suggestions: []string{
			"Verify the path exists and is spelled correctly",
			"File may have already been deleted",
			"Check if the directory still exists",
		},
	}
}

// NewInvalidPathError creates an error for invalid or dangerous paths
func NewInvalidPathError(path string, reason string) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodeInvalidPath,
		Message: fmt.Sprintf("Invalid path: %s (%s)", path, reason),
		Cause:   nil,
		Context: map[string]interface{}{
			"path":   path,
			"reason": reason,
		},
		Suggestions: []string{
			"Ensure the path does not contain path traversal attempts (..)",
			"Use absolute paths when possible",
			"Avoid special characters in paths",
		},
	}
}

// NewScanCancelledError creates an error for cancelled scans
func NewScanCancelledError(filesScanned int, bytesScanned uint64) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodeScanCancelled,
		Message: "Scan operation was cancelled by user",
		Cause:   nil,
		Context: map[string]interface{}{
			"files_scanned": filesScanned,
			"bytes_scanned": bytesScanned,
		},
		Suggestions: []string{
			"Partial results are not saved when scan is cancelled",
			"Use 'moonbit scan --mode quick' for faster scans",
			"Press Ctrl+C again to force quit",
		},
	}
}

// NewCleanFailedError creates an error with aggregated clean failure details
func NewCleanFailedError(category string, totalFiles int, filesDeleted int, filesFailed int, errors []string) *MoonBitError {
	return &MoonBitError{
		Code:    ErrCodeCleanFailed,
		Message: fmt.Sprintf("Failed to clean %d out of %d files in category '%s'", filesFailed, totalFiles, category),
		Cause:   nil,
		Context: map[string]interface{}{
			"category":      category,
			"total_files":   totalFiles,
			"files_deleted": filesDeleted,
			"files_failed":  filesFailed,
		},
		Suggestions: []string{
			"Check error details below for specific failure reasons",
			"Some files may be in use or locked by other processes",
			"Try closing applications that might be using these files",
			"Review logs at ~/.local/share/moonbit/logs/audit.log",
		},
	}
}

// Wrap wraps a standard error into a MoonBitError if it isn't already one
func Wrap(err error, code ErrorCode, message string) *MoonBitError {
	if err == nil {
		return nil
	}

	// If already a MoonBitError, return as-is
	if mbErr, ok := err.(*MoonBitError); ok {
		return mbErr
	}

	return &MoonBitError{
		Code:    code,
		Message: message,
		Cause:   err,
		Context: make(map[string]interface{}),
	}
}
