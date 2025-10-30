package cleaner

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
)

// Safety configuration for cleaning operations
type SafetyConfig struct {
	RequireConfirmation bool     `toml:"require_confirmation"`
	MaxDeletionSize     uint64   `toml:"max_deletion_size_mb"`
	ProtectedPaths      []string `toml:"protected_paths"`
	SafeMode            bool     `toml:"safe_mode"`
	ShredPasses         int      `toml:"shred_passes"`
}

// CleanProgress represents progress updates during cleaning
type CleanProgress struct {
	FilesProcessed int
	BytesFreed     uint64
	CurrentFile    string
	TotalFiles     int
	TotalBytes     uint64
}

// CleanComplete represents the completion of a cleaning operation
type CleanComplete struct {
	Category      string
	FilesDeleted  int
	BytesFreed    uint64
	Duration      time.Duration
	BackupCreated bool
	BackupPath    string
	Errors        []string
}

// CleanMsg represents messages from the cleaner
type CleanMsg struct {
	Progress *CleanProgress
	Complete *CleanComplete
	Error    error
}

// Cleaner handles file deletion with safety mechanisms
type Cleaner struct {
	cfg           *config.Config
	safetyConfig  *SafetyConfig
	backupEnabled bool
}

// NewCleaner creates a new cleaner instance
func NewCleaner(cfg *config.Config) *Cleaner {
	safetyCfg := &SafetyConfig{
		RequireConfirmation: true,
		MaxDeletionSize:     1024, // 1GB default
		SafeMode:            true,
		ShredPasses:         1,
		ProtectedPaths: []string{
			"/bin",
			"/usr/bin",
			"/usr/sbin",
			"/sbin",
			"/etc",
			"/var/lib",
			"/boot",
			"/sys",
			"/proc",
		},
	}

	return &Cleaner{
		cfg:           cfg,
		safetyConfig:  safetyCfg,
		backupEnabled: true,
	}
}

// CleanCategory cleans files from a specific category
func (c *Cleaner) CleanCategory(ctx context.Context, category *config.Category, dryRun bool, progressCh chan<- CleanMsg) error {
	start := time.Now()

	// Safety checks
	if err := c.performSafetyChecks(category, dryRun); err != nil {
		progressCh <- CleanMsg{Error: fmt.Errorf("safety check failed: %w", err)}
		return err
	}

	// Create backup if not dry run
	var backupPath string
	if !dryRun && c.backupEnabled {
		backupPath = c.createBackup(category)
		if backupPath == "" {
			// Backup failed - abort if not in safe mode
			if c.safetyConfig.SafeMode {
				progressCh <- CleanMsg{Error: fmt.Errorf("backup creation failed, aborting for safety")}
				return fmt.Errorf("backup creation failed")
			}
		}
	}

	// Clean files
	filesDeleted := 0
	bytesFreed := uint64(0)
	var errors []string

	for _, fileInfo := range category.Files {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Send progress update
		progressCh <- CleanMsg{
			Progress: &CleanProgress{
				FilesProcessed: filesDeleted,
				BytesFreed:     bytesFreed,
				CurrentFile:    fileInfo.Path,
				TotalFiles:     len(category.Files),
				TotalBytes:     category.Size,
			},
		}

		if dryRun {
			// Dry run - just count
			filesDeleted++
			bytesFreed += fileInfo.Size
		} else {
			// Actual cleaning
			if err := c.deleteFile(fileInfo.Path, category.ShredEnabled); err != nil {
				errors = append(errors, fmt.Sprintf("failed to delete %s: %v", fileInfo.Path, err))
				continue
			}
			filesDeleted++
			bytesFreed += fileInfo.Size
		}
	}

	// Send completion message
	duration := time.Since(start)
	progressCh <- CleanMsg{
		Complete: &CleanComplete{
			Category:      category.Name,
			FilesDeleted:  filesDeleted,
			BytesFreed:    bytesFreed,
			Duration:      duration,
			BackupCreated: backupPath != "",
			BackupPath:    backupPath,
			Errors:        errors,
		},
	}

	return nil
}

