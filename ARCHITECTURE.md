# MoonBit Architecture Documentation

**Version**: 1.2.2  
**Last Updated**: 2025-01-05

---

## Overview

MoonBit is a cross-distribution Linux system cleaner built in Go. It provides both a Terminal User Interface (TUI) and Command-Line Interface (CLI) for scanning, previewing, and cleaning temporary files, caches, logs, and application data.

### Key Design Principles

1. **Safety First**: Dry-run by default, protected paths, size limits, risk level checks
2. **Separation of Concerns**: Clear package boundaries, filesystem abstraction, channel-based communication
3. **User Experience**: Interactive TUI, comprehensive CLI, clear error messages
4. **Cross-Distribution**: Auto-detection of available cleanup targets per distribution

---

## Package Structure

```
moonbit/
├── cmd/
│   ├── main.go              # Entry point
│   └── installer/           # Installation TUI
├── internal/
│   ├── cli/                 # CLI command handling
│   ├── ui/                  # Bubble Tea TUI implementation
│   ├── config/              # Configuration management
│   ├── scanner/             # Filesystem scanning
│   ├── cleaner/              # File deletion with safety checks
│   ├── duplicates/           # Duplicate file detection
│   ├── session/              # Session cache management
│   ├── audit/                # Audit logging
│   ├── errors/               # Custom error types
│   ├── validation/           # Input validation
│   └── utils/                # Shared utilities
└── systemd/                  # Systemd timer/service files
```

---

## Core Components

### 1. Configuration System (`internal/config`)

**Purpose**: Manages application configuration and category definitions.

**Key Types**:
- `Config`: Main configuration structure
- `Category`: Cleaning target definition (paths, filters, risk level)
- `SessionCache`: Cached scan results

**Responsibilities**:
- Load/save TOML configuration from `~/.config/moonbit/config.toml`
- Define 30+ cleaning categories for all major Linux distributions
- Auto-generate default configuration on first run
- Validate configuration structure

**Key Functions**:
- `DefaultConfig()`: Creates comprehensive default configuration
- `Load(path string)`: Loads configuration from file or creates default
- `ParseRiskLevel(s string)`: Converts string to RiskLevel enum

**Configuration Structure**:
```toml
[scan]
max_depth = 3
ignore_patterns = ["node_modules", ".git", ".svn", ".hg"]

[[categories]]
name = "Pacman Cache"
paths = ["/var/cache/pacman/pkg"]
filters = [".pkg.tar"]
risk = "Low"
selected = true
min_age_days = 0
```

---

### 2. Scanner (`internal/scanner`)

**Purpose**: Efficiently scans filesystem for cleanable files based on category definitions.

**Key Types**:
- `Scanner`: Main scanner struct with filesystem abstraction
- `FileSystem`: Interface for filesystem operations (enables testing)
- `ScanMsg`: Channel message type for progress/complete/error
- `ScanProgress`: Progress update structure
- `ScanComplete`: Final scan results

**Responsibilities**:
- Traverse directories using godirwalk for efficiency
- Apply category filters (OR logic - matches ANY filter)
- Respect ignore patterns (AND logic - matches ANY = skip)
- Apply age-based filtering (MinAgeDays)
- Send progress updates via channels

**Key Functions**:
- `NewScanner(cfg *config.Config)`: Creates scanner with default filesystem
- `NewScannerWithFs(cfg *config.Config, fs FileSystem)`: Creates scanner with custom filesystem (for testing)
- `ScanCategory(ctx, category, progressCh)`: Scans a single category asynchronously

**Design Patterns**:
- **Filesystem Abstraction**: `FileSystem` interface allows testing with afero memory filesystem
- **Channel-Based Communication**: Progress updates sent via `ScanMsg` channel
- **Concurrent Processing**: Uses goroutines for parallel directory traversal

**Filter Logic**:
- Files match if they match **ANY** filter in the category (OR logic)
- Files are excluded if they match **ANY** ignore pattern (AND logic)
- Age filtering: Only files older than `MinAgeDays` are included

---

