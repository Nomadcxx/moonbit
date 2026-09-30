package config

import (
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"testing"
)

func categoryNamed(cfg *Config, name string) *Category {
	for i := range cfg.Categories {
		if cfg.Categories[i].Name == name {
			return &cfg.Categories[i]
		}
	}
	return nil
}

// OPS-1: the default config must not delete files a running daemon holds open.
//
// journald mmaps its active journal files; unlinking them corrupts the journal
// and reclaims nothing until journald restarts. `moonbit journal vacuum` drives
// journalctl --vacuum-* instead.
func TestNoSystemdJournalDeletionCategory(t *testing.T) {
	cfg := DefaultConfig()
	if cat := categoryNamed(cfg, "Systemd Journal"); cat != nil {
		t.Errorf("Systemd Journal must not be a file-deletion category; got paths %v", cat.Paths)
	}
	for _, cat := range cfg.Categories {
		for _, p := range cat.Paths {
			if filepath.Clean(p) == "/var/log/journal" {
				t.Errorf("category %q targets /var/log/journal for deletion", cat.Name)
			}
		}
	}
}

func TestSaveRejectsSymlinkedConfigDirectory(t *testing.T) {
	base := t.TempDir()
	configHome := filepath.Join(base, "xdg-config")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(configHome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(configHome, "moonbit")); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(configHome, "moonbit", "config.toml")
	if err := Save(DefaultConfig(), path); err == nil {
		t.Fatal("Save should reject a symlinked parent directory")
	}
	if _, err := os.Lstat(filepath.Join(outside, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("config was written through the symlink: lstat error = %v", err)
	}
}

func TestSaveRejectsFIFOConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if err := Save(DefaultConfig(), path); err == nil {
		t.Fatal("Save should reject a FIFO config file")
	}
	buf := make([]byte, 4096)
	if n, _ := reader.Read(buf); n != 0 {
		t.Fatalf("config data was written to a FIFO: %q", buf[:n])
	}
}

func TestSavePreservesExistingConfigPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("old config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("config permissions = %04o, want preserved 0600", got)
	}
}

