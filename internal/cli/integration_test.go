package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/cleaner"
	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	"github.com/Nomadcxx/moonbit/internal/session"
	"github.com/Nomadcxx/moonbit/internal/validation"
)

// isolate points every moonbit path (config, cache, data) at a temp dir so the
// test never touches the developer's real state.
func isolate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for k, v := range map[string]string{
		"MOONBIT_HOME":    root,
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
	} {
		t.Setenv(k, v)
	}
	return root
}

// scanCategory runs the real scanner over a category.
func scanCategory(t *testing.T, cat *config.Category) *config.Category {
	t.Helper()
	ch := make(chan scanner.ScanMsg, 128)
	go scanner.NewScanner(&config.Config{}).ScanCategory(context.Background(), cat, ch)
	var stats *config.Category
	for msg := range ch {
		if msg.Complete != nil {
			stats = msg.Complete.Stats
		}
		if msg.Error != nil {
			t.Fatalf("scan error: %v", msg.Error)
		}
	}
	if stats == nil {
		t.Fatal("no scan results")
	}
	return stats
}

func cleanVerified(t *testing.T, cat *config.Category) (int, uint64, []string) {
	t.Helper()
	ch := make(chan cleaner.CleanMsg, 128)
	c := cleaner.NewCleaner(&config.Config{})
	defer c.Close()
	go c.CleanCategory(context.Background(), cat, false, ch)
	var deleted int
	var freed uint64
	var errs []string
	for msg := range ch {
		if msg.Complete != nil {
			deleted, freed, errs = msg.Complete.FilesDeleted, msg.Complete.BytesFreed, msg.Complete.Errors
		}
	}
	return deleted, freed, errs
}

