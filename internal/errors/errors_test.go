package errors

import (
	"errors"
	"strings"
	"testing"
)

func TestPermissionDeniedError(t *testing.T) {
	err := NewPermissionDeniedError("/test/path", errors.New("access denied"))

	if err.Code != ErrCodePermissionDenied {
		t.Errorf("Expected code %s, got %s", ErrCodePermissionDenied, err.Code)
	}

	if !strings.Contains(err.Error(), "/test/path") {
		t.Errorf("Error message should contain path")
	}

	msg := err.UserMessage()
	if !strings.Contains(msg, "sudo") {
		t.Errorf("User message should suggest sudo")
	}
}

func TestPathProtectedError(t *testing.T) {
	protectedPaths := []string{"/bin", "/usr/bin", "/etc"}
	err := NewPathProtectedError("/bin/ls", protectedPaths)

	if err.Code != ErrCodePathProtected {
		t.Errorf("Expected code %s, got %s", ErrCodePathProtected, err.Code)
	}

	if !strings.Contains(err.Error(), "/bin/ls") {
		t.Errorf("Error message should contain the path")
	}

	if !strings.Contains(err.UserMessage(), "Protected paths") {
		t.Errorf("User message should explain protected paths")
	}
}

func TestBackupFailedError(t *testing.T) {
	err := NewBackupFailedError("Test Category", errors.New("disk full"))

	if err.Code != ErrCodeBackupFailed {
		t.Errorf("Expected code %s, got %s", ErrCodeBackupFailed, err.Code)
	}

	if !strings.Contains(err.Error(), "Test Category") {
		t.Errorf("Error message should contain category name")
	}

	msg := err.UserMessage()
	if !strings.Contains(msg, "disk space") {
		t.Errorf("User message should suggest checking disk space")
	}
}

func TestRestoreFailedError(t *testing.T) {
	err := NewRestoreFailedError("/backup/path", 5, 3, errors.New("copy failed"))

	if err.Code != ErrCodeRestoreFailed {
		t.Errorf("Expected code %s, got %s", ErrCodeRestoreFailed, err.Code)
	}

	if err.Context["files_restored"] != 5 {
		t.Errorf("Expected 5 files restored")
	}

	if err.Context["files_failed"] != 3 {
		t.Errorf("Expected 3 files failed")
	}

	msg := err.UserMessage()
	if !strings.Contains(msg, "5 files") {
		t.Errorf("User message should mention files restored count")
	}
}

func TestSafetyCheckFailedError(t *testing.T) {
	err := NewSafetyCheckFailedError("size exceeded", "Test", 1000000, 500000)

	if err.Code != ErrCodeSafetyCheckFailed {
		t.Errorf("Expected code %s, got %s", ErrCodeSafetyCheckFailed, err.Code)
	}

	if !strings.Contains(err.Error(), "size exceeded") {
		t.Errorf("Error message should contain reason")
	}

	msg := err.UserMessage()
	if !strings.Contains(msg, "dry-run") {
		t.Errorf("User message should suggest dry-run")
	}
}

func TestFileNotFoundError(t *testing.T) {
	err := NewFileNotFoundError("/missing/file", errors.New("no such file"))

	if err.Code != ErrCodeFileNotFound {
		t.Errorf("Expected code %s, got %s", ErrCodeFileNotFound, err.Code)
	}

	if !strings.Contains(err.Error(), "/missing/file") {
		t.Errorf("Error message should contain file path")
	}
}

func TestInvalidPathError(t *testing.T) {
	err := NewInvalidPathError("../../etc/passwd", "path traversal")

	if err.Code != ErrCodeInvalidPath {
		t.Errorf("Expected code %s, got %s", ErrCodeInvalidPath, err.Code)
	}

	if !strings.Contains(err.Error(), "path traversal") {
		t.Errorf("Error message should contain reason")
	}
}

func TestScanCancelledError(t *testing.T) {
	err := NewScanCancelledError(100, 50000)

	if err.Code != ErrCodeScanCancelled {
		t.Errorf("Expected code %s, got %s", ErrCodeScanCancelled, err.Code)
	}

	if err.Context["files_scanned"] != 100 {
		t.Errorf("Expected 100 files scanned")
	}
}

func TestCleanFailedError(t *testing.T) {
	errors := []string{"error1", "error2", "error3"}
	err := NewCleanFailedError("Test", 10, 7, 3, errors)

	if err.Code != ErrCodeCleanFailed {
		t.Errorf("Expected code %s, got %s", ErrCodeCleanFailed, err.Code)
	}

	if err.Context["total_files"] != 10 {
		t.Errorf("Expected 10 total files")
	}

	if err.Context["files_deleted"] != 7 {
		t.Errorf("Expected 7 files deleted")
	}

	if err.Context["files_failed"] != 3 {
		t.Errorf("Expected 3 files failed")
	}

	msg := err.UserMessage()
	if !strings.Contains(msg, "3 out of 10") {
		t.Errorf("User message should mention failure count")
	}
}

func TestWrap(t *testing.T) {
	t.Run("Wrap standard error", func(t *testing.T) {
		stdErr := errors.New("standard error")
		wrapped := Wrap(stdErr, ErrCodeCleanFailed, "clean operation failed")

		if wrapped.Code != ErrCodeCleanFailed {
			t.Errorf("Expected code %s", ErrCodeCleanFailed)
		}

		if wrapped.Cause != stdErr {
			t.Errorf("Cause should be original error")
		}
	})

	t.Run("Wrap nil error", func(t *testing.T) {
		wrapped := Wrap(nil, ErrCodeCleanFailed, "test")

		if wrapped != nil {
			t.Errorf("Wrapping nil should return nil")
		}
	})

	t.Run("Wrap MoonBitError returns as-is", func(t *testing.T) {
		original := NewFileNotFoundError("/test", nil)
		wrapped := Wrap(original, ErrCodeCleanFailed, "different message")

		if wrapped != original {
			t.Errorf("Wrapping MoonBitError should return original")
		}
	})
}

func TestUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := NewPermissionDeniedError("/test", cause)

	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Errorf("Unwrap should return the cause")
	}
}

func TestErrorWithContext(t *testing.T) {
	err := &MoonBitError{
		Code:    ErrCodeCleanFailed,
		Message: "test error",
		Context: map[string]interface{}{
			"file":  "/test/file",
			"count": 5,
		},
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "file: /test/file") {
		t.Errorf("Error should contain context: %s", errMsg)
	}
	if !strings.Contains(errMsg, "count: 5") {
		t.Errorf("Error should contain count: %s", errMsg)
	}
}
