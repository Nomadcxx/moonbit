package oplock

import (
	"errors"
	"os"
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

func TestLockFileIsOwnerOnly(t *testing.T) {
	orig := Path
	dir := t.TempDir()
	Path = filepath.Join(dir, "run", "op.lock")
	defer func() { Path = orig }()

	release, err := TryAcquire()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	info, err := os.Stat(Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("lock mode = %04o, want 0600", got)
	}
}

func TestExistingWorldReadableLockIsTightened(t *testing.T) {
	orig := Path
	dir := t.TempDir()
	Path = filepath.Join(dir, "op.lock")
	defer func() { Path = orig }()

	if err := os.WriteFile(Path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	release, err := TryAcquire()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	info, err := os.Stat(Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("lock mode = %04o, want tightened 0600", got)
	}
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