// End-to-end: scan a real directory, persist the cache, revalidate it, clean.
// The bytes reported as freed must equal the bytes that actually left the disk.
func TestEndToEndScanCacheRevalidateClean(t *testing.T) {
	root := isolate(t)
	cacheDir := filepath.Join(root, "appcache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}

	sizes := []int{1024, 2048, 4096}
	var expected uint64
	for i, n := range sizes {
		p := filepath.Join(cacheDir, string(rune('a'+i))+".tmp")
		if err := os.WriteFile(p, make([]byte, n), 0644); err != nil {
			t.Fatal(err)
		}
		expected += uint64(n)
	}
	// A file that must survive: wrong extension.
	keep := filepath.Join(cacheDir, "keep.conf")
	os.WriteFile(keep, []byte("config"), 0644)

	category := config.Category{
		Name:     "Test Cache",
		Paths:    []string{cacheDir},
		Filters:  []string{`\.tmp$`},
		Risk:     config.Low,
		Selected: true,
	}

	stats := scanCategory(t, &category)
	if stats.Size != expected {
		t.Fatalf("scan reported %d bytes, expected %d", stats.Size, expected)
	}

	// Persist and reload through the real session cache, as the CLI does.
	mgr, err := session.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Save(&config.SessionCache{
		ScanResults: stats,
		TotalSize:   stats.Size,
		TotalFiles:  stats.FileCount,
		ScannedAt:   time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}

	verified, report, err := validation.RevalidateCache(
		loaded, []config.Category{category}, validation.CacheOptions{})
	if err != nil {
		t.Fatalf("legitimate cache must revalidate: %v", err)
	}
	if report.TotalDropped() != 0 {
		t.Errorf("nothing should have been dropped, got %v", report.Dropped)
	}
	if verified.TotalFiles != len(sizes) {
		t.Fatalf("expected %d files, got %d", len(sizes), verified.TotalFiles)
	}

	deleted, freed, errs := cleanVerified(t, verified.ScanResults)
	if len(errs) != 0 {
		t.Fatalf("unexpected clean errors: %v", errs)
	}
	if deleted != len(sizes) {
		t.Errorf("expected %d deleted, got %d", len(sizes), deleted)
	}
	if freed != expected {
		t.Errorf("reported %d bytes freed, expected %d", freed, expected)
	}

	// Ground truth: the .tmp files are gone, keep.conf survives.
	entries, _ := os.ReadDir(cacheDir)
	if len(entries) != 1 || entries[0].Name() != "keep.conf" {
		t.Errorf("expected only keep.conf to remain, got %v", entries)
	}
}

// SEC-0 end-to-end: a tampered cache on disk must not delete anything outside
// the configured categories, even though the file is fully user-writable.
func TestEndToEndTamperedCacheIsRejected(t *testing.T) {
	root := isolate(t)
	cacheDir := filepath.Join(root, "appcache")
	os.MkdirAll(cacheDir, 0755)

	// The files an attacker would name.
	systemFile := filepath.Join(root, "pretend-usr-lib", "libc.so.6")
	os.MkdirAll(filepath.Dir(systemFile), 0755)
	os.WriteFile(systemFile, []byte("critical"), 0644)

	otherUserDoc := filepath.Join(root, "pretend-home", "thesis.odt")
	os.MkdirAll(filepath.Dir(otherUserDoc), 0755)
	os.WriteFile(otherUserDoc, []byte("thesis"), 0644)

	legit := filepath.Join(cacheDir, "junk.tmp")
	os.WriteFile(legit, make([]byte, 512), 0644)

	legitInfo, _ := os.Lstat(legit)
	sysInfo, _ := os.Lstat(systemFile)
	docInfo, _ := os.Lstat(otherUserDoc)

	// Hand-written cache: claims Low risk and shredding, names arbitrary paths.
	tampered := &config.SessionCache{
		ScanResults: &config.Category{
			Name:         "Attacker Controlled",
			Risk:         config.Low,
			ShredEnabled: true,
			Files: []config.FileInfo{
				{Path: legit, Size: uint64(legitInfo.Size()),
					ModTime: legitInfo.ModTime().Format(time.RFC3339), CategoryName: "Test Cache"},
				{Path: systemFile, Size: uint64(sysInfo.Size()),
					ModTime: sysInfo.ModTime().Format(time.RFC3339), CategoryName: "Test Cache"},
				{Path: otherUserDoc, Size: uint64(docInfo.Size()),
					ModTime: docInfo.ModTime().Format(time.RFC3339), CategoryName: "Test Cache"},
			},
		},
		TotalFiles: 3,
		ScannedAt:  time.Now(),
	}

	mgr, err := session.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Save(tampered); err != nil {
		t.Fatal(err)
	}
	loaded, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}

	category := config.Category{
		Name:    "Test Cache",
		Paths:   []string{cacheDir},
		Filters: []string{`\.tmp$`},
		Risk:    config.Low,
	}

	verified, report, err := validation.RevalidateCache(
		loaded, []config.Category{category}, validation.CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if verified.TotalFiles != 1 {
		t.Fatalf("only the in-category file may survive, got %d: %+v",
			verified.TotalFiles, verified.ScanResults.Files)
	}
	if verified.ScanResults.ShredEnabled {
		t.Error("cache must not be able to enable shredding")
	}
	if verified.ScanResults.Files[0].CategoryShred {
		t.Error("per-file shred must come from config")
	}
	if report.Dropped[validation.DropOutsideCategory] != 2 {
		t.Errorf("expected 2 out-of-category drops, got %v", report.Dropped)
	}

	cleanVerified(t, verified.ScanResults)

	// The critical files must still be present and unmodified.
	for _, p := range []string{systemFile, otherUserDoc} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s was deleted", p)
			continue
		}
		if len(b) == 0 {
			t.Errorf("%s was truncated or shredded", p)
		}
	}
	if _, err := os.Stat(legit); !os.IsNotExist(err) {
		t.Error("the legitimate in-category file should have been cleaned")
	}
}

// A cache that predates the fix (no per-file provenance) must be refused with a
// clear "re-run scan" error rather than trusted or silently emptied.
func TestEndToEndLegacyCacheRefused(t *testing.T) {
	root := isolate(t)
	p := filepath.Join(root, "old.tmp")
	os.WriteFile(p, []byte("x"), 0644)

	mgr, err := session.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	mgr.Save(&config.SessionCache{
		ScanResults: &config.Category{
			Name:  "Total Cleanable",
			Files: []config.FileInfo{{Path: p, Size: 1}}, // no CategoryName
		},
		TotalFiles: 1,
		ScannedAt:  time.Now(),
	})
	loaded, _ := mgr.Load()

	_, _, err = validation.RevalidateCache(loaded, []config.Category{{
		Name: "Test", Paths: []string{root},
	}}, validation.CacheOptions{})
	if err == nil {
		t.Fatal("a cache without provenance must be refused")
	}
	if _, statErr := os.Stat(p); statErr != nil {
		t.Error("nothing should have been deleted")
	}
}

