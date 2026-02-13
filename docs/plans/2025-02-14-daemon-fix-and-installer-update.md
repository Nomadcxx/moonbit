# Daemon Fix & Installer/TUI Update Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the broken `moonbit daemon` command to work as a proper foreground daemon managed by systemd, create a systemd service file for it, update the TUI installer to support daemon mode, and update the TUI to manage daemon lifecycle.

**Architecture:** The daemon runs as a foreground process (no fork/setsid — Go doesn't support this safely due to goroutine scheduler constraints). Systemd manages it via a `Type=simple` service unit with `Restart=on-failure`. The daemon uses a `sync.Mutex` to protect shared state, a semaphore to prevent scan/clean overlap, and writes structured logs to a configurable file. The installer offers "daemon" as a third scheduling mode alongside "timer" and "manual". The TUI's ModeSchedule view is extended to show daemon status and offer start/stop controls.

**Tech Stack:** Go 1.21+, Cobra CLI, BubbleTea TUI, systemd, TOML config

**Existing Codebase Conventions:**
- CLI commands in `internal/cli/` using Cobra
- TUI in `internal/ui/ui.go` using BubbleTea
- Styles via `internal/cli/styles.go` (S.Bold, S.Success, etc.)
- Audit logging via `internal/audit/` package
- Installer TUI in `cmd/installer/main.go`
- Systemd files in `systemd/`
- Makefile for build/install targets

---

## Phase 1: Fix daemon.go Core Bugs (Critical)

### Task 1.1: Fix Duration Parsing (Blocks Everything)

**Problem:** Go's `time.ParseDuration` doesn't support `d` (days) suffix. The `--clean` flag documents `7d` as valid but it will crash at runtime.

**Files:**
- Modify: `internal/cli/daemon.go` — custom duration parser
- Create: `internal/cli/daemon_test.go` — test the parser

**Step 1: Write the failing test**

Create `internal/cli/daemon_test.go`:

```go
package cli

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"standard hours", "1h", time.Hour, false},
		{"standard minutes", "30m", 30 * time.Minute, false},
		{"standard seconds", "10s", 10 * time.Second, false},
		{"days suffix", "7d", 7 * 24 * time.Hour, false},
		{"fractional days", "1.5d", 36 * time.Hour, false},
		{"mixed case days", "2d", 48 * time.Hour, false},
		{"zero days", "0d", 0, false},
		{"empty string", "", 0, true},
		{"invalid", "abc", 0, true},
		{"negative days", "-1d", -24 * time.Hour, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/nomadx/Documents/moonbit && go test ./internal/cli/ -run TestParseDuration -v`
Expected: FAIL — `parseDuration` undefined

**Step 3: Implement parseDuration**

In `internal/cli/daemon.go`, add this function (near the top, after imports):

```go
// parseDuration extends time.ParseDuration with support for "d" (days) suffix.
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}

	// Check if the string ends with 'd' (days)
	if strings.HasSuffix(s, "d") {
		prefix := strings.TrimSuffix(s, "d")
		days, err := strconv.ParseFloat(prefix, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}

	// Fall back to standard Go duration parsing
	return time.ParseDuration(s)
}
```

Add `"strconv"` and `"strings"` to the imports.

**Step 4: Update daemonCmd.RunE to use parseDuration**

Replace the two `time.ParseDuration` calls in the daemon command's Run/RunE function:

```go
// Replace:
scanInterval, err := time.ParseDuration(daemonScanInterval)
// With:
scanInterval, err := parseDuration(daemonScanInterval)

// Replace:
cleanInterval, err := time.ParseDuration(daemonCleanInterval)
// With:
cleanInterval, err := parseDuration(daemonCleanInterval)
```

**Step 5: Run test to verify it passes**

Run: `cd /home/nomadx/Documents/moonbit && go test ./internal/cli/ -run TestParseDuration -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/cli/daemon.go internal/cli/daemon_test.go
git commit -m "fix(daemon): support 'd' suffix in duration parsing"
```

---

### Task 1.2: Fix Race Conditions with Mutex

**Problem:** `performScan()` and `performClean()` run in goroutines and mutate `DaemonState` fields concurrently without synchronization.

**Files:**
- Modify: `internal/cli/daemon.go` — add mutex to DaemonState

**Step 1: Write the failing test (race detector)**

Add to `internal/cli/daemon_test.go`:

```go
func TestDaemonStateConcurrency(t *testing.T) {
	// This test validates that DaemonState is safe for concurrent access.
	// Run with -race flag to catch data races.
	state := &DaemonState{
		StartTime: time.Now(),
	}

	done := make(chan struct{})

	// Simulate concurrent scan updates
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			state.IncrementScanCount()
			state.SetLastScanTime(time.Now())
		}
	}()

	// Simulate concurrent clean updates
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			state.IncrementCleanCount()
			state.SetLastCleanTime(time.Now())
		}
	}()

	<-done
	<-done

	if state.GetScanCount() != 100 {
		t.Errorf("expected 100 scans, got %d", state.GetScanCount())
	}
	if state.GetCleanCount() != 100 {
		t.Errorf("expected 100 cleans, got %d", state.GetCleanCount())
	}
}
```

**Step 2: Run with race detector to see failure**

Run: `cd /home/nomadx/Documents/moonbit && go test -race ./internal/cli/ -run TestDaemonStateConcurrency -v`
Expected: FAIL — data race detected (or compile error for missing methods)

**Step 3: Add mutex and thread-safe methods to DaemonState**

In `internal/cli/daemon.go`, update the `DaemonState` struct:

```go
type DaemonState struct {
	mu            sync.Mutex
	StartTime     time.Time
	LastScanTime  time.Time
	LastCleanTime time.Time
	ScanCount     int
	CleanCount    int
	FilesCleaned  int
	SpaceFreed    int64
	logger        *audit.Logger
}

func (ds *DaemonState) IncrementScanCount() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.ScanCount++
}

func (ds *DaemonState) SetLastScanTime(t time.Time) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.LastScanTime = t
}

func (ds *DaemonState) GetScanCount() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.ScanCount
}

func (ds *DaemonState) IncrementCleanCount() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.CleanCount++
}

func (ds *DaemonState) SetLastCleanTime(t time.Time) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.LastCleanTime = t
}

func (ds *DaemonState) GetCleanCount() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.CleanCount
}

func (ds *DaemonState) AddFilesCleaned(n int) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.FilesCleaned += n
}

func (ds *DaemonState) AddSpaceFreed(n int64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.SpaceFreed += n
}
```

Add `"sync"` to imports.

**Step 4: Update performScan() and performClean() to use thread-safe methods**

Replace direct field access (`state.ScanCount++`, `state.LastScanTime = time.Now()`, etc.) with method calls (`state.IncrementScanCount()`, `state.SetLastScanTime(time.Now())`, etc.).

**Step 5: Run test with race detector**

Run: `cd /home/nomadx/Documents/moonbit && go test -race ./internal/cli/ -run TestDaemonStateConcurrency -v`
Expected: PASS (no data races)

**Step 6: Commit**

```bash
git add internal/cli/daemon.go internal/cli/daemon_test.go
git commit -m "fix(daemon): add mutex to prevent race conditions in DaemonState"
```

---

### Task 1.3: Prevent Scan/Clean Overlap

**Problem:** Scan and clean can run simultaneously since both are dispatched as goroutines. Concurrent filesystem operations may conflict.

**Files:**
- Modify: `internal/cli/daemon.go` — add operation semaphore

**Step 1: Write the test**

Add to `internal/cli/daemon_test.go`:

```go
func TestOperationSemaphore(t *testing.T) {
	sem := make(chan struct{}, 1)
	
	// First operation should acquire the semaphore
	select {
	case sem <- struct{}{}:
		// ok - acquired
	default:
		t.Fatal("should have acquired semaphore")
	}

	// Second operation should fail to acquire
	select {
	case sem <- struct{}{}:
		t.Fatal("should NOT have acquired semaphore - operation overlap!")
	default:
		// ok - blocked as expected
	}

	// Release first operation
	<-sem

	// Now should be acquirable again
	select {
	case sem <- struct{}{}:
		// ok
		<-sem
	default:
		t.Fatal("should have acquired semaphore after release")
	}
}
```

**Step 2: Run test**

Run: `cd /home/nomadx/Documents/moonbit && go test ./internal/cli/ -run TestOperationSemaphore -v`
Expected: PASS (this tests the pattern, not the integration)

**Step 3: Add semaphore to daemon command**

In `internal/cli/daemon.go`, in the `daemonCmd.RunE` function, before the ticker loop:

```go
// Operation semaphore - prevents scan and clean from running simultaneously
opSem := make(chan struct{}, 1)
```

Update `performScan` and `performClean` signatures to accept the semaphore:

```go
func performScan(state *DaemonState, sem chan struct{}) {
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	default:
		fmt.Println(S.Warning("Skipping scan — another operation in progress"))
		return
	}
	// ... existing scan logic ...
}

func performClean(state *DaemonState, sem chan struct{}) {
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	default:
		fmt.Println(S.Warning("Skipping clean — another operation in progress"))
		return
	}
	// ... existing clean logic ...
}
```

Update all call sites to pass `sem`.

**Step 4: Run all daemon tests**

Run: `cd /home/nomadx/Documents/moonbit && go test -race ./internal/cli/ -run TestDaemon -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/daemon.go internal/cli/daemon_test.go
git commit -m "fix(daemon): add semaphore to prevent scan/clean overlap"
```

---

### Task 1.4: Fix Logging (--log flag)

**Problem:** `daemonLogFile` flag is parsed but never used. Daemon should write logs to the specified file.

**Files:**
- Modify: `internal/cli/daemon.go` — create file logger, use it

**Step 1: Implement file logging**

In `daemonCmd.RunE`, after PID file writing, add log file setup:

```go
// Set up file logging
var logFile *os.File
if daemonLogFile != "" {
	logDir := filepath.Dir(daemonLogFile)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory %s: %w", logDir, err)
	}
	logFile, err = os.OpenFile(daemonLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", daemonLogFile, err)
	}
	defer logFile.Close()

	// Redirect daemon output to log file when not attached to a terminal
	log.SetOutput(logFile)
}
```

Add `"log"` to imports.

Replace `fmt.Println` / `fmt.Printf` calls in the daemon loop and performScan/performClean with `log.Println` / `log.Printf` so they go to the log file when configured.

Keep the styled output (S.Bold, S.Success, etc.) for the initial startup messages that appear before the loop starts (when user is watching), but switch to `log.Println` for operational messages inside the loop.

**Step 2: Run daemon tests**

Run: `cd /home/nomadx/Documents/moonbit && go test ./internal/cli/ -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/cli/daemon.go
git commit -m "fix(daemon): implement --log flag for file logging"
```

---

### Task 1.5: Handle Stale PID File on Startup

**Problem:** If daemon crashes, stale PID file prevents restart.

**Files:**
- Modify: `internal/cli/daemon.go` — check/clean PID on startup

**Step 1: Write the test**

Add to `internal/cli/daemon_test.go`:

```go
func TestCleanStalePidFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write a PID that definitely doesn't exist
	err := os.WriteFile(pidFile, []byte("999999999"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Should clean up stale PID
	err = cleanStalePidFile(pidFile)
	if err != nil {
		t.Errorf("cleanStalePidFile() error = %v", err)
	}

	// PID file should be removed
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("stale PID file should have been removed")
	}
}

func TestCleanStalePidFile_ActiveProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "test.pid")

	// Write our own PID (which is definitely running)
	err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Should return error — process is still running
	err = cleanStalePidFile(pidFile)
	if err == nil {
		t.Error("expected error for active process PID file")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/nomadx/Documents/moonbit && go test ./internal/cli/ -run TestCleanStalePid -v`
Expected: FAIL — `cleanStalePidFile` undefined

**Step 3: Implement cleanStalePidFile**

```go
// cleanStalePidFile checks if a PID file references a running process.
// If the process is dead, the stale PID file is removed.
// If the process is alive, returns an error (daemon already running).
// If no PID file exists, returns nil.
func cleanStalePidFile(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		// Corrupt PID file — remove it
		os.Remove(path)
		return nil
	}

	// Check if process is running
	process, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(path)
		return nil
	}

	// On Unix, FindProcess always succeeds. Signal 0 checks if process exists.
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		// Process doesn't exist — stale PID file
		os.Remove(path)
		return nil
	}

	return fmt.Errorf("daemon already running with PID %d", pid)
}
```

**Step 4: Call it at daemon startup**

In `daemonCmd.RunE`, before `writePidFile`, add:

```go
if err := cleanStalePidFile(daemonPidFile); err != nil {
	return err
}
```

**Step 5: Run tests**

Run: `cd /home/nomadx/Documents/moonbit && go test ./internal/cli/ -run TestCleanStalePid -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/cli/daemon.go internal/cli/daemon_test.go
git commit -m "fix(daemon): handle stale PID file on startup"
```

---

### Task 1.6: Add Graceful Shutdown with Statistics

**Problem:** Signal handler exits without logging final statistics.

**Files:**
- Modify: `internal/cli/daemon.go` — enhance signal handler

**Step 1: Update signal handler**

In the signal handler case of the select loop:

```go
case sig := <-sigChan:
	log.Printf("Received signal %v, shutting down...", sig)
	log.Printf("Daemon statistics — uptime: %s, scans: %d, cleans: %d, files cleaned: %d, space freed: %s",
		time.Since(daemonState.StartTime).Round(time.Second),
		daemonState.GetScanCount(),
		daemonState.GetCleanCount(),
		daemonState.GetFilesCleaned(),
		utils.HumanizeBytes(daemonState.GetSpaceFreed()),
	)

	// Clean up PID file
	os.Remove(daemonPidFile)

	// Flush audit log
	if daemonState.logger != nil {
		daemonState.logger.Close()
	}

	return nil
```

Add `GetFilesCleaned()` and `GetSpaceFreed()` methods to DaemonState (following the same mutex pattern from Task 1.2).

**Step 2: Run all tests**

Run: `cd /home/nomadx/Documents/moonbit && go test ./internal/cli/ -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/cli/daemon.go
git commit -m "fix(daemon): log statistics on graceful shutdown"
```

---

## Phase 2: Systemd Daemon Service

### Task 2.1: Create Daemon Systemd Service File

**Files:**
- Create: `systemd/moonbit-daemon.service`

**Step 1: Create the service file**

```ini
[Unit]
Description=MoonBit System Cleaner Daemon
Documentation=https://github.com/user/moonbit
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/moonbit daemon --log /var/log/moonbit/daemon.log --pid /var/run/moonbit.pid
ExecStop=/bin/kill -SIGTERM $MAINPID
Restart=on-failure
RestartSec=30

# Security hardening (matching existing service files)
PrivateTmp=true
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/var/cache/moonbit /var/cache /var/tmp /var/log/moonbit /var/run /root/.cache/moonbit /root/.local/share

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=moonbit-daemon

[Install]
WantedBy=multi-user.target
```

**Step 2: Verify file is syntactically valid**

Run: `systemd-analyze verify /home/nomadx/Documents/moonbit/systemd/moonbit-daemon.service 2>&1 || echo "Verify done"`

Note: Some warnings are expected since paths may not exist in dev environment.

**Step 3: Commit**

```bash
git add systemd/moonbit-daemon.service
git commit -m "feat(systemd): add daemon service file for Type=simple daemon mode"
```

---

### Task 2.2: Fix Duplicate OnCalendar in Scan Timer

**Problem:** `moonbit-scan.timer` has both `OnCalendar=daily` AND `OnCalendar=*-*-* 02:00:00`. These are redundant/conflicting.

**Files:**
- Modify: `systemd/moonbit-scan.timer`

**Step 1: Remove the duplicate**

Keep only the explicit time `*-*-* 02:00:00` (more precise), remove `OnCalendar=daily`.

**Step 2: Commit**

```bash
git add systemd/moonbit-scan.timer
git commit -m "fix(systemd): remove duplicate OnCalendar in scan timer"
```

---

### Task 2.3: Update Makefile

**Files:**
- Modify: `Makefile` — add daemon install/uninstall targets

**Step 1: Add daemon targets**

Add after the existing `install-systemd` target:

```makefile
.PHONY: install-daemon
install-daemon: build ## Install moonbit with daemon mode (systemd)
	sudo cp $(BINARY_NAME) /usr/local/bin/
	sudo mkdir -p /var/log/moonbit
	sudo cp systemd/moonbit-daemon.service /etc/systemd/system/
	sudo systemctl daemon-reload
	sudo systemctl enable --now moonbit-daemon.service
	@echo "moonbit daemon installed and started"

.PHONY: uninstall-daemon
uninstall-daemon: ## Uninstall moonbit daemon
	-sudo systemctl disable --now moonbit-daemon.service
	-sudo rm -f /etc/systemd/system/moonbit-daemon.service
	sudo systemctl daemon-reload
	@echo "moonbit daemon removed"
```

**Step 2: Update help comments if applicable**

**Step 3: Commit**

```bash
git add Makefile
git commit -m "feat(makefile): add install-daemon and uninstall-daemon targets"
```

---

### Task 2.4: Update systemd/README.md

**Files:**
- Modify: `systemd/README.md` — document daemon mode alongside timer mode

**Step 1: Add daemon mode section**

Add a new section explaining:
- Two operational modes: **Timer** (periodic oneshot, existing) vs **Daemon** (long-running, new)
- When to use each (daemon for systems that want continuous monitoring, timers for minimal footprint)
- Daemon mode is mutually exclusive with timers — don't enable both
- Installation: `make install-daemon` or via TUI installer
- Monitoring: `systemctl status moonbit-daemon` or `moonbit daemon status`

**Step 2: Commit**

```bash
git add systemd/README.md
git commit -m "docs(systemd): document daemon mode alongside timer mode"
```

---

## Phase 3: Update TUI Installer

### Task 3.1: Add Daemon Mode to Installer Schedule Selection

**Problem:** Installer only offers daily/weekly/manual. Need to add "daemon" option.

**Files:**
- Modify: `cmd/installer/main.go`

**Step 1: Update schedule options**

Find the `scheduleOptions` array (or equivalent — the slice of schedule choices in the installer). Add a fourth option:

```go
{title: "Daemon Mode", desc: "Long-running background service (systemd)"},
```

The existing options are roughly:
- Daily (scan+clean timers)
- Weekly (clean timer only)  
- Manual (no automation)

Add Daemon as the first or second option.

**Step 2: Update the schedule selection enum/index handling**

The installer uses `selectedSchedule int` to track which option is selected. Update all switch statements that check this value to handle the new daemon case.

**Step 3: Implement installDaemon() function**

```go
func installDaemon() error {
	// Copy daemon service file
	src := "systemd/moonbit-daemon.service"
	dst := "/etc/systemd/system/moonbit-daemon.service"
	input, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read daemon service file: %w", err)
	}
	if err := os.WriteFile(dst, input, 0644); err != nil {
		return fmt.Errorf("failed to install daemon service: %w", err)
	}
	return nil
}
```

**Step 4: Implement enableDaemon() function**

```go
func enableDaemon() error {
	// Create log directory
	if err := os.MkdirAll("/var/log/moonbit", 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", "moonbit-daemon.service"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to run %v: %s: %w", args, output, err)
		}
	}
	return nil
}
```

**Step 5: Update installSystemd() to handle daemon vs timer mode**

When daemon mode is selected:
- Do NOT install timer/oneshot files
- DO install daemon service file
- The modes should be mutually exclusive

When timer mode is selected:
- Existing behavior (install timers/oneshot services)
- Disable daemon service if it exists

**Step 6: Update configureSchedule() to actually work**

`configureSchedule()` currently returns nil (no-op). Wire it up to call the appropriate enable function based on selected schedule:

```go
func configureSchedule() error {
	switch selectedSchedule {
	case 0: // Daily timers
		return enableTimers()
	case 1: // Weekly timers
		return enableTimers() // Different timer config
	case 2: // Daemon
		return enableDaemon()
	case 3: // Manual
		return nil // No automation
	}
	return nil
}
```

**Step 7: Update uninstall to handle daemon**

The `disableService()` function should also disable the daemon service if present:

```go
// Add to disableService():
exec.Command("systemctl", "disable", "--now", "moonbit-daemon.service").Run()
exec.Command("rm", "-f", "/etc/systemd/system/moonbit-daemon.service").Run()
```

**Step 8: Run build to verify**

Run: `cd /home/nomadx/Documents/moonbit && go build ./cmd/installer/`
Expected: Build succeeds

**Step 9: Commit**

```bash
git add cmd/installer/main.go
git commit -m "feat(installer): add daemon mode to TUI installer schedule options"
```

---

## Phase 4: Update TUI Schedule View

### Task 4.1: Add Daemon Status to TUI ModeSchedule

**Problem:** TUI ModeSchedule only checks/manages systemd timers. It should also show daemon status and offer controls.

**Files:**
- Modify: `internal/ui/ui.go`

**Step 1: Add daemon status check**

In `checkTimerStatus()` (or equivalent function that checks systemctl is-enabled/is-active), add a check for `moonbit-daemon.service`:

```go
// Check daemon service status
daemonEnabled := false
daemonActive := false

cmd := exec.Command("systemctl", "is-enabled", "moonbit-daemon.service")
if err := cmd.Run(); err == nil {
    daemonEnabled = true
}

cmd = exec.Command("systemctl", "is-active", "moonbit-daemon.service")
if err := cmd.Run(); err == nil {
    daemonActive = true
}
```

Add `daemonEnabled` and `daemonActive` bool fields to the model.

**Step 2: Update renderSchedule() to show daemon status**

Add a section to the schedule view showing:
- Whether daemon service is enabled/disabled
- Whether daemon service is active/inactive
- A toggle option (similar to existing timer toggles)

Follow the existing styling conventions using the S.* style helpers.

**Step 3: Add daemon toggle handling**

In the key handler for ModeSchedule, add a keybinding to enable/disable the daemon service. Follow the existing pattern used for timer toggling (executeTimerCommand/runTimerCommand).

When enabling daemon mode, disable timers. When enabling timer mode, disable daemon. Make them mutually exclusive in the UI.

**Step 4: Run build**

Run: `cd /home/nomadx/Documents/moonbit && go build ./...`
Expected: Build succeeds

**Step 5: Commit**

```bash
git add internal/ui/ui.go
git commit -m "feat(tui): add daemon status display and toggle to schedule view"
```

---

## Phase 5: Final Verification

### Task 5.1: Run All Tests

Run: `cd /home/nomadx/Documents/moonbit && go test -race ./...`
Expected: All tests pass (note any pre-existing failures)

### Task 5.2: Run Linter

Run: `cd /home/nomadx/Documents/moonbit && golangci-lint run ./...`
Expected: No new warnings from changed files

### Task 5.3: Build All Binaries

Run: `cd /home/nomadx/Documents/moonbit && make build && go build ./cmd/installer/`
Expected: Both succeed

### Task 5.4: Verify Daemon Starts

Run: `cd /home/nomadx/Documents/moonbit && sudo ./moonbit daemon --scan 5s --clean 10s 2>&1 &; sleep 3; cat /var/run/moonbit.pid; kill %1`
Expected: Daemon starts, PID file written, shuts down cleanly on signal

### Task 5.5: Final Commit

```bash
git add -A
git commit -m "chore: final verification after daemon fix and installer update"
```

---

## Summary of All Changes

| File | Action | Phase |
|------|--------|-------|
| `internal/cli/daemon.go` | Major fix: duration parsing, mutex, semaphore, logging, PID, shutdown | 1 |
| `internal/cli/daemon_test.go` | Create: tests for all daemon fixes | 1 |
| `systemd/moonbit-daemon.service` | Create: Type=simple systemd service | 2 |
| `systemd/moonbit-scan.timer` | Fix: remove duplicate OnCalendar | 2 |
| `systemd/README.md` | Update: document daemon mode | 2 |
| `Makefile` | Add: install-daemon, uninstall-daemon targets | 2 |
| `cmd/installer/main.go` | Update: add daemon mode to schedule selection | 3 |
| `internal/ui/ui.go` | Update: add daemon status to ModeSchedule | 4 |

**Estimated effort:** ~3-4 hours for a developer familiar with Go/systemd. Each phase is independently committable and testable.

---

## ERRATA / Self-Review Corrections

These corrections override the task descriptions above. The implementing agent MUST apply these:

### E1: `Run:` → `RunE:` Conversion (Affects Tasks 1.1-1.6)

The plan references `RunE` but daemon.go actually uses `Run:` (not `RunE:`). This means `return err` and `return nil` patterns won't work. **Before any other daemon changes**, convert both commands:

```go
// daemonCmd (line ~51): Change Run: to RunE:
// daemonStatusCmd (line ~156): Change Run: to RunE:
```

This is a prerequisite for Tasks 1.1-1.6. Do it first.

### E2: performScan/performClean Signatures (Affects Task 1.3)

The plan adds `(state *DaemonState, sem chan struct{})` params, but the actual functions at lines 185 and 223 take NO arguments — they use package-level `daemonState`. Two valid approaches:

**Option A (recommended):** Keep package-level state, add a package-level `opSem` channel. Less churn.
**Option B:** Convert to method receivers on `DaemonState`. More idiomatic Go but bigger refactor.

Pick A unless there's good reason for B.

### E3: Installer scheduleIndex Bounds (Affects Task 3.1)

The installer uses `scheduleIndex int` with a slice `schedules := []string{"daily", "weekly", "manual"}`. The bound check at approximately line 136 uses `< 2` (should probably be `< len(schedules)-1`). When adding daemon mode:
1. Add "daemon" to the schedules slice
2. Update the bound check to `< len(schedules)-1` (generic) instead of hardcoded index
3. Update the conditional logic in `installSystemd()` and `enableService()` to branch on "daemon" mode

### E4: Mutual Exclusivity Enforcement (Cross-cutting concern)

The plan mentions daemon and timer modes should be mutually exclusive, but doesn't spell out WHERE to enforce this. Enforcement points:

1. **Installer**: When daemon selected, skip timer file installation. When timers selected, don't install daemon service. ✅ Task 3.1 covers this.
2. **TUI**: When toggling daemon ON, disable timers. When toggling timers ON, disable daemon. Task 4.1 must handle this explicitly.
3. **CLI**: `moonbit daemon` should warn/fail if timers are active. Add a check at daemon startup:
   ```go
   // Check if timers are enabled (conflicting mode)
   cmd := exec.Command("systemctl", "is-enabled", "moonbit-scan.timer")
   if cmd.Run() == nil {
       return fmt.Errorf("moonbit timers are active — disable them first with 'systemctl disable --now moonbit-scan.timer moonbit-clean.timer' or use the TUI schedule manager")
   }
   ```
   **This is a NEW task not in the plan.** Add it to Phase 1 as Task 1.7.

### E5: PID File Directory Creation (Affects Task 1.5)

`writePidFile` writes to `/var/run/moonbit.pid` but `/var/run/` may require root. The daemon already checks for root, so this works, but `cleanStalePidFile` and `writePidFile` should handle the case where the directory doesn't exist:

```go
os.MkdirAll(filepath.Dir(path), 0755)
```
