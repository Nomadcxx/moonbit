package oplock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSecondHolderIsBusyUntilRelease(t *testing.T) {
	orig := Path
	Path = filepath.Join(t.TempDir(), "run", "op.lock")
	defer func() { Path = orig }()

	release, err := TryAcquire()
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// flock is per open file description, so a second open in the same
	// process stands in for another moonbit process.
	if _, err := TryAcquire(); !errors.Is(err, ErrBusy) {
		t.Fatalf("second acquire = %v, want ErrBusy", err)
	}
	release()
	again, err := TryAcquire()
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	again()
}

func TestUnwritableLockProceedsUnlocked(t *testing.T) {
	orig := Path
	Path = "/proc/moonbit-cannot-exist/op.lock"
	defer func() { Path = orig }()
	release, err := TryAcquire()
	if err != nil || release == nil {
		t.Fatalf("unwritable lock: err = %v, release set = %t; want to proceed", err, release != nil)
	}
	release()
}
