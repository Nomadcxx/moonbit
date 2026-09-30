package duplicates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Nomadcxx/moonbit/internal/paths"
	"github.com/Nomadcxx/moonbit/internal/validation"
)

// FileInfo represents a file with metadata
type FileInfo struct {
	Path    string
	Size    int64
	Hash    string
	ModTime int64
	FileID  string
}

// DuplicateGroup represents a group of duplicate files
type DuplicateGroup struct {
	Hash      string
	Size      int64
	Files     []FileInfo
	TotalSize int64 // Size * (count - 1), space that can be freed
}

// ScanOptions controls duplicate scanning behavior
type ScanOptions struct {
	Paths          []string
	MinSize        int64 // Minimum file size to consider (default: 1KB)
	MaxSize        int64 // Maximum file size to consider (0 = unlimited)
	IgnorePatterns []string
	MaxDepth       int // Relative path depth: root is 0; directories at the limit are not traversed.
}

// ScanProgress reports scanning progress
type ScanProgress struct {
	FilesScanned int
	BytesScanned int64
	CurrentFile  string
	Phase        string
}

// ScanResult contains duplicate detection results
type ScanResult struct {
	Groups             []DuplicateGroup
	TotalDupes         int
	WastedSpace        int64
	FilesScanned       int
	DirectoriesScanned int
	Incomplete         bool
	ScanErrors         []string
}

// Constants for duplicate scanning
const (
	// DefaultHashWorkers is the default number of worker goroutines for parallel file hashing
	// This balances CPU utilization and memory usage for duplicate detection
	DefaultHashWorkers = 4

	// DefaultMinSize is the default minimum file size (1KB) for duplicate detection
	// Files smaller than this are ignored to avoid noise from tiny config files
	DefaultMinSize = 1024 // 1KB

	// DefaultMaxDepth is the default maximum directory traversal depth
	// Prevents infinite recursion and limits scan scope
	DefaultMaxDepth = 10

	// ProgressUpdateInterval is the number of files to scan before sending a progress update
	// Balances update frequency with channel overhead
	ProgressUpdateInterval = 100
)

// Scanner finds duplicate files
type Scanner struct {
	opts ScanOptions
}

// NewScanner creates a new duplicate file scanner
func NewScanner(opts ScanOptions) *Scanner {
	if opts.MinSize == 0 {
		opts.MinSize = DefaultMinSize
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = DefaultMaxDepth
	}
	return &Scanner{opts: opts}
}

