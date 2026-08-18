package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// stubTerminal creates an executable with the given name in a temp dir and puts
// that dir at the front of PATH.
func stubTerminal(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

// $TERMINAL is frequently stale, naming a terminal that was uninstalled. It must
// not win in that case, or moonbit opens nothing and the user sees no error.
func TestFindTerminalIgnoresStaleTERMINAL(t *testing.T) {
	stubTerminal(t, "kitty")
	t.Setenv("TERMINAL", "ghostty") // set, but not installed

	got, ok := findTerminal()
	if !ok {
		t.Fatal("expected to fall back to an installed terminal")
	}
	if filepath.Base(got.bin) != "kitty" {
		t.Errorf("expected kitty, got %q", got.bin)
	}
}

// A $TERMINAL that does exist is the user's explicit choice and must win.
func TestFindTerminalHonoursInstalledTERMINAL(t *testing.T) {
	stubTerminal(t, "kitty", "foot")
	t.Setenv("TERMINAL", "foot")

	got, ok := findTerminal()
	if !ok {
		t.Fatal("expected a terminal")
	}
	if filepath.Base(got.bin) != "foot" {
		t.Errorf("$TERMINAL should win: expected foot, got %q", got.bin)
	}
}

// An unrecognised $TERMINAL still gets used, with the common -e convention.
func TestFindTerminalAcceptsUnknownTERMINAL(t *testing.T) {
	stubTerminal(t, "my-cool-term")
	t.Setenv("TERMINAL", "my-cool-term")

	got, ok := findTerminal()
	if !ok {
		t.Fatal("expected the unknown terminal to be accepted")
	}
	if filepath.Base(got.bin) != "my-cool-term" {
		t.Errorf("expected my-cool-term, got %q", got.bin)
	}
	if len(got.args) != 1 || got.args[0] != "-e" {
		t.Errorf("expected -e for an unknown terminal, got %v", got.args)
	}
}

// Preference order matters: a Wayland-native terminal should beat xterm.
func TestFindTerminalPrefersModernOverXterm(t *testing.T) {
	stubTerminal(t, "xterm", "foot")
	t.Setenv("TERMINAL", "")

	got, ok := findTerminal()
	if !ok {
		t.Fatal("expected a terminal")
	}
	if filepath.Base(got.bin) != "foot" {
		t.Errorf("expected foot to be preferred over xterm, got %q", got.bin)
	}
}

// With nothing installed, the caller must be told rather than left guessing.
func TestFindTerminalReportsNoneFound(t *testing.T) {
	stubTerminal(t) // empty dir on PATH
	t.Setenv("TERMINAL", "")

	if _, ok := findTerminal(); ok {
		t.Error("expected no terminal to be found")
	}
}

// The argument form is not universal; getting it wrong opens a window that
// closes immediately with no error, which is the failure this whole path exists
// to avoid.
func TestTerminalArgumentForms(t *testing.T) {
	want := map[string][]string{
		"ghostty":        {"-e"},
		"kitty":          {"-e"},
		"foot":           {"-e"},
		"alacritty":      {"-e"},
		"wezterm":        {"start", "--"},
		"gnome-terminal": {"--"},
		"xfce4-terminal": {"-x"},
		"xterm":          {"-e"},
	}
	got := map[string][]string{}
	for _, c := range terminalCandidates {
		got[c.bin] = c.args
	}
	for bin, args := range want {
		g, ok := got[bin]
		if !ok {
			t.Errorf("%s missing from the candidate list", bin)
			continue
		}
		if len(g) != len(args) {
			t.Errorf("%s: expected %v, got %v", bin, args, g)
			continue
		}
		for i := range args {
			if g[i] != args[i] {
				t.Errorf("%s: expected %v, got %v", bin, args, g)
				break
			}
		}
	}
}

// Running with a terminal already attached must not spawn another one.
func TestRunFromLauncherIsNoOpWithATerminal(t *testing.T) {
	if !hasControllingTerminal() {
		t.Skip("no controlling terminal in this environment; nothing to assert")
	}
	if err := RunFromLauncher(); err != nil {
		t.Errorf("with a terminal attached this should do nothing, got %v", err)
	}
}

// Under a launcher with no terminal available, the failure must be reported.
func TestRunFromLauncherErrorsWithNoTerminal(t *testing.T) {
	if hasControllingTerminal() {
		t.Skip("this assertion needs a process without a controlling terminal")
	}
	stubTerminal(t)
	t.Setenv("TERMINAL", "")

	if err := RunFromLauncher(); err == nil {
		t.Error("expected an error when no terminal emulator exists")
	}
}
