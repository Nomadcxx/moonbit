// Package units enables and disables moonbit's systemd units for both the
// TUI's Schedule screen and the desktop panel, so the two share one path and
// one diagnostic.
package units

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	Daemon     = "moonbit-daemon.service"
	ScanTimer  = "moonbit-scan.timer"
	CleanTimer = "moonbit-clean.timer"
)

// Timers are the scheduled scan and clean, which conflict with the daemon.
var Timers = []string{ScanTimer, CleanTimer}

// Installed reports whether systemd can see a unit at all. `systemctl cat`
// searches every unit path and exits non-zero when the unit does not exist.
func Installed(unit string) bool {
	return exec.Command("systemctl", "cat", unit).Run() == nil
}

// Apply runs `systemctl <action> --now` on units and returns systemd's own
// diagnostic on failure. exec.Cmd.Run discards stderr, which is how a missing
// unit file surfaced to users as an unactionable "exit status 1".
func Apply(action string, names ...string) error {
	var missing []string
	for _, u := range names {
		if !Installed(u) {
			missing = append(missing, u)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s not installed on this system.\n"+
			"Re-run the installer, or install the units manually:\n"+
			"  sudo install -m644 systemd/moonbit-*.service systemd/moonbit-*.timer /etc/systemd/system/\n"+
			"  sudo systemctl daemon-reload",
			strings.Join(missing, ", "))
	}

	args := append([]string{action, "--now"}, names...)
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%v: %s", err, msg)
		}
		return err
	}
	return nil
}
