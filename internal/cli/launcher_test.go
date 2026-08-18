package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// stubTerminals creates executables with the given names in a temp dir and puts
// that dir at the front of PATH.
func stubTerminals(t *testing.T, names ...string) string {
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

func firstCandidate(t *testing.T) (terminalCandidate, bool) {
	t.Helper()
	c := candidatesToTry()
	if len(c) == 0 {
		return terminalCandidate{}, false
	}
	return c[0], true
}

// MOONBIT_TERMINAL is the escape hatch for desktops the built-in list gets
// wrong, so it must beat both $TERMINAL and the list.
func TestMoonbitTerminalOverrideWins(t *testing.T) {
	stubTerminals(t, "kitty", "foot", "weird-term")
	t.Setenv("TERMINAL", "kitty")
	t.Setenv(TerminalEnvVar, "weird-term --run")

	got, ok := firstCandidate(t)
	if !ok {
		t.Fatal("expected a candidate")
	}
	if filepath.Base(got.bin) != "weird-term" {
		t.Errorf("override should win, got %q", got.bin)
	}
	if len(got.args) != 1 || got.args[0] != "--run" {
		t.Errorf("override args should be honoured, got %v", got.args)
	}
}

// An override naming only a binary gets the widest convention.
func TestMoonbitTerminalOverrideDefaultsToDashE(t *testing.T) {
	stubTerminals(t, "weird-term")
	t.Setenv("TERMINAL", "")
	t.Setenv(TerminalEnvVar, "weird-term")

	got, ok := firstCandidate(t)
	if !ok {
		t.Fatal("expected a candidate")
	}
	if len(got.args) != 1 || got.args[0] != "-e" {
		t.Errorf("expected -e default, got %v", got.args)
	}
}

// $TERMINAL is frequently stale, naming a terminal that was uninstalled. It must
// not win in that case, or moonbit opens nothing and the user sees no error.
func TestStaleTerminalEnvIsIgnored(t *testing.T) {
	stubTerminals(t, "kitty")
	t.Setenv(TerminalEnvVar, "")
	t.Setenv("TERMINAL", "ghostty") // set, but not installed

	got, ok := firstCandidate(t)
	if !ok {
		t.Fatal("expected to fall back to an installed terminal")
	}
	if filepath.Base(got.bin) != "kitty" {
		t.Errorf("expected kitty, got %q", got.bin)
	}
}

// A $TERMINAL that does exist is the user's explicit choice.
func TestInstalledTerminalEnvIsHonoured(t *testing.T) {
	stubTerminals(t, "kitty", "foot")
	t.Setenv(TerminalEnvVar, "")
	t.Setenv("TERMINAL", "foot")

	got, ok := firstCandidate(t)
	if !ok {
		t.Fatal("expected a candidate")
	}
	if filepath.Base(got.bin) != "foot" {
		t.Errorf("$TERMINAL should win over list order, got %q", got.bin)
	}
	// It must still get foot's real argument form, not a guess.
	if len(got.args) != 1 || got.args[0] != "-e" {
		t.Errorf("expected foot's -e, got %v", got.args)
	}
}

// A known terminal reached through $TERMINAL must use its table entry, not -e.
func TestTerminalEnvUsesTableArgsForKnownTerminal(t *testing.T) {
	stubTerminals(t, "gnome-terminal")
	t.Setenv(TerminalEnvVar, "")
	t.Setenv("TERMINAL", "gnome-terminal")

	got, ok := firstCandidate(t)
	if !ok {
		t.Fatal("expected a candidate")
	}
	if len(got.args) != 1 || got.args[0] != "--" {
		t.Errorf("gnome-terminal needs --, got %v (-e is deprecated and parses differently)", got.args)
	}
}

// Preference order matters: a modern terminal should beat xterm.
func TestListPrefersModernOverXterm(t *testing.T) {
	stubTerminals(t, "xterm", "foot")
	t.Setenv(TerminalEnvVar, "")
	t.Setenv("TERMINAL", "")

	got, ok := firstCandidate(t)
	if !ok {
		t.Fatal("expected a candidate")
	}
	if filepath.Base(got.bin) != "foot" {
		t.Errorf("expected foot to be preferred over xterm, got %q", got.bin)
	}
}

// Every installed terminal should be offered, so a failed exec can fall through.
func TestAllInstalledTerminalsAreCandidates(t *testing.T) {
	stubTerminals(t, "kitty", "foot", "xterm")
	t.Setenv(TerminalEnvVar, "")
	t.Setenv("TERMINAL", "")

	got := candidatesToTry()
	if len(got) != 3 {
		t.Fatalf("expected all 3 installed terminals as candidates, got %d", len(got))
	}
}

// With nothing installed, the caller must be told rather than left guessing.
func TestNoTerminalsFound(t *testing.T) {
	stubTerminals(t) // empty dir on PATH
	t.Setenv(TerminalEnvVar, "")
	t.Setenv("TERMINAL", "")

	if got := candidatesToTry(); len(got) != 0 {
		t.Errorf("expected no candidates, got %v", got)
	}
}

// The argument form is not universal, and there is no safe default: `ghostty --`
// does nothing and `gnome-terminal -e` parses its argument differently. These
// were checked against real binaries where available.
func TestTerminalArgumentForms(t *testing.T) {
	want := map[string][]string{
		// verified by running each binary and confirming a marker file appeared
		"ghostty":        {"-e"},
		"kitty":          {"-e"},
		"foot":           {"-e"},
		"alacritty":      {"-e"},
		"konsole":        {"-e"},
		"xfce4-terminal": {"-x"},
		"urxvt":          {"-e"},
		// documented forms
		"wezterm":        {"start", "--"},
		"gnome-terminal": {"--"},
		"ptyxis":         {"--"},
		"kgx":            {"--"},
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

// The desktops users actually run must all be covered.
func TestCoversCommonDesktopDefaults(t *testing.T) {
	have := map[string]bool{}
	for _, c := range terminalCandidates {
		have[c.bin] = true
	}
	for desktop, bin := range map[string]string{
		"KDE":             "konsole",
		"GNOME (classic)": "gnome-terminal",
		"GNOME (Console)": "kgx",
		"Fedora 41+":      "ptyxis",
		"Hyprland":        "kitty",
		"Sway/niri":       "foot",
		"XFCE":            "xfce4-terminal",
		"MATE":            "mate-terminal",
		"fallback":        "xterm",
	} {
		if !have[bin] {
			t.Errorf("%s default terminal %q is not a candidate", desktop, bin)
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
	stubTerminals(t)
	t.Setenv(TerminalEnvVar, "")
	t.Setenv("TERMINAL", "")

	if err := RunFromLauncher(); err == nil {
		t.Error("expected an error when no terminal emulator exists")
	}
}
