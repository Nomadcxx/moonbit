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

	// Absolute path: the launcher's PATH is not guaranteed to include the
	// install prefix, and a bare name would silently resolve to nothing.
	execLine := keys["Exec"]
	if !strings.HasPrefix(execLine, "/") {
		t.Errorf("Exec must be an absolute path, got %q", execLine)
	}
	if !strings.HasSuffix(execLine, " --launcher") {
		t.Errorf("Exec must pass --launcher so moonbit opens its own terminal, got %q", execLine)
	}

	// pkexec cannot be used: polkit's auth_admin requires a session attached to
	// a seat, and compositors running as a systemd user service give their
	// children a seatless session, so polkit refuses without prompting.
	if strings.Contains(execLine, "pkexec") {
		t.Errorf("Exec must not use pkexec, got %q", execLine)
	}

	// Terminal=true delegates terminal selection to the launcher, which commonly
	// defaults to a terminal that is not installed. moonbit finds its own.
	if keys["Terminal"] != "false" {
		t.Errorf("Terminal must be false; moonbit opens its own terminal, got %q", keys["Terminal"])
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
