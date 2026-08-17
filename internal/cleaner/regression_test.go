package cleaner

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/moonbit/internal/config"
)

func runClean(t *testing.T, cat *config.Category) (deleted int, freed uint64, errs []string) {
	t.Helper()
	ch := make(chan CleanMsg, 64)
	c := NewCleaner(&config.Config{})
	go c.CleanCategory(context.Background(), cat, false, ch)
	for msg := range ch {
		if msg.Complete != nil {
			deleted, freed, errs = msg.Complete.FilesDeleted, msg.Complete.BytesFreed, msg.Complete.Errors
		}
	}
	return
}

func sha(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// SEC-1: shredding must never reach through a symlink and destroy the target.
//
// Before the fix, isProtectedPath judged the link by its own path and shredFile
// opened with plain O_WRONLY, so the target was overwritten with random data
// while only the link was unlinked.
func TestShredDoesNotFollowSymlinks(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	victimDir := filepath.Join(tmp, "victim")
	os.MkdirAll(cacheDir, 0755)
	os.MkdirAll(victimDir, 0755)

	victim := filepath.Join(victimDir, "important.db")
	content := make([]byte, 4096)
	for i := range content {
		content[i] = 'A'
	}
	if err := os.WriteFile(victim, content, 0644); err != nil {
		t.Fatal(err)
	}
	before := sha(t, victim)

	link := filepath.Join(cacheDir, "evil.tmp")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// Worst case: the cache claims the target's size and turns shredding on.
	st, _ := os.Stat(link)
	deleted, freed, _ := runClean(t, &config.Category{
		Name:         "Test",
		Risk:         config.Low,
		ShredEnabled: true,
		Size:         uint64(st.Size()),
		Files:        []config.FileInfo{{Path: link, Size: uint64(st.Size())}},
	})

	if sha(t, victim) != before {
		t.Fatal("SEC-1 regression: symlink target was overwritten")
	}
	if deleted != 0 {
		t.Errorf("a symlink must not be counted as deleted, got %d", deleted)
	}
	if freed != 0 {
		t.Errorf("a symlink must not contribute to bytes freed, got %d", freed)
	}
}

// SEC-1 at the syscall layer: shredFile itself must refuse a symlink.
func TestShredFileRefusesSymlink(t *testing.T) {
	tmp := t.TempDir()
	victim := filepath.Join(tmp, "victim")
	if err := os.WriteFile(victim, []byte("precious"), 0644); err != nil {
		t.Fatal(err)
	}
	before := sha(t, victim)

	link := filepath.Join(tmp, "link")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := NewCleaner(&config.Config{}).shredFile(link, 8); err == nil {
		t.Error("shredFile must refuse to open a symlink (O_NOFOLLOW)")
	}
	if sha(t, victim) != before {
		t.Error("shredFile overwrote the symlink target")
	}
}

// isProtectedPath must judge the target, not just the link path.
func TestIsProtectedPathResolvesSymlinks(t *testing.T) {
	tmp := t.TempDir()
	link := filepath.Join(tmp, "innocent.tmp")
	if err := os.Symlink("/etc/passwd", link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	c := NewCleaner(&config.Config{})
	if !c.isProtectedPath(link) {
		t.Error("a symlink pointing into a protected directory must be treated as protected")
	}
	// Sanity: an ordinary path in the same directory is not protected.
	if c.isProtectedPath(filepath.Join(tmp, "ordinary.tmp")) {
		t.Error("ordinary temp path should not be protected")
	}
}

// SEC-2 / reporting accuracy: bytes freed must be measured at delete time, not
// taken from the scan record.
func TestReportedBytesReflectDiskNotCache(t *testing.T) {
	tmp := t.TempDir()

	shrunk := filepath.Join(tmp, "shrunk.log")
	os.WriteFile(shrunk, make([]byte, 10*1024*1024), 0644)
	gone := filepath.Join(tmp, "gone.log")
	os.WriteFile(gone, make([]byte, 5*1024*1024), 0644)

	cat := &config.Category{
		Name: "Test", Risk: config.Low,
		Size: 15 * 1024 * 1024,
		Files: []config.FileInfo{
			{Path: shrunk, Size: 10 * 1024 * 1024},
			{Path: gone, Size: 5 * 1024 * 1024},
		},
	}

	// Between scan and clean: one file truncated by its writer, one already gone.
	os.WriteFile(shrunk, make([]byte, 1024), 0644)
	os.Remove(gone)

	deleted, freed, errs := runClean(t, cat)

	if freed != 1024 {
		t.Errorf("expected 1024 bytes freed (actual size on disk), got %d", freed)
	}
	if deleted != 1 {
		t.Errorf("expected 1 file deleted, got %d", deleted)
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error for the already-removed file, got %d", len(errs))
	}
}

// Non-regular files are never deletable, however they reach the cleaner.
func TestDeleteFileRefusesNonRegularFiles(t *testing.T) {
	tmp := t.TempDir()
	c := NewCleaner(&config.Config{})

	dir := filepath.Join(tmp, "adir")
	os.MkdirAll(dir, 0755)
	if _, err := c.deleteFile(dir, false); err == nil {
		t.Error("deleteFile must refuse a directory")
	}

	link := filepath.Join(tmp, "alink")
	target := filepath.Join(tmp, "target")
	os.WriteFile(target, []byte("x"), 0644)
	if err := os.Symlink(target, link); err == nil {
		if _, err := c.deleteFile(link, false); err == nil {
			t.Error("deleteFile must refuse a symlink")
		}
		if _, err := os.Stat(target); err != nil {
			t.Error("symlink target must survive")
		}
	}
}

// OPS-1: held-open logs are truncated, not unlinked. The file must survive at
// zero length and the reclaimed bytes must be reported.
func TestTruncateActionReclaimsWithoutUnlinking(t *testing.T) {
	tmp := t.TempDir()
	logFile := filepath.Join(tmp, "container.log")
	if err := os.WriteFile(logFile, make([]byte, 8192), 0644); err != nil {
		t.Fatal(err)
	}

	deleted, freed, errs := runClean(t, &config.Category{
		Name:   "Docker Container Logs",
		Risk:   config.Medium,
		Action: config.ActionTruncate,
		Size:   8192,
		Files:  []config.FileInfo{{Path: logFile, Size: 8192}},
	})

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if deleted != 1 {
		t.Errorf("expected 1 file processed, got %d", deleted)
	}
	if freed != 8192 {
		t.Errorf("expected 8192 bytes reclaimed, got %d", freed)
	}

	st, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("truncate must not unlink the file: %v", err)
	}
	if st.Size() != 0 {
		t.Errorf("expected the file to be truncated to 0, got %d", st.Size())
	}
}

// Per-file action from the revalidation gate must be honoured even when the
// aggregate category carries no action.
func TestPerFileTruncateActionIsHonoured(t *testing.T) {
	tmp := t.TempDir()
	logFile := filepath.Join(tmp, "x.log")
	os.WriteFile(logFile, make([]byte, 512), 0644)

	_, freed, errs := runClean(t, &config.Category{
		Name:  "Aggregate",
		Risk:  config.Low,
		Files: []config.FileInfo{{Path: logFile, Size: 512, CategoryAction: config.ActionTruncate}},
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if freed != 512 {
		t.Errorf("expected 512 bytes reclaimed, got %d", freed)
	}
	if _, err := os.Stat(logFile); err != nil {
		t.Error("per-file truncate must not unlink the file")
	}
}

func TestTruncateFileRefusesSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	os.WriteFile(target, make([]byte, 256), 0644)
	link := filepath.Join(tmp, "link.log")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if _, err := NewCleaner(&config.Config{}).truncateFile(link); err == nil {
		t.Error("truncateFile must refuse a symlink (O_NOFOLLOW)")
	}
	st, _ := os.Stat(target)
	if st.Size() != 256 {
		t.Error("truncateFile truncated through a symlink")
	}
}