// revalidateSessionCache is the CLI's wrapper; make sure it wires the
// authoritative category set through and surfaces errors.
func TestRevalidateSessionCacheWrapper(t *testing.T) {
	root := isolate(t)
	cacheDir := filepath.Join(root, "appcache")
	os.MkdirAll(cacheDir, 0755)
	p := filepath.Join(cacheDir, "a.tmp")
	os.WriteFile(p, make([]byte, 64), 0644)

	info, _ := os.Lstat(p)
	cache := &config.SessionCache{
		ScanResults: &config.Category{
			Name: "Total Cleanable",
			Files: []config.FileInfo{{
				Path: p, Size: 64,
				ModTime:      info.ModTime().Format(time.RFC3339),
				CategoryName: "Test Cache",
			}},
		},
		TotalFiles: 1,
		ScannedAt:  time.Now(),
	}

	cfg := &config.Config{Categories: []config.Category{{
		Name:    "Test Cache",
		Paths:   []string{cacheDir},
		Filters: []string{`\.tmp$`},
		Risk:    config.Low,
	}}}

	out, err := revalidateSessionCache(cache, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TotalFiles != 1 {
		t.Errorf("expected the file to survive, got %d", out.TotalFiles)
	}

	// A stale cache must produce an error, not a silent pass.
	cache.ScannedAt = time.Now().Add(-72 * time.Hour)
	if _, err := revalidateSessionCache(cache, cfg); err == nil {
		t.Error("stale cache must be refused")
	}
}

// Full CLI pipeline: the exact functions `moonbit scan` and `moonbit clean`
// call, driven against a sandboxed home. Verifies that a symlink planted in a
// scanned cache directory is neither counted nor followed, and that the bytes
// reported as freed match what actually left the disk.
func TestCLIScanThenCleanEndToEnd(t *testing.T) {
	root := isolate(t)

	// Populate a realistic user cache.
	yay := filepath.Join(root, ".cache", "yay")
	if err := os.MkdirAll(yay, 0755); err != nil {
		t.Fatal(err)
	}
	var expected uint64
	for name, size := range map[string]int{"pkg1.tar.gz": 100 * 1024, "pkg2.tar.gz": 200 * 1024} {
		if err := os.WriteFile(filepath.Join(yay, name), make([]byte, size), 0644); err != nil {
			t.Fatal(err)
		}
		expected += uint64(size)
	}

	// A symlink inside the scanned directory pointing at something precious.
	precious := filepath.Join(root, "precious.txt")
	if err := os.WriteFile(precious, []byte("PRECIOUS DATA"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(precious, filepath.Join(yay, "evil.tar.gz")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// Reset the command-level globals this pipeline reads.
	prevMode, prevInc, prevExc := scanMode, includeCategories, excludeCategories
	t.Cleanup(func() { scanMode, includeCategories, excludeCategories = prevMode, prevInc, prevExc })
	scanMode, includeCategories, excludeCategories = "", nil, nil

	if err := ScanAndSave(); err != nil {
		t.Fatalf("ScanAndSave failed: %v", err)
	}

	// Inspect what the scan recorded before cleaning.
	mgr, err := session.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	cached, err := mgr.Load()
	if err != nil {
		t.Fatalf("scan did not write a usable cache: %v", err)
	}
	// The symlink must not have inflated the recorded total.
	if cached.TotalSize != expected {
		t.Errorf("scan recorded %d bytes, expected exactly %d (symlink must not be counted)",
			cached.TotalSize, expected)
	}
	if cached.TotalFiles != 2 {
		t.Errorf("scan recorded %d files, expected 2", cached.TotalFiles)
	}
	for _, f := range cached.ScanResults.Files {
		if filepath.Base(f.Path) == "evil.tar.gz" {
			t.Error("the symlink was collected by the scan")
		}
		if f.CategoryName == "" {
			t.Errorf("scan recorded %s without provenance", f.Path)
		}
	}

	if err := CleanSession(false); err != nil {
		t.Fatalf("CleanSession failed: %v", err)
	}

	// Ground truth.
	if _, err := os.Stat(filepath.Join(yay, "pkg1.tar.gz")); !os.IsNotExist(err) {
		t.Error("pkg1.tar.gz should have been cleaned")
	}
	if _, err := os.Stat(filepath.Join(yay, "pkg2.tar.gz")); !os.IsNotExist(err) {
		t.Error("pkg2.tar.gz should have been cleaned")
	}
	b, err := os.ReadFile(precious)
	if err != nil {
		t.Fatalf("the symlink target was deleted: %v", err)
	}
	if string(b) != "PRECIOUS DATA" {
		t.Error("the symlink target was modified")
	}
	if _, err := os.Lstat(filepath.Join(yay, "evil.tar.gz")); err != nil {
		t.Error("the symlink itself should have been left alone, not unlinked")
	}
}
