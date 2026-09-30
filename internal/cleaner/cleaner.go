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
	"syscall"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/Nomadcxx/moonbit/internal/config"
	moonbiterrors "github.com/Nomadcxx/moonbit/internal/errors"
	"github.com/Nomadcxx/moonbit/internal/paths"
	"github.com/Nomadcxx/moonbit/internal/validation"
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
	Category       string
	FilesDeleted   int
	FilesTruncated int
	BytesFreed     uint64
	Duration       time.Duration
	BackupCreated  bool
	BackupPath     string
	Errors         []string
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
	homePath      string
}

type backupFileMetadata struct {
	config.FileInfo
	Mode   *uint32          `json:"mode,omitempty"`
	Owner  *backupFileOwner `json:"owner,omitempty"`
	SHA256 string           `json:"sha256,omitempty"`
}

type backupFileOwner struct {
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`
}

// NewCleaner creates a new cleaner instance
func NewCleaner(cfg *config.Config) *Cleaner {
	homePath, err := paths.HomeDir()
	if err == nil {
		homePath, err = filepath.Abs(homePath)
	}
	if err != nil {
		homePath = ""
	}
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
			"/home",
			"/root",
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
		homePath:      homePath,
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
	filesTruncated := 0
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
				FilesProcessed: filesDeleted + filesTruncated + filesFailed,
				BytesFreed:     bytesFreed,
				CurrentFile:    fileInfo.Path,
				TotalFiles:     len(category.Files),
				TotalBytes:     category.Size,
			},
		}

		if dryRun {
			if fileCleanAction(category, fileInfo) == config.ActionTruncate {
				filesTruncated++
			} else {
				filesDeleted++
			}
			bytesFreed += fileInfo.Size
		} else {
			// Shredding is enabled per file by the revalidation gate, which reads
			// it from config. The category-level flag is only meaningful for
			// callers passing a config category directly.
			shred := category.ShredEnabled || fileInfo.CategoryShred
			action := fileCleanAction(category, fileInfo)

			var freed uint64
			var err error
			if action == config.ActionTruncate {
				freed, err = c.truncateScannedFile(fileInfo)
			} else {
				freed, err = c.deleteScannedFile(fileInfo, shred)
			}
			if err != nil {
				filesFailed++
				errorMessages = append(errorMessages, fmt.Sprintf("%s: %v", fileInfo.Path, err))
				continue
			}
			if action == config.ActionTruncate {
				filesTruncated++
			} else {
				filesDeleted++
			}
			bytesFreed += freed
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
			Category:       category.Name,
			FilesDeleted:   filesDeleted,
			FilesTruncated: filesTruncated,
			BytesFreed:     bytesFreed,
			Duration:       duration,
			BackupCreated:  backupPath != "",
			BackupPath:     backupPath,
			Errors:         errorMessages,
		},
	}

	if cleanErr != nil {
		progressCh <- CleanMsg{Error: cleanErr}
	}

	return cleanErr
}

func fileCleanAction(category *config.Category, file config.FileInfo) config.CleanAction {
	action := file.CategoryAction
	if action == config.ActionDelete {
		action = category.Action
	}
	return action
}

// truncateFile reclaims a file's space without unlinking it, for files a running
// daemon holds open. Unlinking those frees nothing until the last descriptor
// closes and leaves the writer with a nameless handle; truncation frees the
// space immediately and the writer keeps working.
//
// It returns the number of bytes reclaimed.
func (c *Cleaner) truncateFile(path string) (uint64, error) {
	return c.truncateFileExpected(path, nil)
}

func (c *Cleaner) truncateScannedFile(fileInfo config.FileInfo) (uint64, error) {
	return c.truncateFileExpected(fileInfo.Path, &fileInfo)
}

func (c *Cleaner) truncateFileExpected(path string, expected *config.FileInfo) (uint64, error) {
	if c.isProtectedPath(path) {
		return 0, moonbiterrors.NewPathProtectedError(path, c.safetyConfig.ProtectedPaths)
	}

	file, err := paths.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, moonbiterrors.NewFileNotFoundError(path, err)
		}
		if os.IsPermission(err) {
			return 0, moonbiterrors.NewPermissionDeniedError(path, err)
		}
		return 0, fmt.Errorf("failed to open file for truncation: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("refusing to truncate %s: not a regular file (mode %s)", path, info.Mode())
	}
	if err := matchesScannedIdentity(info, expected); err != nil {
		return 0, fmt.Errorf("refusing to truncate changed file %s: %w", path, err)
	}

	freed := uint64(info.Size())
	if freed == 0 {
		return 0, nil
	}
	if err := file.Truncate(0); err != nil {
		return 0, fmt.Errorf("failed to truncate: %w", err)
	}
	return freed, nil
}

// deleteFile removes a single file, optionally shredding it first. It returns
// the number of bytes actually reclaimed from disk, measured immediately before
// removal rather than taken from the (possibly stale) scan record.
func (c *Cleaner) deleteFile(path string, shredEnabled bool) (uint64, error) {
	return c.deleteFileExpected(path, shredEnabled, nil)
}

func (c *Cleaner) deleteScannedFile(fileInfo config.FileInfo, shredEnabled bool) (uint64, error) {
	return c.deleteFileExpected(fileInfo.Path, shredEnabled, &fileInfo)
}

func (c *Cleaner) deleteFileExpected(path string, shredEnabled bool, expected *config.FileInfo) (uint64, error) {
	if c.isProtectedPath(path) {
		return 0, moonbiterrors.NewPathProtectedError(path, c.safetyConfig.ProtectedPaths)
	}

	openFlag := os.O_RDONLY
	if shredEnabled {
		openFlag = os.O_WRONLY
	}
	file, err := paths.OpenFile(path, openFlag, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, moonbiterrors.NewFileNotFoundError(path, err)
		}
		if os.IsPermission(err) {
			return 0, moonbiterrors.NewPermissionDeniedError(path, err)
		}
		return 0, fmt.Errorf("failed to open file for deletion: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat file: %w", err)
	}

	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("refusing to delete %s: not a regular file (mode %s)", path, info.Mode())
	}
	if err := matchesScannedIdentity(info, expected); err != nil {
		return 0, fmt.Errorf("refusing to delete changed file %s: %w", path, err)
	}

	// Size on disk now, not the size recorded at scan time. A log the writing
	// daemon truncated between scan and clean must not be reported at its old size.
	freed := uint64(info.Size())

	if shredEnabled && info.Size() > 0 {
		if err := c.shredOpenFile(file, info.Size()); err != nil {
			return 0, fmt.Errorf("failed to shred file: %w", err)
		}
	}

	err = paths.RemoveIf(path, func(info os.FileInfo) error {
		if err := matchesScannedIdentity(info, expected); err != nil {
			return fmt.Errorf("file identity changed before unlink: %w", err)
		}
		return nil
	})
	if err != nil {
		if os.IsPermission(err) {
			return 0, moonbiterrors.NewPermissionDeniedError(path, err)
		}
		return 0, err
	}
	return freed, nil
}

func matchesScannedIdentity(info os.FileInfo, expected *config.FileInfo) error {
	if expected == nil || expected.FileID == "" {
		return nil
	}
	if paths.FileID(info) != expected.FileID {
		return fmt.Errorf("file identity changed since scan")
	}
	if uint64(info.Size()) != expected.Size {
		return fmt.Errorf("file size changed since scan")
	}
	if expected.ModTime != "" {
		if info.ModTime().Format(time.RFC3339Nano) != expected.ModTime {
			return fmt.Errorf("file modification time changed since scan")
		}
	}
	return nil
}

// ShredFile overwrites a file with random data before deletion
func (c *Cleaner) shredFile(path string, size int64) error {
	file, err := paths.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return c.shredOpenFile(file, size)
}

func (c *Cleaner) shredOpenFile(file *os.File, size int64) error {
	st, err := file.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("refusing to shred %s: not a regular file (mode %s)", file.Name(), st.Mode())
	}
	if st.Size() < size {
		size = st.Size()
	}

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

	// Check the literal path and, when it resolves, the symlink target as well.
	// filepath.Abs does not resolve symlinks, so on its own it judges a link by
	// its own location -- letting a link in an unprotected directory point at a
	// protected one. Both must clear the list.
	candidates := []string{absPath}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil && resolved != absPath {
		candidates = append(candidates, resolved)
	}

	for _, candidate := range candidates {
		for _, protected := range c.safetyConfig.ProtectedPaths {
			// Ensure protected path ends with separator for proper boundary checking
			protectedWithSep := protected
			if !strings.HasSuffix(protected, string(filepath.Separator)) {
				protectedWithSep = protected + string(filepath.Separator)
			}

			// Check if path starts with protected directory
			if candidate == protected || strings.HasPrefix(candidate, protectedWithSep) {
				if (protected == "/home" || protected == "/root") && paths.IsWithin(c.homePath, candidate) {
					continue
				}
				return true
			}
		}
	}

	return false
}

func safeRestoreCategories(cfg *config.Config) ([]config.Category, string, error) {
	home, err := paths.HomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("resolve invoking user's home: %w", err)
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return nil, "", fmt.Errorf("resolve invoking user's home: %w", err)
	}

	// Built-in system targets and their filters come from the binary. A
	// user-owned config may add custom cache paths under that user's home, but
	// cannot authorize privileged restores elsewhere or weaken built-in rules.
	categories := config.AuthoritativeCategories(config.DefaultConfig())
	seen := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		seen[strings.ToLower(strings.TrimSpace(category.Name))] = struct{}{}
	}
	if cfg != nil {
		for _, category := range cfg.Categories {
			name := strings.ToLower(strings.TrimSpace(category.Name))
			if _, exists := seen[name]; exists || !categoryPathsWithinHome(category, home) {
				continue
			}
			categories = append(categories, category)
			seen[name] = struct{}{}
		}
	}
	return categories, home, nil
}

func categoryPathsWithinHome(category config.Category, home string) bool {
	if len(category.Paths) == 0 {
		return false
	}
	for _, pattern := range category.Paths {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return false
		}
		if len(matches) == 0 {
			matches = []string{pattern}
		}
		for _, match := range matches {
			absPath, err := filepath.Abs(match)
			if err != nil || !paths.IsWithin(home, absPath) {
				return false
			}
		}
	}
	return true
}

func validateBackupOwner(info os.FileInfo, expectedUID uint32) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != expectedUID {
		return fmt.Errorf("backup file is not owned by uid %d", expectedUID)
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("backup file permissions %04o are not private", info.Mode().Perm())
	}
	return nil
}

func (c *Cleaner) createBackup(category *config.Category) string {
	timestamp := time.Now().Format("20060102_150405")

	backupDir, err := paths.DataDir("backups")
	if err != nil {
		log.Printf("ERROR: Failed to determine backup directory: %v", err)
		return ""
	}

	if err := paths.MkdirAll(backupDir, 0700); err != nil {
		mbErr := moonbiterrors.NewBackupFailedError(category.Name, err)
		log.Printf("ERROR: %s", mbErr.UserMessage())
		return ""
	}
	if err := paths.ChmodDir(backupDir, 0700); err != nil {
		mbErr := moonbiterrors.NewBackupFailedError(category.Name, err)
		log.Printf("ERROR: %s", mbErr.UserMessage())
		return ""
	}

	var backupID [16]byte
	if _, err := rand.Read(backupID[:]); err != nil {
		log.Printf("ERROR: Failed to create a unique backup name: %v", err)
		return ""
	}
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_%s_%x.backup",
		sanitizeName(category.Name), timestamp, backupID))

	backupFilesDir := backupPath + ".files"
	if err := paths.MkdirAll(backupFilesDir, 0700); err != nil {
		mbErr := moonbiterrors.NewBackupFailedError(category.Name, err)
		log.Printf("ERROR: %s", mbErr.UserMessage())
		return ""
	}
	if err := paths.ChmodDir(backupFilesDir, 0700); err != nil {
		mbErr := moonbiterrors.NewBackupFailedError(category.Name, err)
		log.Printf("ERROR: %s", mbErr.UserMessage())
		return ""
	}

	backupFiles := make([]string, 0, len(category.Files))
	backupMetadata := make([]backupFileMetadata, 0, len(category.Files))
	cleanupBackupFiles := func() {
		for _, path := range backupFiles {
			if err := paths.Remove(path); err != nil && !os.IsNotExist(err) {
				log.Printf("ERROR: Failed to remove incomplete backup file %s: %v", path, err)
			}
		}
	}

	for _, file := range category.Files {
		fileMetadata, err := c.backupFile(file.Path, backupFilesDir)
		if err != nil {
			log.Printf("ERROR: Failed to backup file %s: %v", file.Path, err)
			cleanupBackupFiles()
			return ""
		}
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(file.Path)))
		backupFiles = append(backupFiles, filepath.Join(backupFilesDir, hash[:16]))
		if fileMetadata != nil {
			fileMetadata.FileInfo = file
			backupMetadata = append(backupMetadata, *fileMetadata)
		}
	}

	if err := c.createBackupMetadata(backupPath, category, timestamp, backupMetadata...); err != nil {
		cleanupBackupFiles()
		mbErr := moonbiterrors.NewBackupFailedError(category.Name, err)
		log.Printf("ERROR: %s", mbErr.UserMessage())
		return ""
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
func (c *Cleaner) createBackupMetadata(backupPath string, category *config.Category, timestamp string, files ...backupFileMetadata) error {
	metadata := map[string]interface{}{
		"created_at": time.Now().Format(time.RFC3339),
		"timestamp":  timestamp,
		"category":   category.Name,
		"file_count": len(files),
		"total_size": category.Size,
		"files":      files,
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		log.Printf("ERROR: Failed to marshal backup metadata for %s: %v", category.Name, err)
		return err
	}

	metaPath := backupPath + ".json"
	file, err := paths.OpenFile(metaPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		if removeErr := paths.Remove(metaPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("ERROR: Failed to remove incomplete backup metadata %s: %v", metaPath, removeErr)
		}
		return err
	}
	if err := file.Close(); err != nil {
		if removeErr := paths.Remove(metaPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("ERROR: Failed to remove incomplete backup metadata %s: %v", metaPath, removeErr)
		}
		return err
	}
	return nil
}

// backupFile copies a single file to backup directory
func (c *Cleaner) backupFile(srcPath, backupDir string) (*backupFileMetadata, error) {
	src, err := paths.OpenFile(srcPath, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open source file %s: %w", srcPath, err)
	}
	defer src.Close()
	srcInfo, err := src.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat source file %s: %w", srcPath, err)
	}

	// Skip if it's a directory (we only backup files)
	if srcInfo.IsDir() {
		return nil, nil
	}
	if !srcInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to back up non-regular file %s", srcPath)
	}

	// Create safe filename (hash of original path to avoid collisions)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(srcPath)))
	dstPath := filepath.Join(backupDir, hash[:16])

	// Copy file
	dst, err := paths.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("ERROR: Failed to create backup destination %s: %v", dstPath, err)
		return nil, err
	}

	digest := sha256.New()
	copied, err := io.Copy(io.MultiWriter(dst, digest), src)
	if err == nil {
		err = dst.Sync()
	}
	currentInfo, statErr := src.Stat()
	if err == nil && statErr != nil {
		err = fmt.Errorf("stat source file after copy: %w", statErr)
	}
	if err == nil && (copied != srcInfo.Size() || currentInfo.Size() != srcInfo.Size() || !currentInfo.ModTime().Equal(srcInfo.ModTime())) {
		err = fmt.Errorf("source file changed during backup")
	}
	closeErr := dst.Close()
	if err != nil {
		log.Printf("ERROR: Failed to copy file %s to backup: %v", srcPath, err)
		if removeErr := paths.Remove(dstPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("ERROR: Failed to remove incomplete backup file %s: %v", dstPath, removeErr)
		}
		return nil, err
	}
	if closeErr != nil {
		if removeErr := paths.Remove(dstPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("ERROR: Failed to remove incomplete backup file %s: %v", dstPath, removeErr)
		}
		return nil, closeErr
	}

	mode := uint32(srcInfo.Mode().Perm())
	metadata := &backupFileMetadata{
		FileInfo: config.FileInfo{
			Path: srcPath, Size: uint64(srcInfo.Size()), ModTime: srcInfo.ModTime().Format(time.RFC3339Nano),
		},
		Mode:   &mode,
		SHA256: fmt.Sprintf("%x", digest.Sum(nil)),
	}
	if stat, ok := srcInfo.Sys().(*syscall.Stat_t); ok {
		metadata.Owner = &backupFileOwner{UID: stat.Uid, GID: stat.Gid}
	}
	return metadata, nil
}

func restoreFile(path string, source io.Reader) error {
	return restoreFileWithMetadata(path, source, nil)
}

func restoreFileWithMetadata(path string, source io.Reader, metadata *backupFileMetadata) error {
	mode := os.FileMode(0600)
	var previous os.FileInfo
	current, err := paths.OpenFile(path, os.O_RDONLY, 0)
	if err == nil {
		previous, err = current.Stat()
		closeErr := current.Close()
		if err != nil {
			return fmt.Errorf("stat restore destination: %w", err)
		}
		if closeErr != nil {
			return fmt.Errorf("close restore destination: %w", closeErr)
		}
		mode = previous.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect restore destination: %w", err)
	}
	if metadata != nil && metadata.Mode != nil {
		mode = os.FileMode(*metadata.Mode).Perm()
	}

	return paths.AtomicWriteFile(path, mode, func(dst *os.File) error {
		if metadata != nil && metadata.Owner != nil && os.Geteuid() == 0 {
			if err := dst.Chown(int(metadata.Owner.UID), int(metadata.Owner.GID)); err != nil {
				return fmt.Errorf("restore original file ownership: %w", err)
			}
		} else if previous != nil {
			if stat, ok := previous.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
				if err := dst.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
					return fmt.Errorf("preserve restore destination ownership: %w", err)
				}
			}
		}
		if metadata == nil || metadata.SHA256 == "" {
			if _, err := io.Copy(dst, source); err != nil {
				return err
			}
		} else {
			digest := sha256.New()
			if _, err := io.Copy(dst, io.TeeReader(source, digest)); err != nil {
				return err
			}
			if fmt.Sprintf("%x", digest.Sum(nil)) != strings.ToLower(metadata.SHA256) {
				return fmt.Errorf("backup file checksum mismatch")
			}
		}
		if err := dst.Chmod(mode); err != nil {
			return fmt.Errorf("restore file permissions: %w", err)
		}
		return nil
	})
}

// RestoreBackup restores files from a backup
func RestoreBackup(backupPath string) error {
	backupDir, err := paths.DataDir("backups")
	if err != nil {
		return fmt.Errorf("failed to determine backup directory: %w", err)
	}
	absBackupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return fmt.Errorf("failed to resolve backup directory: %w", err)
	}
	absBackupPath, err := filepath.Abs(backupPath)
	if err != nil {
		return fmt.Errorf("invalid backup path: %w", err)
	}
	backupName := filepath.Base(absBackupPath)
	if backupName == "." || backupName == ".." ||
		strings.ContainsAny(backupName, `/\\`) || !strings.HasSuffix(backupName, ".backup") ||
		absBackupPath != filepath.Join(absBackupDir, backupName) {
		return fmt.Errorf("backup path must name a .backup file directly under %s", absBackupDir)
	}
	backupPath = absBackupPath

	metaPath := backupPath + ".json"
	metaFile, err := paths.OpenFile(metaPath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to read backup metadata: %w", err)
	}
	metaInfo, err := metaFile.Stat()
	if err != nil {
		metaFile.Close()
		return fmt.Errorf("failed to stat backup metadata: %w", err)
	}
	if !metaInfo.Mode().IsRegular() {
		metaFile.Close()
		return fmt.Errorf("backup metadata is not a regular file")
	}
	if os.Geteuid() == 0 {
		if err := validateBackupOwner(metaInfo, 0); err != nil {
			metaFile.Close()
			return fmt.Errorf("unsafe backup metadata: %w", err)
		}
	}
	var metadata struct {
		Category string               `json:"category"`
		Files    []backupFileMetadata `json:"files"`
	}
	if err := json.NewDecoder(metaFile).Decode(&metadata); err != nil {
		metaFile.Close()
		return fmt.Errorf("failed to parse backup metadata: %w", err)
	}
	if err := metaFile.Close(); err != nil {
		return fmt.Errorf("failed to close backup metadata: %w", err)
	}

	cfg, err := config.LoadReadOnly("")
	if err != nil {
		return fmt.Errorf("failed to load config for restore: %w", err)
	}
	categories, homePath, err := safeRestoreCategories(cfg)
	if err != nil {
		return fmt.Errorf("failed to determine safe restore categories: %w", err)
	}
	restoreValidator, err := validation.NewRestorePathValidator(categories)
	if err != nil {
		return fmt.Errorf("failed to validate restore category rules: %w", err)
	}
	restoreGuard := &Cleaner{safetyConfig: GetDefaultSafetyConfig(), homePath: homePath}

	backupFilesDir := backupPath + ".files"
	var restoreErrors []string

	// Restore each file
	for _, file := range metadata.Files {
		categoryName := metadata.Category
		if file.CategoryName != "" && !strings.EqualFold(strings.TrimSpace(file.CategoryName), strings.TrimSpace(categoryName)) {
			msg := fmt.Sprintf("backup file category %q does not match manifest category %q", file.CategoryName, categoryName)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}
		if restoreGuard.isProtectedPath(file.Path) {
			msg := fmt.Sprintf("protected restore destination %s", file.Path)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}
		if err := restoreValidator.ValidatePath(file.Path, categoryName); err != nil {
			msg := fmt.Sprintf("unsafe restore destination %s: %v", file.Path, err)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}

		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(file.Path)))
		srcPath := filepath.Join(backupFilesDir, hash[:16])
		src, err := paths.OpenFile(srcPath, os.O_RDONLY, 0)
		if err != nil {
			msg := fmt.Sprintf("failed to open backup file for %s: %v", file.Path, err)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}
		srcInfo, err := src.Stat()
		if err != nil {
			src.Close()
			msg := fmt.Sprintf("failed to stat backup file for %s: %v", file.Path, err)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}
		if !srcInfo.Mode().IsRegular() {
			src.Close()
			msg := fmt.Sprintf("backup file for %s is not a regular file", file.Path)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}
		if os.Geteuid() == 0 {
			if err := validateBackupOwner(srcInfo, 0); err != nil {
				src.Close()
				msg := fmt.Sprintf("unsafe backup file for %s: %v", file.Path, err)
				log.Printf("ERROR: %s", msg)
				restoreErrors = append(restoreErrors, msg)
				continue
			}
		}
		if srcInfo.Size() < 0 || uint64(srcInfo.Size()) != file.Size {
			src.Close()
			msg := fmt.Sprintf("backup file for %s has size %d, expected %d", file.Path, srcInfo.Size(), file.Size)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}

		targetDir := filepath.Dir(file.Path)
		if err := paths.MkdirAll(targetDir, 0755); err != nil {
			src.Close()
			msg := fmt.Sprintf("failed to create target directory %s: %v", targetDir, err)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}

		restoreErr := restoreFileWithMetadata(file.Path, src, &file)
		srcCloseErr := src.Close()
		if restoreErr == nil {
			restoreErr = srcCloseErr
		}

		if restoreErr != nil {
			msg := fmt.Sprintf("failed to restore file %s to %s: %v", srcPath, file.Path, restoreErr)
			log.Printf("ERROR: %s", msg)
			restoreErrors = append(restoreErrors, msg)
			continue
		}
	}

	if len(restoreErrors) > 0 {
		return fmt.Errorf("restore incomplete: %d file(s) failed: %s", len(restoreErrors), strings.Join(restoreErrors, "; "))
	}

	return nil
}

// ListBackups returns a list of available backups
func ListBackups() ([]string, error) {
	backupDir, err := paths.DataDir("backups")
	if err != nil {
		return nil, err
	}

	entries, err := paths.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}

	var backups []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".backup.json") {
			continue
		}
		metadata, err := paths.OpenFile(filepath.Join(backupDir, name), os.O_RDONLY, 0)
		if err != nil {
			continue
		}
		if err := metadata.Close(); err != nil {
			continue
		}
		backupName := strings.TrimSuffix(name, ".json")
		if _, err := paths.ReadDir(filepath.Join(backupDir, backupName+".files")); err == nil {
			backups = append(backups, backupName)
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
			"/home",
			"/root",
			"/sys",
			"/proc",
			// Note: /var/lib removed to allow Docker cleanup
			// Categories should be specific about what they clean
		},
	}
}