func TestLoadRejectsSymlinkedConfigFile(t *testing.T) {
	base := t.TempDir()
	configHome := filepath.Join(base, "xdg-config")
	configDir := filepath.Join(configHome, "moonbit")
	externalConfig := filepath.Join(base, "external.toml")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalConfig, []byte("[scan]\nmax_depth = 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalConfig, filepath.Join(configDir, "config.toml")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)

	if _, err := Load(""); err == nil {
		t.Fatal("Load should reject a symlinked config file")
	}
}

// OPS-1: Docker container logs are held open by dockerd, so they are truncated.
func TestDockerContainerLogsUseTruncate(t *testing.T) {
	cat := categoryNamed(DefaultConfig(), "Docker Container Logs")
	if cat == nil {
		t.Fatal("Docker Container Logs category missing")
	}
	if cat.Action != ActionTruncate {
		t.Errorf("expected truncate action, got %q", cat.Action)
	}
}

// OPS-1: System Logs must match rotated artefacts only, never the active files
// rsyslog/journald and friends hold open, and must not be Low risk (Low is what
// the quick/automated path treats as safe).
func TestSystemLogsTargetsRotatedFilesOnly(t *testing.T) {
	cat := categoryNamed(DefaultConfig(), "System Logs")
	if cat == nil {
		t.Fatal("System Logs category missing")
	}

	if cat.Risk == Low {
		t.Error("System Logs must not be Low risk: it is the one category that can break system logging")
	}
	if cat.Selected {
		t.Error("System Logs must not be selected by default (quick mode only takes selected categories)")
	}

	var res []*regexp.Regexp
	for _, f := range cat.Filters {
		re, err := regexp.Compile(f)
		if err != nil {
			t.Fatalf("filter %q does not compile: %v", f, err)
		}
		res = append(res, re)
	}
	matches := func(name string) bool {
		for _, re := range res {
			if re.MatchString(name) {
				return true
			}
		}
		return false
	}

	// Active files a daemon holds open -- must NOT match.
	for _, active := range []string{
		"syslog", "messages", "daemon.log", "auth.log", "kern.log",
		"Xorg.0.log", "pacman.log", "dpkg.log", "jellyfin.log", "lightdm.log",
	} {
		if matches(active) {
			t.Errorf("active log %q must not be matched by System Logs filters", active)
		}
	}

	// Rotated artefacts -- SHOULD match.
	for _, rotated := range []string{
		"syslog.1", "messages.1.gz", "auth.log.1", "daemon.log.2.gz",
		"pacman.log.old", "Xorg.0.log.old", "nginx-20260818.gz",
	} {
		if !matches(rotated) {
			t.Errorf("rotated log %q should be matched by System Logs filters", rotated)
		}
	}
}

// Quick mode only takes Low-risk, selected categories. Nothing in that set may
// target files a daemon holds open, because the systemd timer runs
// `clean --force --mode quick` unattended as root.
func TestQuickModeCategoriesAreSafeForUnattendedUse(t *testing.T) {
	dangerous := []string{"/var/log", "/var/log/journal", "/var/lib/docker/containers"}

	for _, cat := range DefaultConfig().Categories {
		if cat.Risk != Low || !cat.Selected {
			continue // not in quick mode
		}
		for _, p := range cat.Paths {
			clean := filepath.Clean(p)
			for _, d := range dangerous {
				if clean == d {
					t.Errorf("category %q is in the unattended quick-mode path but targets %s",
						cat.Name, d)
				}
			}
		}
	}
}

// AuthoritativeCategories is the trust anchor for cache revalidation, so it must
// include the runtime-detected categories as well as configured ones.
func TestAuthoritativeCategoriesIncludesDynamic(t *testing.T) {
	cfg := &Config{Categories: []Category{{Name: "Configured"}}}
	all := AuthoritativeCategories(cfg)

	if len(all) < 1 || all[0].Name != "Configured" {
		t.Fatalf("configured categories must be included, got %+v", all)
	}
	// Dynamic categories are environment-dependent; assert the contract instead.
	if len(all) != len(cfg.Categories)+len(DynamicCategories()) {
		t.Errorf("expected configured + dynamic, got %d", len(all))
	}

	if got := AuthoritativeCategories(nil); len(got) != len(DynamicCategories()) {
		t.Error("nil config should still yield the dynamic categories")
	}
}

func TestDynamicThumbnailCategoryKeepsMinimumAge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MOONBIT_HOME", home)
	thumbnailPath := filepath.Join(home, ".cache", "thumbnails")
	if err := os.MkdirAll(thumbnailPath, 0755); err != nil {
		t.Fatal(err)
	}

	for _, category := range DynamicCategories() {
		if category.Name == "Thumbnail Cache" {
			if category.MinAgeDays != 30 {
				t.Fatalf("dynamic thumbnails min age = %d, want 30", category.MinAgeDays)
			}
			return
		}
	}
	t.Fatal("expected dynamic thumbnail category")
}

func TestValidateRejectsUnsafeCategoryRules(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"scan ignore pattern", func(cfg *Config) { cfg.Scan.IgnorePatterns = []string{"("} }},
		{"category filter", func(cfg *Config) { cfg.Categories[0].Filters = []string{"("} }},
		{"category exclude", func(cfg *Config) { cfg.Categories[0].ExcludePatterns = []string{"("} }},
		{"unknown action", func(cfg *Config) { cfg.Categories[0].Action = "wipe" }},
		{"duplicate category", func(cfg *Config) { cfg.Categories = append(cfg.Categories, cfg.Categories[0]) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.edit(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate accepted an unsafe rule")
			}
		})
	}
}

func TestLoadRejectsInvalidIgnorePattern(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[scan]\nignore_patterns = [\"(\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load should reject an invalid ignore pattern")
	}
}

// Every default category's filters must compile -- an invalid pattern is
// silently skipped by both the scanner and the gate, which would widen scope.
func TestAllDefaultFiltersCompile(t *testing.T) {
	for _, cat := range DefaultConfig().Categories {
		for _, f := range cat.Filters {
			if _, err := regexp.Compile(f); err != nil {
				t.Errorf("category %q filter %q does not compile: %v", cat.Name, f, err)
			}
		}
		for _, e := range cat.ExcludePatterns {
			if _, err := regexp.Compile(e); err != nil {
				t.Errorf("category %q exclude %q does not compile: %v", cat.Name, e, err)
			}
		}
	}
}
