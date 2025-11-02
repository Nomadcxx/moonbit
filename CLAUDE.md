# CLAUDE.md

**Note**: This project uses [bd (beads)](https://github.com/steveyegge/beads) for issue tracking. Use `bd` commands instead of markdown TODOs. See AGENTS.md for workflow details.

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

MoonBit is a Go-based TUI (Terminal User Interface) application for system cleaning and privacy scrubbing on Linux (primarily Arch-based distributions). It provides an interactive interface for scanning, previewing, and selectively deleting temporary files, caches, logs, and application data.

The application is built using:
- **Cobra** for CLI command structure
- **Bubble Tea** for the TUI interface
- **Lipgloss** for terminal styling
- **godirwalk** for efficient filesystem traversal
- **afero** for filesystem abstraction (enables testing)

## Development Commands

### Build
```bash
go build -o moonbit cmd/main.go
```

### Run Tests
```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/scanner
go test ./internal/cleaner
go test ./internal/config
go test ./internal/cli
go test ./internal/ui

# Run tests with coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run a single test
go test -run TestFunctionName ./internal/package
```

### Run Application
```bash
# Interactive TUI mode (default)
./moonbit

# Scan system for cleanable files
./moonbit scan

# Clean files from last scan (dry-run by default)
./moonbit clean

# Actually delete files (requires --force)
./moonbit clean --force

# For system-wide paths, use sudo
sudo ./moonbit scan
sudo ./moonbit clean --force
```

## Architecture Overview

### Package Structure

The codebase follows Go's standard project layout with clear separation of concerns:

- **cmd/main.go**: Entry point that delegates to the CLI package
- **internal/cli**: CLI command handling (scan, clean) and session management
- **internal/config**: Configuration loading/saving and category definitions
- **internal/scanner**: Filesystem scanning with concurrent processing
- **internal/cleaner**: File deletion with safety checks and optional shredding
- **internal/ui**: Bubble Tea TUI implementation with multiple view modes
- **internal/themes**: Color definitions following sysc-greet design patterns

### Core Data Flow

1. **Configuration Loading** (internal/config/config.go:172-199)
   - Loads from `~/.config/moonbit/config.toml` or creates default config
   - Defines cleaning categories with paths, filters, and risk levels
   - Each category has: Name, Paths, Filters (regex), Risk (Low/Medium/High), ShredEnabled

2. **Scanning Flow** (internal/scanner/scanner.go)
   - Scanner uses godirwalk for efficient directory traversal
   - Supports filesystem abstraction via FileSystem interface for testing
   - Scans run concurrently with progress updates via channels (ScanMsg)
   - Filters are OR-based: file matches if it matches ANY filter
   - Ignore patterns (from config) are respected to skip directories like node_modules
   - Results stored as FileInfo structs with Path, Size, ModTime

3. **Session Cache** (internal/cli/root.go:22-28)
   - Scan results cached to `~/.cache/moonbit/scan_results.json`
   - Enables separation between scan and clean operations
   - Structure: ScanResults (Category), TotalSize, TotalFiles, ScannedAt
   - Cache shared between CLI and TUI modes

4. **Cleaning Flow** (internal/cleaner/cleaner.go)
   - Implements safety checks: protected paths, size limits, risk level verification
   - Optional backup creation before deletion (manifest-based)
   - Optional shredding: overwrites files with random data before deletion
   - Progress updates via channels (CleanMsg)
   - Dry-run mode enabled by default for safety

5. **TUI Architecture** (internal/ui/ui.go)
   - Bubble Tea Model with multiple view modes: Welcome, ScanProgress, Results, Select, Confirm, Clean, Complete
   - Async operations use Go channels and custom message types
   - Shared progress state for real-time updates during scanning/cleaning
   - Keyboard navigation: up/down for menus, left/right for confirm dialog, enter/space to select
   - Style system based on sysc-greet color patterns

### Key Design Patterns

**Channel-Based Progress Communication**
Both scanner and cleaner packages use Go channels to communicate progress:
- Progress messages for UI updates
- Complete messages with final results
- Error messages for failure handling

Example from scanner (internal/scanner/scanner.go:78-99):
```go
type ScanMsg struct {
    Progress *ScanProgress  // Periodic updates
    Complete *ScanComplete  // Final results
    Error    error          // Errors
}
```

**Filesystem Abstraction for Testing**
The scanner uses a FileSystem interface (internal/scanner/scanner.go:16-20) that can be backed by either real OS operations (OsFileSystem) or afero's memory filesystem (AferoFileSystem), enabling unit tests without touching the actual filesystem.

**Safety-First Deletion**
The cleaner package (internal/cleaner/cleaner.go) implements multiple safety layers:
- Protected paths list prevents deletion of critical system directories
- Maximum deletion size limits (default 1GB)
- Risk level checks (High-risk categories require confirmation)
- Dry-run mode as default
- Optional backup creation before deletion

**Session-Based Workflow**
CLI scan and clean commands are decoupled via session cache, allowing:
- Scan once, clean later
- Review scan results before cleaning
- Share results between CLI and TUI modes

### Important Implementation Details

1. **Filter Logic**: In scanner (internal/scanner/scanner.go:272-296), filters use OR logic - a file is included if it matches ANY filter. The ignore patterns use AND logic - matching ANY ignore pattern excludes the file/directory.

2. **Config Risk Levels**: Defined in internal/config/config.go:14-32 as Low/Medium/High constants. High-risk categories require explicit confirmation and are skipped in safe mode.

3. **TUI State Management**: The UI model (internal/ui/ui.go:58-90) maintains state including current mode, scan/clean results, menu position, and category selections. Mode transitions are handled in handleMenuSelect (internal/ui/ui.go:239-302).

4. **Confirmation Dialog**: Uses left/right arrow keys (not up/down) for navigation between Cancel and Confirm buttons. Default selection is "Confirm" (internal/ui/ui.go:568).

5. **Shredding Implementation**: When enabled, files are overwritten with random data before deletion (internal/cleaner/cleaner.go:188-234). Configured via ShredPasses in SafetyConfig.

## Configuration

Default config location: `~/.config/moonbit/config.toml`

Key settings:
- `scan.max_depth`: Directory traversal depth (default: 3)
- `scan.ignore_patterns`: Regex patterns to skip (e.g., "node_modules", ".git")
- `categories`: Array of cleaning targets with paths, filters, and risk levels

The application auto-generates a default config on first run (internal/config/config.go:79-169).

## Testing Strategy

- Scanner tests use afero memory filesystem to simulate directory structures
- Cleaner tests verify safety checks without actual file deletion
- Config tests validate TOML parsing and default generation
- UI tests verify state transitions and message handling

## Common Gotchas

1. **Sudo Requirements**: System-wide paths like `/var/cache/pacman/pkg` require sudo for both scanning and cleaning.

2. **Dry-Run Default**: The clean command defaults to dry-run mode. Use `--force` to actually delete files.

3. **Filter Regex**: Filters in category definitions are Go regex patterns, not glob patterns. Use `\` escaping.

4. **Session Cache Persistence**: Scan results persist in `~/.cache/moonbit/` until overwritten or explicitly cleaned.

5. **TUI vs CLI Modes**: Running `moonbit` without arguments launches TUI. Use subcommands (`scan`, `clean`) for CLI mode.
