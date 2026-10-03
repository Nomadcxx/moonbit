package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDurationRejectsNonPositive(t *testing.T) {
	for _, input := range []string{"0s", "-1s", "0d", "-1d"} {
		t.Run(input, func(t *testing.T) {
			_, err := parseDuration(input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be positive")
		})
	}
}

func TestPerformCleanUsesLiveClean(t *testing.T) {
	originalClean := daemonCleanSession
	originalScan := daemonScanSession
	originalState := daemonState
	originalOut := daemonOut
	originalMode := scanMode
	defer func() {
		daemonCleanSession = originalClean
		daemonScanSession = originalScan
		daemonState = originalState
		daemonOut = originalOut
		scanMode = originalMode
	}()

	daemonState = &DaemonState{StartTime: time.Now(), logger: (*audit.Logger)(nil)}
	daemonOut = io.Discard

	var gotDryRun bool
	var gotMode string
	daemonCleanSession = func(dryRun bool) error {
		gotDryRun = dryRun
		gotMode = scanMode
		return nil
	}

	performClean()

	assert.False(t, gotDryRun, "scheduled daemon clean should actually clean")
	assert.Equal(t, "quick", gotMode, "daemon clean must apply the timer's Selected/Low filter")
	assert.Equal(t, 1, daemonState.stats().CleanCount)
}

func TestSkippedCleanRunsAfterScanCompletes(t *testing.T) {
	originalClean := daemonCleanSession
	originalScan := daemonScanSession
	originalState := daemonState
	originalOut := daemonOut
	originalMode := scanMode
	defer func() {
		daemonCleanSession = originalClean
		daemonScanSession = originalScan
		daemonState = originalState
		daemonOut = originalOut
		scanMode = originalMode
		cleanPending.Store(false)
	}()

	daemonState = &DaemonState{StartTime: time.Now(), logger: (*audit.Logger)(nil)}
	daemonOut = io.Discard
	daemonScanSession = func() error { return nil }

	cleaned := make(chan struct{}, 1)
	daemonCleanSession = func(dryRun bool) error {
		cleaned <- struct{}{}
		return nil
	}

	select {
	case opSem <- struct{}{}:
	default:
		t.Fatal("opSem already occupied")
	}
	performClean()
	<-opSem

	performScan()

	select {
	case <-cleaned:
	case <-time.After(2 * time.Second):
		t.Fatal("skipped clean was not retried after the scan finished")
	}
	// The retry goroutine still holds opSem until performClean returns.
	// Wait for that before restoring package globals (scanMode, daemonState).
	select {
	case opSem <- struct{}{}:
		<-opSem
	case <-time.After(2 * time.Second):
		t.Fatal("retried clean did not release the op slot")
	}
}

func TestCheckTimerConflictsRejectsActiveMoonbitTimer(t *testing.T) {
	dir := t.TempDir()
	systemctl := filepath.Join(dir, "systemctl")
	script := "#!/bin/sh\n[ \"$3\" = moonbit-scan.timer ] && exit 0\nexit 3\n"
	require.NoError(t, os.WriteFile(systemctl, []byte(script), 0755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := checkTimerConflicts()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "moonbit-scan.timer")
}

func TestCheckTimerConflictsAllowsInactiveMoonbitTimers(t *testing.T) {
	dir := t.TempDir()
	systemctl := filepath.Join(dir, "systemctl")
	require.NoError(t, os.WriteFile(systemctl, []byte("#!/bin/sh\nexit 3\n"), 0755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	assert.NoError(t, checkTimerConflicts())
}

func TestShouldInitialScan(t *testing.T) {
	// No socket: the flag value wins, so the default scan stays on.
	assert.True(t, shouldInitialScan("", false, true))
	assert.False(t, shouldInitialScan("", false, false))

	// A panel socket skips the blocking startup scan unless the flag was set
	// explicitly, so the panel is usable the moment the daemon comes up.
	assert.False(t, shouldInitialScan("/run/moonbit/panel.sock", false, true),
		"panel socket must skip the startup scan by default")
	assert.True(t, shouldInitialScan("/run/moonbit/panel.sock", true, true),
		"--initial-scan=true must force the scan even with a socket")
	assert.False(t, shouldInitialScan("/run/moonbit/panel.sock", true, false))
}