### 3. Cleaner (`internal/cleaner`)

**Purpose**: Safely deletes files with multiple safety mechanisms.

**Key Types**:
- `Cleaner`: Main cleaner struct
- `SafetyConfig`: Safety configuration (protected paths, size limits, risk checks)
- `CleanMsg`: Channel message type for progress/complete/error
- `CleanProgress`: Progress update during deletion
- `CleanComplete`: Final deletion results with statistics

**Responsibilities**:
- Perform safety checks before deletion
- Create optional backups before deletion
- Optionally shred files (overwrite with random data)
- Track deletion progress and errors
- Aggregate errors for user reporting

**Key Functions**:
- `NewCleaner(cfg *config.Config)`: Creates cleaner with default safety config
- `CleanCategory(ctx, category, dryRun, progressCh)`: Cleans files in a category
- `deleteFile(path, shredEnabled)`: Deletes a single file with optional shredding
- `RestoreBackup(backupPath)`: Restores files from a backup
- `ListBackups()`: Lists available backups

**Safety Mechanisms**:
1. **Protected Paths**: Prevents deletion of `/bin`, `/usr/bin`, `/etc`, `/boot`, `/sys`, `/proc`
2. **Size Limits**: Maximum 500GB per operation (configurable)
3. **Risk Level Checks**: High-risk categories require explicit confirmation
4. **Dry-Run Default**: Operations preview by default
5. **Backup Support**: Optional backup creation before deletion
6. **Shredding**: Optional secure deletion (overwrite with random data)

**Backup System**:
- Backups stored in `~/.local/share/moonbit/backups/`
- Each backup has: `.backup` directory (files), `.json` metadata file
- Files named with SHA256 hash (first 16 chars) for deduplication
- Metadata includes: timestamp, category, file count, total size, file list

---

### 4. Session Management (`internal/session`)

**Purpose**: Manages scan result caching between operations.

**Key Types**:
- `Manager`: Session cache manager

**Key Functions**:
- `NewManager()`: Creates session manager with default cache path
- `Save(cache *config.SessionCache)`: Saves scan results to disk
- `Load()`: Loads scan results from disk
- `Clear()`: Removes cache file
- `Exists()`: Checks if cache exists

**Cache Location**: `~/.cache/moonbit/scan_results.json`

**Cache Structure**:
```json
{
  "scan_results": {
    "name": "Total Cleanable",
    "files": [...],
    "file_count": 100,
    "size": 1024000
  },
  "total_size": 1024000,
  "total_files": 100,
  "scanned_at": "2025-01-05T12:00:00Z"
}
```

**Benefits**:
- Enables scan-once, clean-later workflow
- Shares results between CLI and TUI modes
- Allows review of results before cleaning

---

### 5. TUI (`internal/ui`)

**Purpose**: Interactive terminal user interface using Bubble Tea.

**Key Types**:
- `Model`: Main TUI model implementing `tea.Model` interface
- `ViewMode`: Enum for different view states

**View Modes**:
- `ModeWelcome`: Main menu
- `ModeScanProgress`: Scanning in progress
- `ModeResults`: Scan results display
- `ModeSelect`: Category selection
- `ModeConfirm`: Confirmation dialog
- `ModeClean`: Cleaning in progress
- `ModeComplete`: Operation complete
- `ModeSchedule`: Systemd timer management
- `ModeDocker`: Docker cleanup menu

**Key Functions**:
- `Start()`: Launches the TUI
- `NewModel()`: Creates initial model state
- `Update(msg tea.Msg)`: Handles all messages (keys, window resize, custom messages)
- `View()`: Renders the current view

**Message Types**:
- `scanProgressMsg`: Progress updates during scanning
- `scanCompleteMsg`: Scan completion with results
- `cleanCompleteMsg`: Clean completion with statistics
- `timerCommandMsg`: Systemd timer operation results
- `dockerCompleteMsg`: Docker cleanup operation results

**Design Patterns**:
- **Async Operations**: Long-running operations (scan, clean) run in goroutines
- **Channel Communication**: Progress updates sent via channels
- **State Machine**: Clear mode transitions based on user actions
- **Message Routing**: Update() method routes messages to appropriate handlers