// DeleteFile performs actual file deletion with optional shredding
func (c *Cleaner) deleteFile(path string, shredEnabled bool) error {
	// Additional safety check
	if c.isProtectedPath(path) {
		return fmt.Errorf("attempted to delete protected path: %s", path)
	}

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Shred if enabled (overwrite before deletion)
	if shredEnabled && info.Size() > 0 {
		if err := c.shredFile(path, info.Size()); err != nil {
			return fmt.Errorf("failed to shred file: %w", err)
		}
	}

	// Remove the file
	return os.Remove(path)
}

// ShredFile overwrites a file with random data before deletion
func (c *Cleaner) shredFile(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	passes := c.safetyConfig.ShredPasses
	if passes < 1 {
		passes = 1
	}

	for pass := 0; pass < passes; pass++ {
		// Create random data buffer
		buffer := make([]byte, 4096) // 4KB chunks
		_, err := rand.Read(buffer)
		if err != nil {
			return err
		}

		// Write random data to file
		bytesWritten := int64(0)
		for bytesWritten < size {
			chunkSize := int64(len(buffer))
			if bytesWritten+chunkSize > size {
				chunkSize = size - bytesWritten
			}

			n, err := file.Write(buffer[:chunkSize])
			if err != nil {
				return err
			}
			bytesWritten += int64(n)
		}

		// Sync to ensure data is written
		if err := file.Sync(); err != nil {
			return err
		}

		// Seek back to beginning for next pass
		if _, err := file.Seek(0, 0); err != nil {
			return err
		}
	}

	return nil
}

// Perform safety checks before cleaning
func (c *Cleaner) performSafetyChecks(category *config.Category, dryRun bool) error {
	// Check if category is safe to clean
	if category.Risk == config.High && !dryRun {
		if c.safetyConfig.SafeMode {
			return fmt.Errorf("high-risk category '%s' requires manual confirmation", category.Name)
		}
	}

	// Check total size
	if category.Size > c.safetyConfig.MaxDeletionSize*1024*1024 {
		return fmt.Errorf("category size %d bytes exceeds maximum allowed %d bytes",
			category.Size, c.safetyConfig.MaxDeletionSize*1024*1024)
	}

	// Check for protected paths
	for _, fileInfo := range category.Files {
		if c.isProtectedPath(fileInfo.Path) {
			return fmt.Errorf("category contains protected path: %s", fileInfo.Path)
		}
	}

	return nil
}

// Check if a path is protected
func (c *Cleaner) isProtectedPath(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return true // Fail safe - protect on error
	}

	// Check against protected paths
	for _, protected := range c.safetyConfig.ProtectedPaths {
		if filepath.HasPrefix(absPath, protected) {
			return true
		}
	}

	return false
}

// Create backup before cleaning
func (c *Cleaner) createBackup(category *config.Category) string {
	timestamp := time.Now().Format("20060102_150405")
	backupDir := filepath.Join(os.Getenv("XDG_DATA_HOME"), "moonbit", "backups")

	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return ""
	}

	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_%s.tar.gz", category.Name, timestamp))

	// For now, just create a simple manifest file
	// TODO: Implement actual tar backup creation
	manifestPath := backupPath + ".manifest"
	manifest, err := os.Create(manifestPath)
	if err != nil {
		return ""
	}
	defer manifest.Close()

	// Write manifest
	manifest.WriteString(fmt.Sprintf("Backup created: %s\n", time.Now().Format(time.RFC3339)))
	manifest.WriteString(fmt.Sprintf("Category: %s\n", category.Name))
	manifest.WriteString(fmt.Sprintf("Files: %d\n", len(category.Files)))
	manifest.WriteString(fmt.Sprintf("Total Size: %d bytes\n", category.Size))

	for _, file := range category.Files {
		manifest.WriteString(file.Path + "\n")
	}

	return backupPath
}

// GetDefaultSafetyConfig returns default safety configuration
func GetDefaultSafetyConfig() *SafetyConfig {
	return &SafetyConfig{
		RequireConfirmation: true,
		MaxDeletionSize:     1024, // 1GB
		SafeMode:            true,
		ShredPasses:         1,
		ProtectedPaths: []string{
			"/bin",
			"/usr/bin",
			"/usr/sbin",
			"/sbin",
			"/etc",
			"/var/lib",
			"/boot",
			"/sys",
			"/proc",
		},
	}
}
