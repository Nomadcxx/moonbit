package scanner

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/karrick/godirwalk"
	"github.com/spf13/afero"
)

// FileSystem provides an abstraction for filesystem operations
type FileSystem interface {
	Stat(name string) (os.FileInfo, error)
	Walk(root string, walkFunc filepath.WalkFunc) error
	ReadDir(dirname string) ([]os.FileInfo, error)
}

// OsFileSystem implements FileSystem for real OS filesystem using godirwalk
type OsFileSystem struct{}

func (fs *OsFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (fs *OsFileSystem) Walk(root string, walkFunc filepath.WalkFunc) error {
	return godirwalk.Walk(root, &godirwalk.Options{
		FollowSymbolicLinks: false,
		AllowNonDirectory:   false,
		Unsorted:            false,
		Callback: func(osPathname string, de *godirwalk.Dirent) error {
			info, err := os.Stat(osPathname)
			if err != nil {
				return walkFunc(osPathname, nil, err)
			}
			return walkFunc(osPathname, info, nil)
		},
	})
}

func (fs *OsFileSystem) ReadDir(dirname string) ([]os.FileInfo, error) {
	entries, err := afero.ReadDir(afero.NewOsFs(), dirname)
	if err != nil {
		return nil, err
	}
	// Convert []afero.FileInfo to []os.FileInfo
	result := make([]os.FileInfo, len(entries))
	for i, entry := range entries {
		result[i] = entry
	}
	return result, nil
}

// AferoFileSystem implements FileSystem for afero filesystems
type AferoFileSystem struct {
	fs afero.Fs
}

func NewAferoFileSystem(fs afero.Fs) *AferoFileSystem {
	return &AferoFileSystem{fs: fs}
}

func (fs *AferoFileSystem) Stat(name string) (os.FileInfo, error) {
	return fs.fs.Stat(name)
}

func (fs *AferoFileSystem) Walk(root string, walkFunc filepath.WalkFunc) error {
	return afero.Walk(fs.fs, root, walkFunc)
}

func (fs *AferoFileSystem) ReadDir(dirname string) ([]os.FileInfo, error) {
	return afero.ReadDir(fs.fs, dirname)
}

// ScanProgress represents progress updates during scanning
type ScanProgress struct {
	Path         string
	Bytes        uint64
	FilesScanned int
	DirsScanned  int
	CurrentDir   string
}

// ScanComplete represents the completion of a category scan
type ScanComplete struct {
	Category string
	Stats    *config.Category
	Duration time.Duration
}

// ScanMsg represents messages from the scanner
type ScanMsg struct {
	Progress *ScanProgress
	Complete *ScanComplete
	Error    error
}

// Scanner handles directory scanning for cleanup
type Scanner struct {
	cfg     *config.Config
	filter  *regexp.Regexp
	workers int
	fs      FileSystem
}

// NewScanner creates a new scanner instance
func NewScanner(cfg *config.Config) *Scanner {
	// Compile filter patterns
	filterStr := "("
	for i, pattern := range cfg.Scan.IgnorePatterns {
		if i > 0 {
			filterStr += "|"
		}
		filterStr += pattern
	}
	filterStr += ")"

	filter := regexp.MustCompile(filterStr)

	// Determine worker count: use config value, or auto-detect based on CPU count
	workerCount := cfg.Scan.WorkerCount
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
		if workerCount < 2 {
			workerCount = 2 // Minimum 2 workers
		}
		if workerCount > 16 {
			workerCount = 16 // Cap at 16 to avoid excessive goroutines
		}
	}

	return &Scanner{
		cfg:     cfg,
		filter:  filter,
		workers: workerCount,
		fs:      &OsFileSystem{},
	}
}

// NewScannerWithFs creates a new scanner instance with a specific filesystem
func NewScannerWithFs(cfg *config.Config, fs FileSystem) *Scanner {
	// Compile filter patterns
	filterStr := "("
	for i, pattern := range cfg.Scan.IgnorePatterns {
		if i > 0 {
			filterStr += "|"
		}
		filterStr += pattern
	}
	filterStr += ")"

	filter := regexp.MustCompile(filterStr)

	// Determine worker count: use config value, or auto-detect based on CPU count
	workerCount := cfg.Scan.WorkerCount
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
		if workerCount < 2 {
			workerCount = 2 // Minimum 2 workers
		}
		if workerCount > 16 {
			workerCount = 16 // Cap at 16 to avoid excessive goroutines
		}
	}

	return &Scanner{
		cfg:     cfg,
		filter:  filter,
		workers: workerCount,
		fs:      fs,
	}
}

