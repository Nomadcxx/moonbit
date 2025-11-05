package duplicates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// FileInfo represents a file with metadata
type FileInfo struct {
	Path    string
	Size    int64
	Hash    string
	ModTime int64
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
	MaxDepth       int
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
}

// Scanner finds duplicate files
type Scanner struct {
	opts ScanOptions
}

// NewScanner creates a new duplicate file scanner
func NewScanner(opts ScanOptions) *Scanner {
	if opts.MinSize == 0 {
		opts.MinSize = 1024 // 1KB default minimum
	}
	if opts.MaxDepth == 0 {
		opts.MaxDepth = 10
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

	for _, rootPath := range s.opts.Paths {
		err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors, continue scanning
			}

			if info.IsDir() {
				dirsScanned++
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
				ModTime: info.ModTime().Unix(),
			}

			sizeMap[info.Size()] = append(sizeMap[info.Size()], fileInfo)
			filesScanned++
			bytesScanned += info.Size()

			if filesScanned%100 == 0 {
				progressCh <- ScanProgress{
					FilesScanned: filesScanned,
					BytesScanned: bytesScanned,
					CurrentFile:  path,
					Phase:        "Collecting files...",
				}
			}

			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("walk error: %w", err)
		}
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

	jobs := make(chan hashJob, 100)
	results := make(chan map[string][]FileInfo, 100)
	var wg sync.WaitGroup

	// Start worker goroutines
	numWorkers := 4
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localHashMap := make(map[string][]FileInfo)

			for job := range jobs {
				if len(job.files) < 2 {
					continue // No duplicates possible
				}

				for _, file := range job.files {
					hash, err := hashFile(file.Path)
					if err != nil {
						continue // Skip files we can't hash
					}

					file.Hash = hash
					localHashMap[hash] = append(localHashMap[hash], file)
				}
			}

			results <- localHashMap
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
	for localMap := range results {
		for hash, files := range localMap {
			hashMap[hash] = append(hashMap[hash], files...)
		}
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

	return &ScanResult{
		Groups:             groups,
		TotalDupes:         totalDupes,
		WastedSpace:        wastedSpace,
		FilesScanned:       filesScanned,
		DirectoriesScanned: dirsScanned,
	}, nil
}

// hashFile computes SHA256 hash of a file
func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// RemoveDuplicates removes selected duplicate files
func RemoveDuplicates(filesToRemove []string) (int, int64, []string) {
	removed := 0
	freedSpace := int64(0)
	var errors []string

	for _, path := range filesToRemove {
		info, err := os.Stat(path)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", path, err))
			continue
		}

		size := info.Size()

		if err := os.Remove(path); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", path, err))
			continue
		}

		removed++
		freedSpace += size
	}

	return removed, freedSpace, errors
}
