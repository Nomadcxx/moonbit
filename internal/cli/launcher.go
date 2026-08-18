package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// LauncherFlag is the flag the desktop entry passes. It marks "started from a
// graphical launcher, with no terminal attached".
const LauncherFlag = "--launcher"

// TerminalEnvVar lets a user name the terminal moonbit should open when the
// built-in list gets it wrong. This is the escape hatch: the argument form is
// not uniform across terminals, and no list can cover every desktop.
//
//	MOONBIT_TERMINAL="myterm --exec-flag"
const TerminalEnvVar = "MOONBIT_TERMINAL"

// terminalCandidate pairs a terminal emulator with the arguments that make it
// run a command. The flag is not universal and there is no safe default:
// `ghostty --` does nothing, `gnome-terminal -e` is deprecated and parses its
// argument differently. Getting it wrong opens a window that closes instantly
// with no error, which is precisely the failure this file exists to prevent.
type terminalCandidate struct {
	bin  string
	args []string
}

// Ordered by how likely a given desktop is to have it, not alphabetically.
//
// Entries marked "verified" were tested by running
//
//	<bin> <args> /bin/sh -c 'echo OK > marker'
//
// and confirming the marker appeared. The GNOME family was checked in a
// container with a session bus, since those terminals are D-Bus activated and
// do nothing without one. The rest use each project's documented invocation.
var terminalCandidates = []terminalCandidate{
	{"ghostty", []string{"-e"}},   // verified
	{"kitty", []string{"-e"}},     // verified; Hyprland's usual default
	{"foot", []string{"-e"}},      // verified; common on Sway/niri
	{"alacritty", []string{"-e"}}, // verified
	{"konsole", []string{"-e"}},   // verified; KDE default
	{"wezterm", []string{"start", "--"}},
	{"ptyxis", []string{"--"}},         // verified; Fedora 41+ default
	{"kgx", []string{"--"}},            // verified; GNOME Console
	{"gnome-console", []string{"--"}},  // kgx under its other name
	{"gnome-terminal", []string{"--"}}, // verified; -e is deprecated and parses a single string
	{"cosmic-term", []string{"-e"}},
	{"xfce4-terminal", []string{"-x"}}, // verified
	{"mate-terminal", []string{"-x"}},
	{"tilix", []string{"-e"}},
	{"terminator", []string{"-x"}},
	{"blackbox", []string{"-c"}},
	{"deepin-terminal", []string{"-e"}},
	{"lxterminal", []string{"-e"}},
	{"qterminal", []string{"-e"}},
	{"urxvt", []string{"-e"}}, // verified
	{"st", []string{"-e"}},
	{"xterm", []string{"-e"}}, // verified
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

// candidatesToTry builds the ordered list of terminals to attempt.
//
// MOONBIT_TERMINAL wins outright. $TERMINAL comes next but only when it names
// something installed: it is very often stale, left pointing at a terminal that
// was removed, and trusting it blindly means opening nothing at all.
func candidatesToTry() []terminalCandidate {
	var out []terminalCandidate

	if raw := strings.TrimSpace(os.Getenv(TerminalEnvVar)); raw != "" {
		fields := strings.Fields(raw)
		if path, err := exec.LookPath(fields[0]); err == nil {
			args := fields[1:]
			if len(args) == 0 {
				args = []string{"-e"}
			}
			out = append(out, terminalCandidate{bin: path, args: args})
		}
	}

	if t := strings.TrimSpace(os.Getenv("TERMINAL")); t != "" {
		if path, err := exec.LookPath(t); err == nil {
			args := []string{"-e"} // widest convention for an unknown terminal
			for _, c := range terminalCandidates {
				if c.bin == t {
					args = c.args
					break
				}
			}
			out = append(out, terminalCandidate{bin: path, args: args})
		}
	}

	for _, c := range terminalCandidates {
		if path, err := exec.LookPath(c.bin); err == nil {
			out = append(out, terminalCandidate{bin: path, args: c.args})
		}
	}
	return out
}

// notifyDesktop surfaces a message to a user who has no terminal to read.
// Best-effort: without it the failure is invisible.
func notifyDesktop(urgency, summary, body string) {
	if path, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command(path, "-u", urgency, "-a", "moonbit", summary, body).Run()
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", summary, body)
}

// RunFromLauncher replaces this process with a terminal running moonbit.
//
// It uses syscall.Exec rather than spawning and exiting. GNOME and KDE place a
// launched application in a transient systemd scope; if moonbit forked a
// terminal and then exited, systemd could tear the scope down and take the
// terminal with it. Replacing the process image means the scope tracks the
// terminal itself, so it lives exactly as long as the user keeps it open.
//
// The desktop entry deliberately does NOT use pkexec. polkit's auth_admin
// requires an active session attached to a seat, and compositors that run as a
// systemd user service (niri, and anything started through uwsm) put their
// children under user@<uid>.service, which has no seat. polkit then refuses
// outright, with no password prompt and no readable error. sudo has no such
// requirement, so moonbit elevates inside the terminal where the prompt shows.
//
// It also does NOT use Terminal=true, which hands terminal selection to the
// launcher; launchers commonly default to `xterm -e` whether or not xterm is
// installed.
//
// On success this function does not return.
func RunFromLauncher() error {
	// Started from a terminal after all: nothing to do, carry on normally.
	if hasControllingTerminal() {
		return nil
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

	candidates := candidatesToTry()
	if len(candidates) == 0 {
		notifyDesktop("critical", "moonbit cannot start",
			"No terminal emulator was found. Install one (kitty, foot, alacritty, "+
				"konsole or gnome-terminal), or set MOONBIT_TERMINAL, or run "+
				"'sudo moonbit' from a terminal.")
		return fmt.Errorf("no terminal emulator found")
	}

	// syscall.Exec only returns on failure, so a binary that vanished between
	// LookPath and here just moves us to the next candidate.
	var lastErr error
	for _, term := range candidates {
		argv := append([]string{term.bin}, append(append([]string{}, term.args...), inner...)...)
		lastErr = syscall.Exec(term.bin, argv, os.Environ())
	}

	notifyDesktop("critical", "moonbit cannot start",
		fmt.Sprintf("Could not open a terminal (last error: %v). "+
			"Set MOONBIT_TERMINAL to your terminal, or run 'sudo moonbit'.", lastErr))
	return fmt.Errorf("failed to exec any terminal, last error: %w", lastErr)
}