---

### 6. CLI (`internal/cli`)

**Purpose**: Command-line interface using Cobra.

**Commands**:
- `moonbit`: Launch TUI (default)
- `moonbit scan`: Scan for cleanable files
- `moonbit clean`: Clean files from last scan
- `moonbit duplicates find`: Find duplicate files
- `moonbit duplicates clean`: Interactively remove duplicates
- `moonbit docker images`: Clean unused Docker images
- `moonbit docker all`: Clean all unused Docker resources
- `moonbit pkg orphans`: Remove orphaned packages
- `moonbit pkg kernels`: Remove old kernels
- `moonbit backup list`: List available backups
- `moonbit backup restore <name>`: Restore a backup

**Key Functions**:
- `ScanAndSave()`: Runs scan and saves results to cache
- `ScanAndSaveWithMode(mode)`: Runs scan with mode filtering (quick/deep)
- `CleanSession(dryRun)`: Cleans files from cached scan results

**Refactored Structure** (Phase 3):
- `displayScanHeader()`: Shows scan header
- `initializeScanner()`: Loads config and creates scanner
- `prepareScanCategories()`: Filters categories by mode
- `scanAllCategories()`: Orchestrates scanning loop
- `scanSingleCategory()`: Scans one category
- `saveScanResults()`: Persists results to cache
- `displayScanResults()`: Shows final summary

---

### 7. Duplicate Detection (`internal/duplicates`)

**Purpose**: Finds duplicate files based on content hashing.

**Key Types**:
- `Scanner`: Duplicate file scanner
- `ScanOptions`: Configuration for scanning
- `ScanResult`: Results with duplicate groups
- `DuplicateGroup`: Group of files with same content hash

**Key Functions**:
- `NewScanner(opts ScanOptions)`: Creates scanner with options
- `Scan(progressCh)`: Scans for duplicates, returns groups
- `RemoveDuplicates(filesToRemove)`: Removes specified files

