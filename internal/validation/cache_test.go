package validation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
)

// helper: write a file and return the FileInfo the scanner would have recorded.
func scanned(t *testing.T, path string, contents []byte, category string) config.FileInfo {
	t.Helper()
	if err := os.WriteFile(path, contents, 0644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return config.FileInfo{
		Path:         path,
		Size:         uint64(st.Size()),
		ModTime:      st.ModTime().Format(time.RFC3339),
		CategoryName: category,
	}
}

func cacheOf(files ...config.FileInfo) *config.SessionCache {
	var total uint64
	for _, f := range files {
		total += f.Size
	}
	return &config.SessionCache{
		ScanResults: &config.Category{Name: "Total Cleanable", Files: files, Size: total},
		TotalSize:   total,
		TotalFiles:  len(files),
		ScannedAt:   time.Now(),
	}
}

// SEC-0: the cache is user-writable but consumed as root. Paths it names that no
// configured category covers must never reach the cleaner.
func TestRevalidateRejectsPathsOutsideConfiguredCategories(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	elsewhere := filepath.Join(tmp, "elsewhere")
	os.MkdirAll(cacheDir, 0755)
	os.MkdirAll(elsewhere, 0755)

	legit := scanned(t, filepath.Join(cacheDir, "junk.tmp"), []byte("aaaa"), "Test Cache")
	// Attacker-supplied entry: claims the same category, lives somewhere else.
	forged := scanned(t, filepath.Join(elsewhere, "important.db"), []byte("bbbb"), "Test Cache")

	categories := []config.Category{{
		Name:    "Test Cache",
		Paths:   []string{cacheDir},
		Filters: []string{`\.tmp$`},
		Risk:    config.Low,
	}}

	out, report, err := RevalidateCache(cacheOf(legit, forged), categories, CacheOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.TotalFiles != 1 {
		t.Fatalf("expected exactly 1 surviving file, got %d", out.TotalFiles)
	}
	if out.ScanResults.Files[0].Path != legit.Path {
		t.Errorf("wrong survivor: %s", out.ScanResults.Files[0].Path)
	}
	if report.Dropped[DropOutsideCategory] != 1 {
		t.Errorf("expected the forged path to be dropped as out-of-category, got %v", report.Dropped)
	}

	// And the file itself must still be on disk.
	if _, err := os.Stat(forged.Path); err != nil {
		t.Errorf("forged target should be untouched: %v", err)
	}
}

// SEC-0: Risk and ShredEnabled must come from config, not from the cache, or the
// safety checks are self-attested by the thing they guard against.
func TestRevalidateTakesRiskAndShredFromConfigNotCache(t *testing.T) {
	tmp := t.TempDir()
	f := scanned(t, filepath.Join(tmp, "x.tmp"), []byte("aaaa"), "Test Cache")

	c := cacheOf(f)
	// Attacker claims Low risk and shredding on the aggregate category.
	c.ScanResults.Risk = config.Low
	c.ScanResults.ShredEnabled = true

	categories := []config.Category{{
		Name:         "Test Cache",
		Paths:        []string{tmp},
		Filters:      []string{`\.tmp$`},
		Risk:         config.High, // config says High
		ShredEnabled: false,       // config says no shredding
	}}

	out, _, err := RevalidateCache(c, categories, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.ScanResults.Risk != config.High {
		t.Errorf("risk must come from config (High), got %v", out.ScanResults.Risk)
	}
	if out.ScanResults.ShredEnabled {
		t.Error("aggregate ShredEnabled must never be honoured from the cache")
	}
	if out.ScanResults.Files[0].CategoryShred {
		t.Error("per-file shred must come from config (false), not the cache")
	}
}

// A cache claiming a category that config does not define is not authorised.
func TestRevalidateRejectsUnknownCategory(t *testing.T) {
	tmp := t.TempDir()
	f := scanned(t, filepath.Join(tmp, "x.tmp"), []byte("aaaa"), "Attacker Controlled")

	out, report, err := RevalidateCache(cacheOf(f), []config.Category{{
		Name:  "Test Cache",
		Paths: []string{tmp},
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 0 {
		t.Fatalf("expected nothing to survive, got %d", out.TotalFiles)
	}
	if report.Dropped[DropUnknownCategory] != 1 {
		t.Errorf("expected unknown-category drop, got %v", report.Dropped)
	}
}

// SEC-1 reaches the cleaner only if a symlink survives the gate. It must not.
func TestRevalidateRejectsSymlinks(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	os.MkdirAll(cacheDir, 0755)

	victim := filepath.Join(tmp, "victim.db")
	os.WriteFile(victim, []byte("precious"), 0644)

	link := filepath.Join(cacheDir, "evil.tmp")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	st, _ := os.Stat(link) // deliberately Stat: records the target's size
	entry := config.FileInfo{
		Path:         link,
		Size:         uint64(st.Size()),
		ModTime:      st.ModTime().Format(time.RFC3339),
		CategoryName: "Test Cache",
	}

	out, report, err := RevalidateCache(cacheOf(entry), []config.Category{{
		Name:    "Test Cache",
		Paths:   []string{cacheDir},
		Filters: []string{`\.tmp$`},
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 0 {
		t.Fatalf("a symlink must never survive revalidation, got %d survivors", out.TotalFiles)
	}
	if report.Dropped[DropNotRegular] != 1 {
		t.Errorf("expected not-regular drop, got %v", report.Dropped)
	}
}

// The subtle escape: a symlinked *parent* directory inside an authorised tree.
// Lstat on the full path reports an ordinary regular file, so only comparing the
// resolved path against the resolved category root catches this.
func TestRevalidateRejectsEscapeViaSymlinkedParentDirectory(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	secretDir := filepath.Join(tmp, "secret")
	os.MkdirAll(cacheDir, 0755)
	os.MkdirAll(secretDir, 0755)

	victim := filepath.Join(secretDir, "important.tmp")
	os.WriteFile(victim, []byte("precious"), 0644)

	// cache/escape -> ../secret
	if err := os.Symlink(secretDir, filepath.Join(cacheDir, "escape")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	viaLink := filepath.Join(cacheDir, "escape", "important.tmp")
	st, err := os.Lstat(viaLink)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Mode().IsRegular() {
		t.Fatal("precondition: Lstat through a symlinked parent should look regular")
	}

	entry := config.FileInfo{
		Path:         viaLink,
		Size:         uint64(st.Size()),
		ModTime:      st.ModTime().Format(time.RFC3339),
		CategoryName: "Test Cache",
	}

	out, report, err := RevalidateCache(cacheOf(entry), []config.Category{{
		Name:    "Test Cache",
		Paths:   []string{cacheDir},
		Filters: []string{`\.tmp$`},
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 0 {
		t.Fatalf("escape via symlinked parent must be rejected, got %d survivors", out.TotalFiles)
	}
	if report.Dropped[DropOutsideCategory] != 1 {
		t.Errorf("expected out-of-category drop, got %v", report.Dropped)
	}
}

// A legitimately symlinked category root must keep working.
func TestRevalidateAcceptsSymlinkedCategoryRoot(t *testing.T) {
	tmp := t.TempDir()
	realCache := filepath.Join(tmp, "real-cache")
	os.MkdirAll(realCache, 0755)

	linkedRoot := filepath.Join(tmp, "cache")
	if err := os.Symlink(realCache, linkedRoot); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	f := scanned(t, filepath.Join(linkedRoot, "junk.tmp"), []byte("aaaa"), "Test Cache")

	out, _, err := RevalidateCache(cacheOf(f), []config.Category{{
		Name:    "Test Cache",
		Paths:   []string{linkedRoot},
		Filters: []string{`\.tmp$`},
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 1 {
		t.Fatalf("a symlinked cache root is legitimate and must still clean, got %d", out.TotalFiles)
	}
}

// SEC-3: a stale cache must be refused outright.
func TestRevalidateRejectsStaleCache(t *testing.T) {
	tmp := t.TempDir()
	f := scanned(t, filepath.Join(tmp, "x.tmp"), []byte("aaaa"), "Test Cache")

	c := cacheOf(f)
	c.ScannedAt = time.Now().Add(-48 * time.Hour)

	_, _, err := RevalidateCache(c, []config.Category{{Name: "Test Cache", Paths: []string{tmp}}},
		CacheOptions{MaxAge: 24 * time.Hour})
	if err == nil {
		t.Fatal("expected a stale cache to be refused")
	}
}

func TestRevalidateRejectsCacheWithoutTimestampOrProvenance(t *testing.T) {
	tmp := t.TempDir()

	t.Run("no timestamp", func(t *testing.T) {
		f := scanned(t, filepath.Join(tmp, "a.tmp"), []byte("aaaa"), "Test Cache")
		c := cacheOf(f)
		c.ScannedAt = time.Time{}
		if _, _, err := RevalidateCache(c, nil, CacheOptions{}); err == nil {
			t.Error("expected refusal when ScannedAt is zero")
		}
	})

	t.Run("no category provenance", func(t *testing.T) {
		f := scanned(t, filepath.Join(tmp, "b.tmp"), []byte("aaaa"), "")
		if _, _, err := RevalidateCache(cacheOf(f), nil, CacheOptions{}); err == nil {
			t.Error("expected refusal when a file carries no CategoryName")
		}
	})
}

// SEC-3 / accuracy: a file that changed between scan and clean is out of scope.
func TestRevalidateRejectsChangedFiles(t *testing.T) {
	tmp := t.TempDir()
	f := scanned(t, filepath.Join(tmp, "x.tmp"), []byte("aaaaaaaaaa"), "Test Cache")
	c := cacheOf(f)

	// Daemon truncates the file after the scan recorded it.
	os.WriteFile(f.Path, []byte("a"), 0644)

	categories := []config.Category{{Name: "Test Cache", Paths: []string{tmp}, Filters: []string{`\.tmp$`}}}
	out, report, err := RevalidateCache(c, categories, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 0 {
		t.Fatalf("a changed file must not be cleaned, got %d survivors", out.TotalFiles)
	}
	if report.Dropped[DropChanged] != 1 {
		t.Errorf("expected changed drop, got %v", report.Dropped)
	}
}

func TestRevalidateRejectsMissingFiles(t *testing.T) {
	tmp := t.TempDir()
	f := scanned(t, filepath.Join(tmp, "x.tmp"), []byte("aaaa"), "Test Cache")
	c := cacheOf(f)
	os.Remove(f.Path)

	out, report, err := RevalidateCache(c, []config.Category{{
		Name: "Test Cache", Paths: []string{tmp}, Filters: []string{`\.tmp$`},
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 0 {
		t.Fatal("a missing file must not be reported as cleanable")
	}
	if report.Dropped[DropMissing] != 1 {
		t.Errorf("expected missing drop, got %v", report.Dropped)
	}
}

// Filters and exclude patterns are re-applied from config, not trusted.
func TestRevalidateReappliesFiltersAndExcludes(t *testing.T) {
	tmp := t.TempDir()
	match := scanned(t, filepath.Join(tmp, "keep.tmp"), []byte("aaaa"), "Test Cache")
	nonMatch := scanned(t, filepath.Join(tmp, "keep.conf"), []byte("aaaa"), "Test Cache")
	excluded := scanned(t, filepath.Join(tmp, "skip-me.tmp"), []byte("aaaa"), "Test Cache")

	out, report, err := RevalidateCache(cacheOf(match, nonMatch, excluded), []config.Category{{
		Name:            "Test Cache",
		Paths:           []string{tmp},
		Filters:         []string{`\.tmp$`},
		ExcludePatterns: []string{`skip-me`},
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 1 || out.ScanResults.Files[0].Path != match.Path {
		t.Fatalf("expected only keep.tmp to survive, got %+v", out.ScanResults.Files)
	}
	if report.Dropped[DropFilterMismatch] != 1 {
		t.Errorf("expected a filter-mismatch drop, got %v", report.Dropped)
	}
	if report.Dropped[DropExcluded] != 1 {
		t.Errorf("expected an exclude drop, got %v", report.Dropped)
	}
}

func TestRevalidateReappliesMinAgeDays(t *testing.T) {
	tmp := t.TempDir()
	f := scanned(t, filepath.Join(tmp, "new.tmp"), []byte("aaaa"), "Thumbnails")

	out, report, err := RevalidateCache(cacheOf(f), []config.Category{{
		Name:       "Thumbnails",
		Paths:      []string{tmp},
		Filters:    []string{`\.tmp$`},
		MinAgeDays: 30,
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 0 {
		t.Fatal("a file newer than min_age_days must not be cleaned")
	}
	if report.Dropped[DropTooRecent] != 1 {
		t.Errorf("expected too-recent drop, got %v", report.Dropped)
	}
}

// The gate must carry the configured action through, so held-open logs get
// truncated rather than unlinked.
func TestRevalidateCarriesConfiguredAction(t *testing.T) {
	tmp := t.TempDir()
	f := scanned(t, filepath.Join(tmp, "container.log"), []byte("aaaa"), "Docker Container Logs")

	out, _, err := RevalidateCache(cacheOf(f), []config.Category{{
		Name:    "Docker Container Logs",
		Paths:   []string{tmp},
		Filters: []string{`\.log$`},
		Action:  config.ActionTruncate,
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles != 1 {
		t.Fatalf("expected 1 survivor, got %d", out.TotalFiles)
	}
	if out.ScanResults.Files[0].CategoryAction != config.ActionTruncate {
		t.Error("configured truncate action must reach the cleaner")
	}
}

// The gate is subtractive: it never invents entries.
func TestRevalidateIsSubtractive(t *testing.T) {
	tmp := t.TempDir()
	a := scanned(t, filepath.Join(tmp, "a.tmp"), []byte("aaaa"), "Test Cache")
	b := scanned(t, filepath.Join(tmp, "b.tmp"), []byte("bbbb"), "Test Cache")

	in := cacheOf(a, b)
	out, _, err := RevalidateCache(in, []config.Category{{
		Name: "Test Cache", Paths: []string{tmp}, Filters: []string{`\.tmp$`},
	}}, CacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalFiles > in.TotalFiles {
		t.Fatalf("gate added entries: %d in, %d out", in.TotalFiles, out.TotalFiles)
	}

	original := map[string]bool{a.Path: true, b.Path: true}
	for _, f := range out.ScanResults.Files {
		if !original[f.Path] {
			t.Errorf("gate emitted a path that was not in the input: %s", f.Path)
		}
	}
}

func TestReportSummary(t *testing.T) {
	r := newReport()
	if r.Summary() != "" {
		t.Error("empty report should render an empty summary")
	}
	r.drop(DropMissing, "/a")
	r.drop(DropMissing, "/b")
	r.drop(DropNotRegular, "/c")
	if r.TotalDropped() != 3 {
		t.Errorf("expected 3 drops, got %d", r.TotalDropped())
	}
	if r.Summary() == "" {
		t.Error("non-empty report should render a summary")
	}
	if len(r.Examples[DropMissing]) != 2 {
		t.Errorf("expected 2 recorded examples, got %d", len(r.Examples[DropMissing]))
	}
}
