package cleaner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/Nomadcxx/moonbit/internal/config"
	moonbiterrors "github.com/Nomadcxx/moonbit/internal/errors"
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
	auditLog      *audit.Logger
}

// NewCleaner creates a new cleaner instance
func NewCleaner(cfg *config.Config) *Cleaner {
	safetyCfg := &SafetyConfig{
		RequireConfirmation: true,
		MaxDeletionSize:     512000,
		SafeMode:            true,
		ShredPasses:         1,
		ProtectedPaths: []string{
			"/bin",
			"/usr/bin",
			"/usr/sbin",
			"/sbin",
			"/etc",
			"/boot",
			"/sys",
			"/proc",
		},
	}

	auditLog, err := audit.NewLogger()
	if err != nil {
		log.Printf("Warning: Failed to create audit logger: %v", err)
	}

	return &Cleaner{
		cfg:           cfg,
		safetyConfig:  safetyCfg,
		backupEnabled: false,
		auditLog:      auditLog,
	}
}

// Close closes the audit logger if it exists
func (c *Cleaner) Close() error {
	if c.auditLog != nil {
		return c.auditLog.Close()
	}
	return nil
}

// CleanCategory cleans files from a specific category
func (c *Cleaner) CleanCategory(ctx context.Context, category *config.Category, dryRun bool, progressCh chan<- CleanMsg) error {
	defer close(progressCh)

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

	filesDeleted := 0
	filesFailed := 0
	bytesFreed := uint64(0)
	var errorMessages []string

	for _, fileInfo := range category.Files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		progressCh <- CleanMsg{
			Progress: &CleanProgress{
				FilesProcessed: filesDeleted + filesFailed,
				BytesFreed:     bytesFreed,
				CurrentFile:    fileInfo.Path,
				TotalFiles:     len(category.Files),
				TotalBytes:     category.Size,
			},
		}

		if dryRun {
			filesDeleted++
			bytesFreed += fileInfo.Size
		} else {
			if err := c.deleteFile(fileInfo.Path, category.ShredEnabled); err != nil {
				filesFailed++
				errorMessages = append(errorMessages, fmt.Sprintf("%s: %v", fileInfo.Path, err))
				continue
			}
			filesDeleted++
			bytesFreed += fileInfo.Size
		}
	}

	duration := time.Since(start)

	var cleanErr error
	if filesFailed > 0 {
		cleanErr = moonbiterrors.NewCleanFailedError(
			category.Name,
			len(category.Files),
			filesDeleted,
			filesFailed,
			errorMessages,
		)
	}

	if c.auditLog != nil {
		c.auditLog.LogCleanOperation(filesDeleted, bytesFreed, cleanErr)
	}

	progressCh <- CleanMsg{
		Complete: &CleanComplete{
			Category:      category.Name,
			FilesDeleted:  filesDeleted,
			BytesFreed:    bytesFreed,
			Duration:      duration,
			BackupCreated: backupPath != "",
			BackupPath:    backupPath,
			Errors:        errorMessages,
		},
	}

	if cleanErr != nil {
		progressCh <- CleanMsg{Error: cleanErr}
	}

	return nil
}

// DeleteFile performs actual file deletion with optional shredding
func (c *Cleaner) deleteFile(path string, shredEnabled bool) error {
	if c.isProtectedPath(path) {
		return moonbiterrors.NewPathProtectedError(path, c.safetyConfig.ProtectedPaths)
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return moonbiterrors.NewFileNotFoundError(path, err)
		}
		if os.IsPermission(err) {
			return moonbiterrors.NewPermissionDeniedError(path, err)
		}
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if shredEnabled && info.Size() > 0 {
		if err := c.shredFile(path, info.Size()); err != nil {
			return fmt.Errorf("failed to shred file: %w", err)
		}
	}

	err = os.Remove(path)
	if err != nil && os.IsPermission(err) {
		return moonbiterrors.NewPermissionDeniedError(path, err)
	}
	return err
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

func (c *Cleaner) performSafetyChecks(category *config.Category, dryRun bool) error {
	if category.Risk == config.High && !dryRun {
		if c.safetyConfig.SafeMode {
			return fmt.Errorf("high-risk category '%s' requires manual confirmation", category.Name)
		}
	}

	maxBytes := c.safetyConfig.MaxDeletionSize * 1024 * 1024
	if category.Size > maxBytes {
		return moonbiterrors.NewSafetyCheckFailedError(
			"category size exceeds maximum allowed",
			category.Name,
			category.Size,
			maxBytes,
		)
	}

	for _, fileInfo := range category.Files {
		if c.isProtectedPath(fileInfo.Path) {
			return moonbiterrors.NewPathProtectedError(fileInfo.Path, c.safetyConfig.ProtectedPaths)
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
		// Ensure protected path ends with separator for proper boundary checking
		protectedWithSep := protected
		if !strings.HasSuffix(protected, string(filepath.Separator)) {
			protectedWithSep = protected + string(filepath.Separator)
		}

		// Check if path starts with protected directory
		if absPath == protected || strings.HasPrefix(absPath, protectedWithSep) {
			return true
		}
	}

	return false
}

func (c *Cleaner) createBackup(category *config.Category) string {
	timestamp := time.Now().Format("20060102_150405")

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Printf("ERROR: Failed to get user home directory: %v", err)
			return ""
		}
		dataHome = filepath.Join(homeDir, ".local", "share")
	}

	backupDir := filepath.Join(dataHome, "moonbit", "backups")

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		mbErr := moonbiterrors.NewBackupFailedError(category.Name, err)
		log.Printf("ERROR: %s", mbErr.UserMessage())
		return ""
	}

	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_%s.backup",
		sanitizeName(category.Name), timestamp))

	if err := c.createBackupMetadata(backupPath, category, timestamp); err != nil {
		mbErr := moonbiterrors.NewBackupFailedError(category.Name, err)
		log.Printf("ERROR: %s", mbErr.UserMessage())
		return ""
	}

	backupFilesDir := backupPath + ".files"
	if err := os.MkdirAll(backupFilesDir, 0755); err != nil {
		mbErr := moonbiterrors.NewBackupFailedError(category.Name, err)
		log.Printf("ERROR: %s", mbErr.UserMessage())
		return ""
	}

	for _, file := range category.Files {
		if err := c.backupFile(file.Path, backupFilesDir); err != nil {
			log.Printf("ERROR: Failed to backup file %s: %v", file.Path, err)
			continue
		}
	}

	return backupPath
}

