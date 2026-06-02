# Systemd Automation Gap Design

**Goal:** Make MoonBit reliable enough to dogfood through systemd timers and daemon mode, then use the resulting logs to guide deeper cleanup features.

**Architecture:** Centralize runtime paths so config, session cache, audit logs, and backups work even when `HOME` is absent under systemd. Keep scan/clean operations observable through journal-friendly summary lines and audit log entries. Fix destructive paths so scheduled clean only reports success when it actually cleans successfully.

**Scope:**
- Add HOME-less path resolution for root/systemd contexts.
- Update systemd units to provide explicit environment and writable paths.
- Add scan/clean start and result logging that identifies config/cache/log paths.
- Fix daemon clean to perform live clean when scheduled.
- Fix cleaner partial failure and restore error reporting.
- Make TUI use loaded config and add confirmation before Docker prune.
- After verification, run a feature-focused gap pass for deep-clean space-saving targets.

**Testing:** Add focused unit tests before behavior changes for path resolution, daemon intervals, cleaner partial failures, restore failures, TUI config loading, and Docker confirmation state transitions. Run `go test ./...`, `go test ./... -race`, `go vet ./...`, and explicit builds.
