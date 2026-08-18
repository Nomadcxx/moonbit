package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// LauncherFlag is the flag the desktop entry passes. It marks "started from a
// graphical launcher, with no terminal attached".
const LauncherFlag = "--launcher"

// terminalCandidate pairs a terminal emulator with the arguments that make it
// run a command. The flag is not universal: most take -e, but wezterm needs
// "start --", the GTK terminals need -x or --, and getting it wrong produces a
// window that opens and closes with no visible error.
type terminalCandidate struct {
	bin  string
	args []string
}

// Ordered by preference. Wayland-native and widely used first, then the older
// X11 terminals as a floor. This mirrors what rofi-sensible-terminal does,
// except moonbit does the search itself rather than trusting the launcher to.
var terminalCandidates = []terminalCandidate{
	{"ghostty", []string{"-e"}},
	{"kitty", []string{"-e"}},
	{"foot", []string{"-e"}},
	{"alacritty", []string{"-e"}},
	{"wezterm", []string{"start", "--"}},
	{"konsole", []string{"-e"}},
	{"gnome-terminal", []string{"--"}},
	{"xfce4-terminal", []string{"-x"}},
	{"mate-terminal", []string{"-x"}},
	{"tilix", []string{"-e"}},
	{"terminator", []string{"-x"}},
	{"lxterminal", []string{"-e"}},
	{"qterminal", []string{"-e"}},
	{"urxvt", []string{"-e"}},
	{"st", []string{"-e"}},
	{"xterm", []string{"-e"}},
}

// hasControllingTerminal reports whether this process can reach a terminal.
// A desktop launcher starts us with stdio pointed at the journal or /dev/null,
// and /dev/tty cannot be opened without a controlling terminal.
func hasControllingTerminal() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// findTerminal returns the first usable terminal emulator. $TERMINAL wins when
// it names something actually installed, so a user's choice is honoured.
func findTerminal() (terminalCandidate, bool) {
	if t := strings.TrimSpace(os.Getenv("TERMINAL")); t != "" {
		// Respect the user's setting, but only if it exists -- $TERMINAL is
		// frequently stale, naming a terminal that was uninstalled long ago.
		if path, err := exec.LookPath(t); err == nil {
			for _, c := range terminalCandidates {
				if c.bin == t {
					return terminalCandidate{bin: path, args: c.args}, true
				}
			}
			// Unknown terminal: -e is the overwhelmingly common convention.
			return terminalCandidate{bin: path, args: []string{"-e"}}, true
		}
	}

	for _, c := range terminalCandidates {
		if path, err := exec.LookPath(c.bin); err == nil {
			return terminalCandidate{bin: path, args: c.args}, true
		}
	}
	return terminalCandidate{}, false
}

// notifyDesktop surfaces a message to a user who has no terminal to read.
// Best-effort: without it the failure is invisible.
func notifyDesktop(urgency, summary, body string) {
	if path, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command(path, "-u", urgency, "-a", "moonbit", summary, body).Run()
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", summary, body)
}

// RunFromLauncher re-executes moonbit inside a terminal it locates itself.
//
// The desktop entry deliberately does NOT use pkexec. polkit's auth_admin
// requires an active session attached to a seat, and compositors that run as a
// systemd user service (niri, and anything started through uwsm) put their
// children under user@<uid>.service, which has no seat. polkit then refuses
// outright -- no password prompt, exit 127, and the terminal closes before the
// user can read anything. sudo has no such requirement, so moonbit opens a
// terminal and elevates there, where the password prompt is visible.
//
// The entry also does NOT use Terminal=true. That delegates finding a terminal
// to the launcher, and launchers routinely default to `xterm -e` whether or not
// xterm is installed.
func RunFromLauncher() error {
	// Started from a terminal after all: nothing to do, carry on normally.
	if hasControllingTerminal() {
		return nil
	}

	term, ok := findTerminal()
	if !ok {
		names := make([]string, 0, len(terminalCandidates))
		for _, c := range terminalCandidates {
			names = append(names, c.bin)
		}
		err := fmt.Errorf("no terminal emulator found (looked for $TERMINAL and: %s)",
			strings.Join(names, ", "))
		notifyDesktop("critical", "moonbit cannot start",
			"No terminal emulator was found. Install one (for example kitty, foot or "+
				"alacritty), or run 'sudo moonbit' from a terminal.")
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		notifyDesktop("critical", "moonbit cannot start",
			"Could not determine its own path: "+err.Error())
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	// Drop the launcher flag so the inner process runs normally and cannot
	// recurse back into here.
	inner := []string{exe}
	for _, a := range os.Args[1:] {
		if a != LauncherFlag {
			inner = append(inner, a)
		}
	}

	args := append(append([]string{}, term.args...), inner...)
	cmd := exec.Command(term.bin, args...)
	if err := cmd.Start(); err != nil {
		notifyDesktop("critical", "moonbit cannot start",
			fmt.Sprintf("Failed to open %s: %v", term.bin, err))
		return fmt.Errorf("failed to start terminal %s: %w", term.bin, err)
	}

	// Detach: the launcher should not wait on the terminal.
	return cmd.Process.Release()
}
