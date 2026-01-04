package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	pathTraversalPattern = regexp.MustCompile(`\.\.`)
	packageNamePattern   = regexp.MustCompile(`^[a-zA-Z0-9._+-]+$`)
)

// ValidateFilePath checks if a file path is safe to use
func ValidateFilePath(path string) error {
	if path == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	cleanPath := filepath.Clean(path)

	if pathTraversalPattern.MatchString(cleanPath) {
		return fmt.Errorf("path contains unsafe traversal: %s", path)
	}

	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	protectedPaths := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/boot", "/sys", "/proc", "/dev"}
	for _, protected := range protectedPaths {
		if strings.HasPrefix(absPath, protected) {
			return fmt.Errorf("cannot operate on protected system path: %s", absPath)
		}
	}

	return nil
}

// ValidatePackage checks if a package name is valid
func ValidatePackage(pkg string) error {
	if pkg == "" {
		return fmt.Errorf("package name cannot be empty")
	}

	if !packageNamePattern.MatchString(pkg) {
		return fmt.Errorf("invalid package name: %s (must contain only letters, numbers, dots, hyphens, underscores)", pkg)
	}

	if len(pkg) > 255 {
		return fmt.Errorf("package name too long: %d characters (max 255)", len(pkg))
	}

	return nil
}

// ValidateSize checks if a size value is within acceptable bounds
func ValidateSize(size uint64, maxSize uint64) error {
	if size > maxSize {
		return fmt.Errorf("size %d exceeds maximum allowed: %d", size, maxSize)
	}
	return nil
}

// ValidateMode checks if a scan/clean mode is valid
func ValidateMode(mode string) error {
	if mode == "" {
		return nil
	}

	validModes := map[string]bool{
		"quick": true,
		"deep":  true,
	}

	if !validModes[mode] {
		return fmt.Errorf("invalid mode: %s (must be 'quick' or 'deep')", mode)
	}

	return nil
}

// ValidateDirExists checks if a directory exists
func ValidateDirExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", path)
		}
		return fmt.Errorf("cannot access directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}

	return nil
}

// ValidateFileExists checks if a file exists and is accessible
func ValidateFileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", path)
		}
		return fmt.Errorf("cannot access file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", path)
	}

	return nil
}
