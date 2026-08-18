package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The installer previously skipped installing the timer units when the user
// chose daemon (or manual) mode. Timer and daemon modes are mutually exclusive
// at runtime, not at install time -- so the units were absent from
// /etc/systemd/system and the TUI's Schedule screen, whose whole purpose is
// switching modes later, failed with a bare "exit status 1".
//
// This asserts the installer ships every unit that exists in the repo.
func TestInstallerCoversEveryShippedUnit(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	unitDir := filepath.Join(repoRoot, "systemd")

	entries, err := os.ReadDir(unitDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", unitDir, err)
	}

	onDisk := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".service") || strings.HasSuffix(name, ".timer") {
			onDisk[name] = true
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("no unit files found; test is not exercising anything")
	}

	installed := map[string]bool{}
	for _, f := range systemdUnitFiles() {
		installed[filepath.Base(f)] = true

		if _, err := os.Stat(filepath.Join(repoRoot, f)); err != nil {
			t.Errorf("installer lists %s but it does not exist in the repo: %v", f, err)
		}
	}

	for name := range onDisk {
		if !installed[name] {
			t.Errorf("systemd/%s ships in the repo but the installer never installs it; "+
				"it cannot be enabled from the TUI", name)
		}
	}
}

// Both automation modes must be installable, so both sets of units have to land.
func TestInstallerShipsBothTimerAndDaemonUnits(t *testing.T) {
	installed := map[string]bool{}
	for _, f := range systemdUnitFiles() {
		installed[filepath.Base(f)] = true
	}

	for _, required := range []string{
		"moonbit-scan.service",
		"moonbit-scan.timer",
		"moonbit-clean.service",
		"moonbit-clean.timer",
		"moonbit-daemon.service",
	} {
		if !installed[required] {
			t.Errorf("%s is not installed by the installer", required)
		}
	}
}

// The launcher entry is the only way a user who avoids terminals starts moonbit,
// so its two fragile properties are worth pinning.
func TestDesktopEntry(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(repoRoot, "packaging", "moonbit.desktop"))
	if err != nil {
		t.Fatalf("cannot read desktop entry: %v", err)
	}

	keys := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			keys[k] = v
		}
	}

	for _, required := range []string{"Type", "Name", "Exec", "Icon", "Terminal", "Categories"} {
		if keys[required] == "" {
			t.Errorf("desktop entry is missing %s", required)
		}
	}

	// pkexec sanitises PATH to /usr/sbin:/usr/bin:/sbin:/bin. A bare program name
	// resolves against that, so /usr/local/bin/moonbit would not be found and the
	// launcher would silently do nothing.
	exec := keys["Exec"]
	if !strings.HasPrefix(exec, "pkexec /") {
		t.Errorf("Exec must invoke pkexec with an absolute path, got %q", exec)
	}

	// moonbit is a TUI; without a terminal the launcher opens nothing.
	if keys["Terminal"] != "true" {
		t.Errorf("Terminal must be true for a TUI, got %q", keys["Terminal"])
	}

	// Icon is looked up by name in the theme, so it must match the installed file.
	if keys["Icon"] != "moonbit" {
		t.Errorf("Icon should be the theme name \"moonbit\", got %q", keys["Icon"])
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "packaging", keys["Icon"]+".svg")); err != nil {
		t.Errorf("no icon file matching Icon=%s: %v", keys["Icon"], err)
	}
}

// Every packaging route has to rewrite Exec away from /usr/local/bin, or the
// packaged launcher points at a binary that route never installs.
func TestPackagingRewritesDesktopExec(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	for _, f := range []string{"PKGBUILD", "flake.nix", ".github/workflows/release.yml"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Errorf("cannot read %s: %v", f, err)
			continue
		}
		if !strings.Contains(string(b), "moonbit.desktop") {
			t.Errorf("%s does not install the desktop entry", f)
		}
	}
}