**Algorithm**:
1. **Phase 1**: Collect files and group by size (files with different sizes can't be duplicates)
2. **Phase 2**: Hash files with duplicate sizes using SHA256 (parallel workers)
3. **Phase 3**: Group files by hash, sort by modification time (oldest first)

**Constants**:
- `DefaultHashWorkers`: 4 worker goroutines for parallel hashing
- `DefaultMinSize`: 1KB minimum file size
- `DefaultMaxDepth`: 10 directory depth limit
- `ProgressUpdateInterval`: 100 files before progress update

---

### 8. Error Handling (`internal/errors`)

**Purpose**: Provides structured, user-friendly error types.

**Key Types**:
- `MoonBitError`: Custom error with code, message, context, suggestions
- `ErrorCode`: Enum of error types

**Error Codes**:
- `PERMISSION_DENIED`, `FILE_NOT_FOUND`, `PATH_PROTECTED`
- `BACKUP_FAILED`, `RESTORE_FAILED`
- `SCAN_CANCELLED`, `SCAN_TIMEOUT`
- `CLEAN_FAILED`, `SAFETY_CHECK_FAILED`

**Key Functions**:
- `NewPermissionDeniedError()`: Creates permission error with suggestions
- `NewPathProtectedError()`: Creates protected path error
- `NewCleanFailedError()`: Creates aggregated clean failure error
- `UserMessage()`: Formats error with bullet-point suggestions

**Benefits**:
- User-friendly error messages
- Actionable suggestions
- Programmatic error handling via error codes
- Error aggregation for batch operations

---

### 9. Validation (`internal/validation`)

**Purpose**: Validates user inputs before operations.

**Key Functions**:
- `ValidateFilePath(path)`: Validates file path safety
- `ValidatePackage(pkg)`: Validates package name format
- `ValidateSize(size, maxSize)`: Validates size bounds
- `ValidateMode(mode)`: Validates scan/clean mode
- `ValidateDirExists(path)`: Checks directory exists
- `ValidateFileExists(path)`: Checks file exists

**Protections**:
- Path traversal detection (`..`)
- Protected system path checking
- Package name format validation
- Size limit validation

---

### 10. Audit Logging (`internal/audit`)

**Purpose**: Logs all privileged operations for compliance and troubleshooting.

**Key Types**:
- `Logger`: Thread-safe audit logger
- `LogEntry`: Log entry structure

**Key Functions**:
- `NewLogger()`: Creates logger with log file
- `Log(entry)`: Writes log entry
- `LogPackageOperation()`: Logs package manager operations
- `LogSystemdOperation()`: Logs systemd timer operations
- `LogDockerOperation()`: Logs Docker operations
- `LogCleanOperation()`: Logs file deletion operations

**Log Location**: `~/.local/share/moonbit/logs/audit.log`

**Log Format**:
```
[2025-01-05T12:00:00Z] user=nomadx operation=package_remove args=[package1,package2] result=success
[2025-01-05T12:01:00Z] user=nomadx operation=docker_prune_images args=[-a,-f] result=success
```

---

## Data Flow

### Scan Flow

```
User → CLI/TUI
  ↓
Config.Load() → Load categories
  ↓
Scanner.ScanCategory() → Traverse filesystem
  ↓
Apply filters → Match files
  ↓
Send progress → Update UI
  ↓
Complete → Save to SessionCache
  ↓
Display results
```

### Clean Flow

```
User → Select categories
  ↓
Load SessionCache → Get scan results
  ↓
Cleaner.CleanCategory() → For each category:
  ↓
  Perform safety checks
  ↓
  Create backup (optional)
  ↓
  Delete files (with optional shredding)
  ↓
  Send progress → Update UI
  ↓
Complete → Show statistics
```

---

## Channel-Based Communication

Both scanner and cleaner use Go channels for async communication:

**Scanner**:
```go
type ScanMsg struct {
    Progress *ScanProgress  // Periodic updates
    Complete *ScanComplete  // Final results
    Error    error          // Errors
}
```

**Cleaner**:
```go
type CleanMsg struct {
    Progress *CleanProgress  // Periodic updates
    Complete *CleanComplete   // Final results
    Error    error            // Errors
}
```

**Benefits**:
- Non-blocking UI updates
- Real-time progress feedback
- Clean error handling
- Easy to test

---

## Safety Mechanisms

### 1. Protected Paths
Prevents deletion of critical system directories:
- `/bin`, `/usr/bin`, `/usr/sbin`, `/sbin`
- `/etc`, `/boot`, `/sys`, `/proc`

### 2. Size Limits
- Default: 500GB maximum per operation
- Configurable via `MaxDeletionSize` in safety config
- Prevents accidental deletion of entire filesystems

### 3. Risk Level Checks
- **Low**: Safe to delete (caches, temp files)
- **Medium**: Requires confirmation
- **High**: Requires explicit confirmation, skipped in safe mode

### 4. Dry-Run Default
- All clean operations preview by default
- Requires `--force` flag to actually delete
- Shows exactly what would be deleted

### 5. Backup System
- Optional backup creation before deletion
- Files stored with SHA256 hash names
- JSON metadata for restoration
- Can restore individual files or entire categories

### 6. Shredding
- Optional secure deletion
- Overwrites files with random data before deletion
- Configurable number of passes
- Prevents file recovery

---

## Testing Strategy

### Unit Tests
- Use afero memory filesystem for filesystem operations
- Mock filesystem via `FileSystem` interface
- Test error paths and edge cases
- Current coverage: 64.9% average

### Integration Tests
- Test complete workflows (scan → display → clean)
- Test CLI command execution
- Test TUI state transitions

### Test Files
- `*_test.go` files in each package
- Table-driven tests for multiple scenarios
- Temporary directories for filesystem tests
- Mock implementations for testing

---

## Configuration

### Default Location
`~/.config/moonbit/config.toml`

### Auto-Generation
Configuration is auto-generated on first run if it doesn't exist.

### Key Settings
- `scan.max_depth`: Directory traversal depth (default: 3)
- `scan.ignore_patterns`: Regex patterns to skip directories
- `categories[]`: Array of cleaning target definitions

### Category Definition
```toml
[[categories]]
name = "Category Name"
paths = ["/path/to/scan"]
filters = ["\\.log$", "\\.tmp$"]  # OR logic
risk = "Low"  # Low, Medium, or High
selected = true  # Included in quick scan
min_age_days = 30  # Only files older than 30 days
shred = false  # Enable secure deletion
```

---

## Cross-Distribution Support

MoonBit automatically detects available cleanup targets:

- **Arch/Manjaro**: Pacman, Yay, Paru, Pamac
- **Debian/Ubuntu/Mint**: APT cache
- **Fedora/RHEL/CentOS**: DNF cache
- **openSUSE**: Zypper cache

Categories for non-existent package managers are automatically hidden.

---

## Performance Considerations

### Current Implementation
- Scanner loads all file info into memory
- Duplicate finder keeps all metadata in memory
- Worker count hardcoded to 4

### Future Optimizations (Phase 4)
- Streaming processing for large directories
- Pagination for result sets
- Configurable worker count
- Early termination for empty directories
- Better file size pre-filtering

---

## Error Handling

### Error Types
- Custom `MoonBitError` with error codes
- User-friendly messages with suggestions
- Error aggregation for batch operations
- Context preservation via error wrapping

### Error Flow
```
Operation → Error → MoonBitError → UserMessage() → Display to user
```

---

## Session Cache Format

**Location**: `~/.cache/moonbit/scan_results.json`

**Structure**:
```json
{
  "scan_results": {
    "name": "Total Cleanable",
    "files": [
      {
        "path": "/tmp/file.log",
        "size": 1024,
        "mod_time": "2025-01-05T12:00:00Z"
      }
    ],
    "file_count": 100,
    "size": 1024000
  },
  "total_size": 1024000,
  "total_files": 100,
  "scanned_at": "2025-01-05T12:00:00Z"
}
```

---

## Backup Format

**Location**: `~/.local/share/moonbit/backups/`

**Structure**:
- `CategoryName_20250105_120000.backup/`: Directory containing backup files
- `CategoryName_20250105_120000.backup.json`: Metadata file

**Metadata**:
```json
{
  "created_at": "2025-01-05T12:00:00Z",
  "timestamp": "20250105_120000",
  "category": "Pacman Cache",
  "file_count": 50,
  "total_size": 5120000,
  "files": [
    {
      "path": "/var/cache/pacman/pkg/package.pkg.tar",
      "size": 102400
    }
  ]
}
```

**File Naming**:
- Backup files named with SHA256 hash (first 16 characters)
- Hash computed from original file path
- Enables deduplication and safe restoration

---

## Systemd Integration

### Timers
- `moonbit-scan.timer`: Daily scan at 2 AM
- `moonbit-clean.timer`: Weekly clean on Sunday at 3 AM

### Services
- `moonbit-scan.service`: Executes scan command
- `moonbit-clean.service`: Executes clean command

### Management
- TUI provides interface to enable/disable timers
- CLI can check status: `systemctl list-timers moonbit-*`
- Logs available: `journalctl -u moonbit-scan.service`

---

## Dependencies

### Core
- **Cobra**: CLI command structure
- **Bubble Tea**: TUI framework
- **Lipgloss**: Terminal styling
- **godirwalk**: Efficient directory traversal
- **afero**: Filesystem abstraction for testing

### Standard Library
- `os`, `path/filepath`: Filesystem operations
- `context`: Cancellation and timeouts
- `encoding/json`: JSON serialization
- `crypto/sha256`: File hashing
- `regexp`: Pattern matching

---

## Future Enhancements

### Planned (Phase 4+)
- Performance optimization for large scans
- Integration test suite
- Enhanced documentation
- Additional scan targets (pip, cargo, npm, gradle, maven, flatpak)
- TUI cancellation support
- Category selection persistence

---

*For development commands and usage, see CLAUDE.md*  
*For troubleshooting, see TROUBLESHOOTING.md*
