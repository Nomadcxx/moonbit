package duplicates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/moonbit/internal/paths"
)

func scannedFile(t *testing.T, path string) FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return FileInfo{
		Path: path, Size: info.Size(), Hash: hash, ModTime: info.ModTime().UnixNano(), FileID: paths.FileID(info),
	}
}

func TestScanHonorsMaxDepth(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"root.txt":               "root",
		"one/child.txt":          "child",
		"one/two/grandchild.txt": "grandchild",
	}
	for relative, content := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		maxDepth int
		want     int
	}{
		{maxDepth: 1, want: 1},
		{maxDepth: 2, want: 2},
	} {
		scanner := NewScanner(ScanOptions{Paths: []string{root}, MinSize: 1, MaxDepth: tc.maxDepth})
		progressCh := make(chan ScanProgress, 16)
		go func() {
			for range progressCh {
			}
		}()
		result, err := scanner.Scan(progressCh)
		if err != nil {
			t.Fatal(err)
		}
		if result.FilesScanned != tc.want {
			t.Fatalf("MaxDepth %d should scan %d files, scanned %d", tc.maxDepth, tc.want, result.FilesScanned)
		}
	}
}

func TestScanReportsIncompleteWhenRootCannotBeRead(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "included.txt")
	if err := os.WriteFile(file, []byte("readable"), 0644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing")
	scanner := NewScanner(ScanOptions{Paths: []string{root, missing}, MinSize: 1})
	progressCh := make(chan ScanProgress, 16)
	go func() {
		for range progressCh {
		}
	}()

	result, err := scanner.Scan(progressCh)
	if err != nil {
		t.Fatalf("expected partial scan result, got error: %v", err)
	}
	if result.FilesScanned != 1 {
		t.Fatalf("expected readable root to be scanned, got %d files", result.FilesScanned)
	}
	if !result.Incomplete {
		t.Fatal("scan with an unreadable root must be marked incomplete")
	}
	if len(result.ScanErrors) != 1 || !strings.Contains(result.ScanErrors[0], missing) {
		t.Fatalf("expected a diagnostic for %q, got %v", missing, result.ScanErrors)
	}
}

func TestScanReportsIncompleteWhenNestedDirectoryCannotBeRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can bypass directory permissions")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visible.txt"), []byte("visible"), 0644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "hidden.txt"), []byte("hidden"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0700)
	if err := os.Chmod(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner(ScanOptions{Paths: []string{root}, MinSize: 1})
	progressCh := make(chan ScanProgress, 16)
	go func() {
		for range progressCh {
		}
	}()

	result, err := scanner.Scan(progressCh)
	if err != nil {
		t.Fatalf("expected partial scan result, got error: %v", err)
	}
	if !result.Incomplete || len(result.ScanErrors) == 0 {
		t.Fatalf("nested permission error must mark scan incomplete, got %+v", result)
	}
	if result.FilesScanned != 1 {
		t.Fatalf("expected only the readable file, scanned %d", result.FilesScanned)
	}
}

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
	removed, freedSpace, errors := RemoveDuplicates([]FileInfo{scannedFile(t, file2)})

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

func TestRemoveDuplicatesRejectsParentSymlinkSwap(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	original := filepath.Join(base, "allowed.original")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(allowed, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	scannedPath := filepath.Join(allowed, "target.bin")
	if err := os.WriteFile(scannedPath, []byte("scanned duplicate"), 0600); err != nil {
		t.Fatal(err)
	}
	expected := scannedFile(t, scannedPath)
	victimPath := filepath.Join(outside, "target.bin")
	victim := []byte("outside victim")
	if err := os.WriteFile(victimPath, victim, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(allowed, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, allowed); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	removed, _, errs := RemoveDuplicates([]FileInfo{expected})
	if removed != 0 || len(errs) != 1 {
		t.Fatalf("changed path should be rejected, removed=%d errors=%v", removed, errs)
	}
	got, err := os.ReadFile(victimPath)
	if err != nil {
		t.Fatalf("outside victim was removed: %v", err)
	}
	if string(got) != string(victim) {
		t.Fatalf("outside victim changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(original, "target.bin")); err != nil {
		t.Fatalf("scanned file was removed: %v", err)
	}
}

func TestRemoveDuplicatesRejectsInPlaceContentChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate.bin")
	original := []byte("original content")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	expected := scannedFile(t, path)
	statBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("modified content")
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, statBefore.ModTime(), statBefore.ModTime()); err != nil {
		t.Fatal(err)
	}
	statAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if paths.FileID(statBefore) != paths.FileID(statAfter) || statBefore.Size() != statAfter.Size() {
		t.Fatal("test mutation must keep the file identity and size unchanged")
	}

	removed, _, errs := RemoveDuplicates([]FileInfo{expected})
	if removed != 0 || len(errs) != 1 {
		t.Fatalf("changed contents should be rejected, removed=%d errors=%v", removed, errs)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("changed file was removed: %v", err)
	}
	if string(got) != string(changed) {
		t.Fatalf("changed file was modified: %q", got)
	}
}

func TestRemoveIfRejectsReplacementSinceDuplicateScan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate.bin")
	if err := os.WriteFile(path, []byte("scanned content"), 0600); err != nil {
		t.Fatal(err)
	}
	expected := scannedFile(t, path)
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("scanned content"), 0600); err != nil {
		t.Fatal(err)
	}

	err := paths.RemoveIf(path, func(info os.FileInfo) error {
		return matchesScannedIdentity(info, expected)
	})
	if err == nil {
		t.Fatal("replacement file passed the scan identity check")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("replacement file was removed: %v", err)
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
