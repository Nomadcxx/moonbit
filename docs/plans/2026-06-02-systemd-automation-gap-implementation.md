# Systemd Automation Gap Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the remaining reliability gaps that prevent MoonBit from running correctly as a scheduled system cleaner.

**Architecture:** Introduce a small shared path helper, wire it into config/session/audit/backup code, then layer observable systemd/daemon behavior on top. Keep UI and Docker changes small and state-machine local.

**Tech Stack:** Go, Cobra, Bubble Tea, systemd unit files, standard library tests.

---

### Task 1: HOME-less runtime paths

**Files:**
- Create: `internal/paths/paths.go`
- Create: `internal/paths/paths_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/session/cache.go`
- Modify: `internal/audit/logger.go`
- Modify: `internal/cleaner/cleaner.go`

**Steps:**
1. Write tests for root/systemd fallback path resolution when `HOME` is absent.
2. Implement `paths.HomeDir`, `ConfigFile`, `CacheFile`, `DataDir`.
3. Replace direct `os.UserHomeDir` use in config/session/audit/backup paths.
4. Run targeted package tests.

### Task 2: systemd observability and unit correctness

**Files:**
- Modify: `systemd/moonbit-scan.service`
- Modify: `systemd/moonbit-clean.service`
- Modify: `systemd/moonbit-daemon.service`
- Modify: `internal/cli/root.go`

**Steps:**
1. Add logging summaries for scan/clean config and cache paths.
2. Add explicit `HOME`, `XDG_*`, and writable directories to units.
3. Run `go test ./internal/cli` and build checks.

### Task 3: daemon correctness

**Files:**
- Modify: `internal/cli/daemon.go`
- Add/modify: `internal/cli/daemon_test.go`

**Steps:**
1. Test zero/negative interval rejection.
2. Test scheduled clean calls live clean semantics through an injectable function.
3. Implement minimal injection seam and fixes.

### Task 4: cleaner and restore failure semantics

**Files:**
- Modify: `internal/cleaner/cleaner.go`
- Modify: `internal/cleaner/cleaner_test.go`

**Steps:**
1. Add tests for partial delete failure returning an error.
2. Add tests for restore missing backup files returning an error.
3. Preserve complete progress messages while returning failure to callers.

### Task 5: TUI safety gaps

**Files:**
- Modify: `internal/ui/ui.go`
- Modify: `internal/ui/ui_test.go`

**Steps:**
1. Test `NewModel` loads config through `config.Load`.
2. Test Docker menu enters confirmation instead of pruning immediately.
3. Implement confirmation state with minimal UI changes.

### Task 6: verification and feature gap pass

**Commands:**
- `gofmt -w ...`
- `go test ./...`
- `go test ./... -race`
- `go vet ./...`
- `go build -o /tmp/moonbit-audit-build ./cmd`
- `go build -o /tmp/moonbit-installer-audit-build ./cmd/installer`

**Output:** Summarize fixes, systemd dogfooding evidence, and a deep-clean feature gap analysis.
