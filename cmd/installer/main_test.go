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