// Scan finds duplicate files in the specified paths
func (s *Scanner) Scan(progressCh chan<- ScanProgress) (*ScanResult, error) {
	defer close(progressCh)

	// Phase 1: Collect all files and group by size
	progressCh <- ScanProgress{Phase: "Collecting files..."}

	sizeMap := make(map[int64][]FileInfo)
	filesScanned := 0
	bytesScanned := int64(0)
	dirsScanned := 0
	var scanErrors []string

	for _, rootPath := range s.opts.Paths {
		filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				scanErrors = append(scanErrors, fmt.Sprintf("%s: %v", path, err))
				return nil // Skip errors, continue scanning
			}

			if info.IsDir() {
				dirsScanned++
				if path != rootPath && pathDepth(rootPath, path) >= s.opts.MaxDepth {
					return filepath.SkipDir
				}
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}

			// Apply size filters
			if info.Size() < s.opts.MinSize {
				return nil
			}
			if s.opts.MaxSize > 0 && info.Size() > s.opts.MaxSize {
				return nil
			}

			// Check ignore patterns
			for _, pattern := range s.opts.IgnorePatterns {
				matched, _ := filepath.Match(pattern, filepath.Base(path))
				if matched {
					return nil
				}
			}

			fileInfo := FileInfo{
				Path:    path,
				Size:    info.Size(),
				ModTime: info.ModTime().UnixNano(),
				FileID:  paths.FileID(info),
			}

			sizeMap[info.Size()] = append(sizeMap[info.Size()], fileInfo)
			filesScanned++
			bytesScanned += info.Size()

			if filesScanned%ProgressUpdateInterval == 0 {
				progressCh <- ScanProgress{
					FilesScanned: filesScanned,
					BytesScanned: bytesScanned,
					CurrentFile:  path,
					Phase:        "Collecting files...",
				}
			}

			return nil
		})
	}

	// Phase 2: Hash files with duplicate sizes
	progressCh <- ScanProgress{
		FilesScanned: filesScanned,
		Phase:        "Computing hashes...",
	}

	hashMap := make(map[string][]FileInfo)
	hashCount := 0

	// Use goroutines for parallel hashing
	type hashJob struct {
		files []FileInfo
		size  int64
	}
	type hashResult struct {
		groups map[string][]FileInfo
		errors []string
	}

	jobs := make(chan hashJob, 100)
	results := make(chan hashResult, 100)
	var wg sync.WaitGroup

	// Start worker goroutines for parallel hashing
	numWorkers := DefaultHashWorkers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localResult := hashResult{groups: make(map[string][]FileInfo)}

			for job := range jobs {
				if len(job.files) < 2 {
					continue // No duplicates possible
				}

				for _, file := range job.files {
					hash, err := hashFile(file.Path)
					if err != nil {
						localResult.errors = append(localResult.errors, fmt.Sprintf("%s: %v", file.Path, err))
						continue // Skip files we can't hash
					}

					file.Hash = hash
					localResult.groups[hash] = append(localResult.groups[hash], file)
				}
			}

			results <- localResult
		}()
	}

	// Send jobs
	go func() {
		for size, files := range sizeMap {
			if len(files) < 2 {
				continue // No duplicates possible
			}
			jobs <- hashJob{files: files, size: size}
		}
		close(jobs)
	}()

	// Wait for all workers and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Merge results
	for localResult := range results {
		for hash, files := range localResult.groups {
			hashMap[hash] = append(hashMap[hash], files...)
		}
		scanErrors = append(scanErrors, localResult.errors...)
		hashCount++
		progressCh <- ScanProgress{
			FilesScanned: filesScanned,
			Phase:        fmt.Sprintf("Computing hashes... (%d groups)", hashCount),
		}
	}

	// Phase 3: Build duplicate groups
	progressCh <- ScanProgress{Phase: "Building results..."}

	var groups []DuplicateGroup
	totalDupes := 0
	wastedSpace := int64(0)

	for hash, files := range hashMap {
		if len(files) < 2 {
			continue // Not a duplicate
		}

		// Sort files by modification time (oldest first)
		sort.Slice(files, func(i, j int) bool {
			return files[i].ModTime < files[j].ModTime
		})

		group := DuplicateGroup{
			Hash:      hash,
			Size:      files[0].Size,
			Files:     files,
			TotalSize: files[0].Size * int64(len(files)-1),
		}

		groups = append(groups, group)
		totalDupes += len(files) - 1
		wastedSpace += group.TotalSize
	}

	// Sort groups by wasted space (largest first)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].TotalSize > groups[j].TotalSize
	})
	sort.Strings(scanErrors)

	return &ScanResult{
		Groups:             groups,
		TotalDupes:         totalDupes,
		WastedSpace:        wastedSpace,
		FilesScanned:       filesScanned,
		DirectoriesScanned: dirsScanned,
		Incomplete:         len(scanErrors) > 0,
		ScanErrors:         scanErrors,
	}, nil
}

func pathDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1
}

// hashFile computes SHA256 hash of a file
func hashFile(path string) (string, error) {
	file, err := paths.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return hashOpenFile(file)
}

func hashOpenFile(file io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// RemoveDuplicates removes selected duplicate files that still match their scan records.
func RemoveDuplicates(filesToRemove []FileInfo) (int, int64, []string) {
	removed := 0
	freedSpace := int64(0)
	var removeErrors []string

	for _, expected := range filesToRemove {
		if err := validation.ValidateFilePath(expected.Path); err != nil {
			removeErrors = append(removeErrors, fmt.Sprintf("%s: %v", expected.Path, err))
			continue
		}
		if expected.FileID == "" || expected.Hash == "" {
			removeErrors = append(removeErrors, fmt.Sprintf("%s: missing scan identity", expected.Path))
			continue
		}

		file, err := paths.OpenFile(expected.Path, os.O_RDONLY, 0)
		if err != nil {
			removeErrors = append(removeErrors, fmt.Sprintf("%s: %v", expected.Path, err))
			continue
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			removeErrors = append(removeErrors, fmt.Sprintf("%s: failed to stat file: %v", expected.Path, err))
			continue
		}
		if !info.Mode().IsRegular() || paths.FileID(info) != expected.FileID ||
			info.Size() != expected.Size || info.ModTime().UnixNano() != expected.ModTime {
			file.Close()
			removeErrors = append(removeErrors, fmt.Sprintf("%s: file changed since duplicate scan", expected.Path))
			continue
		}
		hash, err := hashOpenFile(file)
		if err != nil {
			file.Close()
			removeErrors = append(removeErrors, fmt.Sprintf("%s: failed to hash file: %v", expected.Path, err))
			continue
		}
		if hash != expected.Hash {
			file.Close()
			removeErrors = append(removeErrors, fmt.Sprintf("%s: file contents changed since duplicate scan", expected.Path))
			continue
		}
		if err := file.Close(); err != nil {
			removeErrors = append(removeErrors, fmt.Sprintf("%s: failed to close file: %v", expected.Path, err))
			continue
		}
		size := info.Size()

		if err := paths.Remove(expected.Path); err != nil {
			removeErrors = append(removeErrors, fmt.Sprintf("%s: %v", expected.Path, err))
			continue
		}

		removed++
		freedSpace += size
	}

	return removed, freedSpace, removeErrors
}