// ScanCategory scans a specific category
func (s *Scanner) ScanCategory(ctx context.Context, category *config.Category, progressCh chan<- ScanMsg) {
	defer close(progressCh)

	start := time.Now()

	stats := *category
	stats.Files = []config.FileInfo{} // Reset files for fresh scan
	stats.Size = 0
	stats.FileCount = 0
	stats.Selected = true // Mark as selected for scanning

	// Process each path in the category
	for _, pathPattern := range category.Paths {
		if err := s.scanPath(ctx, pathPattern, &stats, progressCh); err != nil {
			progressCh <- ScanMsg{Error: err}
			return
		}
	}

	duration := time.Since(start)

	// Send completion message
	progressCh <- ScanMsg{
		Complete: &ScanComplete{
			Category: category.Name,
			Stats:    &stats,
			Duration: duration,
		},
	}
}

// ScanPath scans a specific path with pattern support
func (s *Scanner) scanPath(ctx context.Context, pathPattern string, stats *config.Category, progressCh chan<- ScanMsg) error {
	// Expand path patterns (supports wildcards like /home/*/.cache)
	paths, err := expandPathPattern(pathPattern)
	if err != nil {
		return err
	}

	for _, path := range paths {
		if err := s.walkDirectory(ctx, path, stats, progressCh); err != nil {
			if os.IsPermission(err) {
				// Skip permission errors silently for cleaner UX
				continue
			}
			return err
		}
	}

	return nil
}

// WalkDirectory performs the actual directory walking
func (s *Scanner) walkDirectory(ctx context.Context, rootPath string, stats *config.Category, progressCh chan<- ScanMsg) error {
	if info, err := s.fs.Stat(rootPath); err != nil {
		if os.IsNotExist(err) {
			// Silently skip non-existent paths (normal for wildcard patterns)
			return nil
		}
		// Only log actual errors (not permission issues or missing paths)
		log.Printf("ERROR: Failed to stat path %s: %v", rootPath, err)
		return err
	} else if !info.IsDir() {
		// Silently skip non-directories (normal for wildcard patterns)
		return nil
	}

	// Use our filesystem abstraction to walk directories
	return s.fs.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			// Silently skip permission errors and inaccessible paths (common during scanning)
			// Only log unexpected errors
			if !os.IsPermission(err) && !os.IsNotExist(err) {
				log.Printf("ERROR: Unexpected error accessing %s: %v", path, err)
			}
			return nil
		}

		// Skip if matches ignore patterns (FIXED: was inverted)
		if s.filter.MatchString(path) {
			if info.IsDir() {
				return filepath.SkipDir // Skip entire directory if it matches ignore pattern
			}
			return nil
		}

		// Skip directories for now (focus on files)
		if info.IsDir() {
			return nil
		}

		// Process file based on category filters
		if s.shouldIncludeFile(path, info, stats) {
			fileEntry := config.FileInfo{
				Path:    path,
				Size:    uint64(info.Size()),
				ModTime: info.ModTime().Format(time.RFC3339),
			}

			stats.Files = append(stats.Files, fileEntry)
			stats.Size += uint64(info.Size())
			stats.FileCount++

			// Send progress update periodically (every 100 files or every 10MB)
			shouldUpdate := stats.FileCount%100 == 0 || stats.Size%10485760 == 0 // 10MB chunks
			if shouldUpdate && stats.FileCount > 0 {
				progressCh <- ScanMsg{
					Progress: &ScanProgress{
						Path:         path,
						Bytes:        stats.Size,
						FilesScanned: stats.FileCount,
						CurrentDir:   filepath.Dir(path),
					},
				}
			}
		}

		return nil
	})
}

// ShouldIncludeFile determines if a file should be included based on filters
func (s *Scanner) shouldIncludeFile(path string, info os.FileInfo, category *config.Category) bool {
	// Skip directories for now (focus on files)
	if info.IsDir() {
		return false
	}

	// Check age-based filtering (if MinAgeDays is set)
	if category.MinAgeDays > 0 {
		fileAge := time.Since(info.ModTime())
		minAge := time.Duration(category.MinAgeDays) * 24 * time.Hour
		if fileAge < minAge {
			return false // File is too new, skip it
		}
	}

	// Apply category-specific filters (FIXED: OR logic, not AND)
	if len(category.Filters) > 0 {
		// File must match at least one filter to be included
		for _, filter := range category.Filters {
			matched, err := regexp.MatchString(filter, filepath.Base(path))
			if err != nil {
				continue // Skip invalid patterns
			}
			if matched {
				return true // File matches this filter, include it
			}
		}
		// No filters matched, exclude file
		return false
	}

	// No filters specified, include all files
	return true
}

// ExpandPathPattern expands path patterns with wildcards
func expandPathPattern(pattern string) ([]string, error) {
	// Simple expansion - can be enhanced for complex patterns
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	if len(matches) == 0 {
		// If no matches, return the original pattern (might be a specific path)
		return []string{pattern}, nil
	}

	return matches, nil
}

// GetDefaultPaths returns typical system cleanup paths
func GetDefaultPaths() []string {
	return []string{
		"/tmp",
		"/var/tmp",
		os.Getenv("HOME") + "/.cache",
		os.Getenv("HOME") + "/.thumbnails",
		"/var/cache/pacman/pkg",
	}
}
