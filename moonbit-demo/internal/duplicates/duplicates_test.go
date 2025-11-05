package duplicates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewScanner(t *testing.T) {
	opts := ScanOptions{
		Paths: []string{"/tmp"},
	}

	scanner := NewScanner(opts)
	if scanner == nil {
		t.Fatal("Expected scanner, got nil")
	}

	// Check defaults
	if scanner.opts.MinSize != 1024 {
		t.Errorf("Expected MinSize 1024, got %d", scanner.opts.MinSize)
	}

	if scanner.opts.MaxDepth != 10 {
		t.Errorf("Expected MaxDepth 10, got %d", scanner.opts.MaxDepth)
	}
}

func TestHashFile(t *testing.T) {
	// Create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := []byte("test content for hashing")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	hash1, err := hashFile(testFile)
	if err != nil {
		t.Fatalf("Failed to hash file: %v", err)
	}

	if hash1 == "" {
		t.Error("Expected non-empty hash")
	}

	// Hash same file again, should get same hash
	hash2, err := hashFile(testFile)
	if err != nil {
		t.Fatalf("Failed to hash file second time: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("Expected same hash, got %s and %s", hash1, hash2)
	}

	// Different content should produce different hash
	testFile2 := filepath.Join(tmpDir, "test2.txt")
	if err := os.WriteFile(testFile2, []byte("different content"), 0644); err != nil {
		t.Fatalf("Failed to create test file 2: %v", err)
	}

	hash3, err := hashFile(testFile2)
	if err != nil {
		t.Fatalf("Failed to hash file 2: %v", err)
	}

	if hash1 == hash3 {
		t.Error("Expected different hashes for different content")
	}
}

func TestScanNoDuplicates(t *testing.T) {
	tmpDir := t.TempDir()

	// Create unique files
	files := []string{"file1.txt", "file2.txt", "file3.txt"}
	for i, name := range files {
		path := filepath.Join(tmpDir, name)
		content := []byte("unique content " + string(rune(i)))
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	opts := ScanOptions{
		Paths:   []string{tmpDir},
		MinSize: 1,
	}

	scanner := NewScanner(opts)
	progressCh := make(chan ScanProgress, 10)

	go func() {
		for range progressCh {
			// Consume progress messages
		}
	}()

	result, err := scanner.Scan(progressCh)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(result.Groups) != 0 {
		t.Errorf("Expected 0 duplicate groups, got %d", len(result.Groups))
	}

	if result.FilesScanned != len(files) {
		t.Errorf("Expected %d files scanned, got %d", len(files), result.FilesScanned)
	}
}

func TestScanWithDuplicates(t *testing.T) {
	tmpDir := t.TempDir()

	// Create duplicate files
	content := []byte("duplicate content that will be hashed the same")

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	file3 := filepath.Join(tmpDir, "file3.txt")

	for _, path := range []string{file1, file2, file3} {
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Add a unique file
	uniqueFile := filepath.Join(tmpDir, "unique.txt")
	if err := os.WriteFile(uniqueFile, []byte("unique"), 0644); err != nil {
		t.Fatalf("Failed to create unique file: %v", err)
	}

	opts := ScanOptions{
		Paths:   []string{tmpDir},
		MinSize: 1,
	}

	scanner := NewScanner(opts)
	progressCh := make(chan ScanProgress, 10)

	go func() {
		for range progressCh {
			// Consume progress messages
		}
	}()

	result, err := scanner.Scan(progressCh)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should find 1 group with 3 duplicate files
	if len(result.Groups) != 1 {
		t.Errorf("Expected 1 duplicate group, got %d", len(result.Groups))
	}

	if result.TotalDupes != 2 {
		t.Errorf("Expected 2 duplicate files (keeping 1), got %d", result.TotalDupes)
	}

	if len(result.Groups) > 0 {
		group := result.Groups[0]
		if len(group.Files) != 3 {
			t.Errorf("Expected 3 files in group, got %d", len(group.Files))
		}

		expectedWasted := int64(len(content)) * 2 // 2 duplicates
		if result.WastedSpace != expectedWasted {
			t.Errorf("Expected %d bytes wasted, got %d", expectedWasted, result.WastedSpace)
		}
	}
}

func TestRemoveDuplicates(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	content := []byte("test content")
	for _, path := range []string{file1, file2} {
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Remove one duplicate
	removed, freedSpace, errors := RemoveDuplicates([]string{file2})

	if removed != 1 {
		t.Errorf("Expected 1 file removed, got %d", removed)
	}

	if freedSpace != int64(len(content)) {
		t.Errorf("Expected %d bytes freed, got %d", len(content), freedSpace)
	}

	if len(errors) > 0 {
		t.Errorf("Unexpected errors: %v", errors)
	}

	// Verify file was removed
	if _, err := os.Stat(file2); !os.IsNotExist(err) {
		t.Error("Expected file2 to be removed")
	}

	// Verify file1 still exists
	if _, err := os.Stat(file1); err != nil {
		t.Error("Expected file1 to still exist")
	}
}

func TestScanOptionsMinSize(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files of different sizes
	smallFile := filepath.Join(tmpDir, "small.txt")
	largeFile := filepath.Join(tmpDir, "large.txt")

	if err := os.WriteFile(smallFile, []byte("small"), 0644); err != nil {
		t.Fatalf("Failed to create small file: %v", err)
	}

	if err := os.WriteFile(largeFile, make([]byte, 2048), 0644); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	opts := ScanOptions{
		Paths:   []string{tmpDir},
		MinSize: 1024, // Only files >= 1KB
	}

	scanner := NewScanner(opts)
	progressCh := make(chan ScanProgress, 10)

	go func() {
		for range progressCh {
			// Consume progress messages
		}
	}()

	result, err := scanner.Scan(progressCh)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should only scan the large file
	if result.FilesScanned != 1 {
		t.Errorf("Expected 1 file scanned (>= 1KB), got %d", result.FilesScanned)
	}
}
