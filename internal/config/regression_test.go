package config

import (
	"path/filepath"
	"regexp"
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
