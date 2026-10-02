// Package oplock keeps moonbit's scans and cleans from overlapping across
// processes. The daemon, the timer services, the TUI and the desktop panel's
// sudo helper are separate programs that read and write the same root
// session cache, so an in-process semaphore alone cannot serialize them.
package oplock

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Path is the lock file. /run/moonbit is the daemon's runtime directory; the
// lock is created there by whichever moonbit runs first.
var Path = "/run/moonbit/op.lock"

// ErrBusy reports that another moonbit process holds the lock. Its text is
// the daemon's long-standing busy message, which panels already recognise.
var ErrBusy = errors.New("another operation in progress")

// TryAcquire takes the lock without waiting and returns its release. A
// process that cannot create the lock file, such as an unprivileged CLI run,
// proceeds unlocked: it cannot touch the root session cache either.
func TryAcquire() (func(), error) {
	_ = os.MkdirAll(filepath.Dir(Path), 0o755)
	f, err := os.OpenFile(Path, os.O_CREATE|os.O_RDWR|unix.O_CLOEXEC, 0o644)
	if err != nil {
		return func() {}, nil
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return func() {}, nil
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
