package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/moonbit/internal/config"
)

func scanOne(t *testing.T, cat *config.Category) *config.Category {
	t.Helper()
	ch := make(chan ScanMsg, 64)
	s := NewScanner(&config.Config{})
	go s.ScanCategory(context.Background(), cat, ch)
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
		t.Fatal("scan produced no stats")
	}
	return stats
}

// SEC-1 / SEC-2 at the source: the scanner must not collect symlinks, and must
// not account them at their target's size.
//
// Before the fix the walk callback used os.Stat, so a symlink was indistinguishable
// from a regular file and carried the target's size into every reported total.
func TestScannerSkipsSymlinks(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	os.MkdirAll(cacheDir, 0755)

	// A real 100-byte file that should be collected.
	real := filepath.Join(cacheDir, "real.tmp")
	if err := os.WriteFile(real, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}

	// A 4096-byte victim outside the scanned tree, reachable via a symlink inside it.
	victim := filepath.Join(tmp, "victim.bin")
	if err := os.WriteFile(victim, make([]byte, 4096), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(cacheDir, "evil.tmp")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	stats := scanOne(t, &config.Category{
		Name:    "Test Cache",
		Paths:   []string{cacheDir},
		Filters: []string{`\.tmp$`},
	})

	if stats.FileCount != 1 {
		t.Errorf("expected only the regular file to be collected, got %d: %+v",
			stats.FileCount, stats.Files)
	}
	if stats.Size != 100 {
		t.Errorf("reported size must exclude symlinks: expected 100, got %d", stats.Size)
	}
	for _, f := range stats.Files {
		if filepath.Base(f.Path) == "evil.tmp" {
			t.Error("symlink was collected")
		}
	}
}

// A category path that is itself a symlink to a file must not be collected --
// glob expansion of category paths can produce these.
func TestScannerSkipsSymlinkAsCategoryRoot(t *testing.T) {
	tmp := t.TempDir()
	victim := filepath.Join(tmp, "victim.bin")
	if err := os.WriteFile(victim, make([]byte, 2048), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "link.tmp")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	stats := scanOne(t, &config.Category{
		Name:  "Test Cache",
		Paths: []string{link},
	})

	if stats.FileCount != 0 {
		t.Errorf("a symlinked category root must not be collected, got %+v", stats.Files)
	}
	if stats.Size != 0 {
		t.Errorf("expected size 0, got %d", stats.Size)
	}
}

// Provenance must be recorded, because the revalidation gate refuses any cache
// entry that lacks it.
func TestScannerRecordsCategoryProvenance(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.tmp"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := scanOne(t, &config.Category{
		Name:     "Test Cache",
		Paths:    []string{tmp},
		Filters:  []string{`\.tmp$`},
		Risk:     config.Medium,
		Selected: true,
	})

	if stats.FileCount != 1 {
		t.Fatalf("expected 1 file, got %d", stats.FileCount)
	}
	f := stats.Files[0]
	if f.CategoryName != "Test Cache" {
		t.Errorf("CategoryName not recorded: %q", f.CategoryName)
	}
	if f.CategoryRisk != config.Medium {
		t.Errorf("CategoryRisk not recorded: %v", f.CategoryRisk)
	}
	if f.ModTime == "" {
		t.Error("ModTime must be recorded so the gate can detect changes")
	}
	if !f.CategorySelected {
		t.Error("CategorySelected not recorded")
	}
}

// shouldIncludeFile rejects everything that is not a regular file.
func TestShouldIncludeFileRejectsNonRegular(t *testing.T) {
	s := NewScanner(&config.Config{})
	cat := &config.Category{}

	cases := []struct {
		name string
		info *mockFileInfo
	}{
		{"directory", &mockFileInfo{name: "d", isDir: true}},
		{"symlink", &mockFileInfo{name: "l", mode: os.ModeSymlink | 0777}},
		{"socket", &mockFileInfo{name: "s", mode: os.ModeSocket | 0666}},
		{"device", &mockFileInfo{name: "dev", mode: os.ModeDevice | 0666}},
		{"fifo", &mockFileInfo{name: "p", mode: os.ModeNamedPipe | 0666}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if s.shouldIncludeFile("/tmp/"+tc.info.name, tc.info, cat) {
				t.Errorf("%s must not be collected", tc.name)
			}
		})
	}

	if !s.shouldIncludeFile("/tmp/regular", &mockFileInfo{name: "regular"}, cat) {
		t.Error("a regular file must still be collected")
	}
}