// sanitizeName removes special characters from names for safe filenames
func sanitizeName(name string) string {
	// Replace spaces and special chars with underscores
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}

// createBackupMetadata creates a JSON metadata file for the backup
func (c *Cleaner) createBackupMetadata(backupPath string, category *config.Category, timestamp string) error {
	metadata := map[string]interface{}{
		"created_at": time.Now().Format(time.RFC3339),
		"timestamp":  timestamp,
		"category":   category.Name,
		"file_count": len(category.Files),
		"total_size": category.Size,
		"files":      category.Files,
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		log.Printf("ERROR: Failed to marshal backup metadata for %s: %v", category.Name, err)
		return err
	}

	metaPath := backupPath + ".json"
	return os.WriteFile(metaPath, data, 0644)
}

// backupFile copies a single file to backup directory
func (c *Cleaner) backupFile(srcPath, backupDir string) error {
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat source file %s: %w", srcPath, err)
	}

	// Skip if it's a directory (we only backup files)
	if srcInfo.IsDir() {
		return nil
	}

	// Create safe filename (hash of original path to avoid collisions)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(srcPath)))
	dstPath := filepath.Join(backupDir, hash[:16])

	// Copy file
	src, err := os.Open(srcPath)
	if err != nil {
		log.Printf("ERROR: Failed to open source file %s: %v", srcPath, err)
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		log.Printf("ERROR: Failed to create backup destination %s: %v", dstPath, err)
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		log.Printf("ERROR: Failed to copy file %s to backup: %v", srcPath, err)
	}
	return err
}

// RestoreBackup restores files from a backup
func RestoreBackup(backupPath string) error {
	// Read metadata
	metaPath := backupPath + ".json"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("failed to read backup metadata: %w", err)
	}

	var metadata struct {
		Files []config.FileInfo `json:"files"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return fmt.Errorf("failed to parse backup metadata: %w", err)
	}

	backupFilesDir := backupPath + ".files"

	// Restore each file
	for _, file := range metadata.Files {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(file.Path)))
		srcPath := filepath.Join(backupFilesDir, hash[:16])

		if _, err := os.Stat(srcPath); err != nil {
			log.Printf("ERROR: Backup file not found for %s: %v", file.Path, err)
			continue
		}

		targetDir := filepath.Dir(file.Path)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			log.Printf("ERROR: Failed to create target directory %s: %v", targetDir, err)
			continue
		}

		src, err := os.Open(srcPath)
		if err != nil {
			log.Printf("ERROR: Failed to open backup file %s: %v", srcPath, err)
			continue
		}

		dst, err := os.Create(file.Path)
		if err != nil {
			src.Close()
			log.Printf("ERROR: Failed to create target file %s: %v", file.Path, err)
			continue
		}

		_, copyErr := io.Copy(dst, src)
		src.Close()
		dst.Close()

		if copyErr != nil {
			os.Remove(file.Path)
			log.Printf("ERROR: Failed to copy file %s to %s: %v", srcPath, file.Path, copyErr)
			continue
		}
	}

	return nil
}

// ListBackups returns a list of available backups
func ListBackups() ([]string, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dataHome = filepath.Join(homeDir, ".local", "share")
	}

	backupDir := filepath.Join(dataHome, "moonbit", "backups")

	// Check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []string{}, nil
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, err
	}

	var backups []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".backup") {
			backups = append(backups, entry.Name())
		}
	}

	return backups, nil
}

// GetDefaultSafetyConfig returns default safety configuration
func GetDefaultSafetyConfig() *SafetyConfig {
	return &SafetyConfig{
		RequireConfirmation: true,
		MaxDeletionSize:     512000, // 500GB (in MB) - increased for systemd journals and large caches
		SafeMode:            true,
		ShredPasses:         1,
		ProtectedPaths: []string{
			"/bin",
			"/usr/bin",
			"/usr/sbin",
			"/sbin",
			"/etc",
			"/boot",
			"/sys",
			"/proc",
			// Note: /var/lib removed to allow Docker cleanup
			// Categories should be specific about what they clean
		},
	}
}
