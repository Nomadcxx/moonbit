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

func TestAutomationUnitsDeclareMutualExclusion(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

	for _, timer := range []string{"moonbit-scan.timer", "moonbit-clean.timer"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, "systemd", timer))
		if err != nil {
			t.Fatalf("cannot read %s: %v", timer, err)
		}
		if !strings.Contains(string(data), "Conflicts=moonbit-daemon.service") {
			t.Errorf("%s must conflict with daemon mode", timer)
		}
	}

	for _, service := range []string{"moonbit-scan.service", "moonbit-clean.service"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, "systemd", service))
		if err != nil {
			t.Fatalf("cannot read %s: %v", service, err)
		}
		if !strings.Contains(string(data), "Conflicts=moonbit-daemon.service") {
			t.Errorf("%s must conflict with daemon mode", service)
		}
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, "systemd", "moonbit-daemon.service"))
	if err != nil {
		t.Fatalf("cannot read daemon unit: %v", err)
	}
	if !strings.Contains(string(data), "Conflicts=moonbit-scan.service moonbit-clean.service moonbit-scan.timer moonbit-clean.timer") {
		t.Fatal("daemon service must conflict with scan and clean services and timers")
	}
}

func TestCleanPreScanDoesNotPrompt(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(repoRoot, "systemd", "moonbit-clean.service"))
	if err != nil {
		t.Fatalf("cannot read clean service: %v", err)
	}
	if !strings.Contains(string(data), "ExecStartPre=/usr/local/bin/moonbit scan --mode quick --no-prompt") {
		t.Fatal("clean service pre-scan must pass --no-prompt")
	}
}

func TestSourceUninstallerUsesOnlyItsInstallPath(t *testing.T) {
	if got := sourceBinaryPath; got != "/usr/local/bin/moonbit" {
		t.Fatalf("source uninstaller path = %q, want /usr/local/bin/moonbit", got)
	}
}

func TestRemoveBinaryPathLeavesPackageOwnedBinary(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "usr", "local", "bin", "moonbit")
	packagePath := filepath.Join(root, "usr", "bin", "moonbit")
	for _, path := range []string{sourcePath, packagePath} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("binary"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := removeBinaryPath(sourcePath); err != nil {
		t.Fatalf("remove source binary: %v", err)
	}
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("source binary still exists: %v", err)
	}
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("package-owned binary was removed: %v", err)
	}
}

func TestInstallerExitCodesTrackCompletionAndCriticalFailures(t *testing.T) {
	if got := installerExitCode(newModel()); got != 2 {
		t.Fatalf("welcome screen exit code = %d, want aborted code 2", got)
	}
	if got := installerExitCode(nil); got != 2 {
		t.Fatalf("unknown final model exit code = %d, want aborted code 2", got)
	}

	success := newModel()
	success.step = stepComplete
	for i := range success.tasks {
		success.tasks[i].status = statusComplete
	}
	if got := installerExitCode(success); got != 0 {
		t.Fatalf("completed installer exit code = %d, want 0", got)
	}

	failed := newModel()
	failed.step = stepInstalling
	failed.currentTaskIndex = 0
	failed.tasks[0].status = statusRunning
	updated, _ := failed.Update(taskCompleteMsg{index: 0, error: "permission denied"})
	completed := updated.(model)
	if got := installerExitCode(completed); got != 1 {
		t.Fatalf("critical task failure exit code = %d, want 1", got)
	}
	if got := completed.renderComplete(); !strings.Contains(got, "Installation failed.") || !strings.Contains(got, "permission denied") {
		t.Fatalf("completion view does not show critical task failure: %q", got)
	}

	optional := newModel()
	optional.step = stepInstalling
	optional.tasks = []installTask{{name: "Optional task", optional: true, status: statusRunning}}
	optional.currentTaskIndex = 0
	updated, _ = optional.Update(taskCompleteMsg{index: 0, error: "optional operation failed"})
	completed = updated.(model)
	if got := installerExitCode(completed); got != 0 {
		t.Fatalf("optional task warning exit code = %d, want 0", got)
	}
	if got := completed.renderComplete(); !strings.Contains(got, "Installation completed with warnings.") ||
		!strings.Contains(got, "Task warnings:") || !strings.Contains(got, "optional operation failed") {
		t.Fatalf("completion view does not show optional task warning: %q", got)
	}

	uninstallFailure := model{
		step:          stepComplete,
		uninstallMode: true,
		tasks:         []installTask{{name: "Remove binary", status: statusFailed}},
		errors:        []string{"Remove binary: permission denied"},
	}
	if got := uninstallFailure.renderComplete(); !strings.Contains(got, "Uninstall failed.") ||
		strings.Contains(got, "Uninstall completed with warnings.") {
		t.Fatalf("uninstall completion view lost critical severity: %q", got)
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
