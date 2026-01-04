# MoonBit Code Quality Analysis & Fixes

## Executive Summary

Project **MoonBit** is a system cleaner for Linux with both TUI and CLI interfaces. The codebase is well-structured with good test coverage and solid safety mechanisms.

**Overall Assessment**: 8.0/10 - Production-ready with architecture improvements recommended.

## High Priority Issues (Phase 1)

### 🟡 **High: Duplicated Session Cache Logic**

**Locations**: `internal/cli/root.go` (lines ~400-430) and `internal/ui/ui.go` (lines ~810-830)

**Issues**:
1. Session cache save/load logic duplicated between CLI and TUI packages
2. Cache path management repeated in both locations
3. No shared utility for cache operations

**Evidence**:
```go
// internal/cli/root.go:404-418
func saveSessionCache(cache *cli.SessionCache) error {
    cacheDir := filepath.Join(homeDir, ".cache", "moonbit")
    if err := os.MkdirAll(cacheDir, 0700); err != nil {
        return err
    }
    // ...
}

// internal/ui/ui.go:817-822 - DUPLICATE LOGIC
func saveSessionCache(cache *SessionCache) error {
    cacheDir := filepath.Join(homeDir, ".cache", "moonbit")
    if err := os.MkdirAll(cacheDir, 0700); err != nil {
        return err
    }
    // ...
}
```

**Impact**: Code duplication makes maintenance harder and introduces risk of inconsistencies

**Tasks**:
1. Extract session cache management into dedicated `internal/session` package
2. Create `cache.go` with unified `Save(cache *SessionCache) error` and `Load() (*SessionCache, error)` functions
3. Update both CLI and TUI to use shared session package

### 🟡 **High: Inconsistent Package Boundaries**

**Location**: `internal/ui/ui.go`

**Issue**: UI package handles filesystem operations directly (violates separation of concerns)

**Evidence**:
```go
// internal/ui/ui.go:492 - UI handling filesystem operations
if err := saveSessionCache(cache); err != nil {
    return scanCompleteMsg{
        Success: false,
        Error:   fmt.Sprintf("failed to save cache: %v", err),
    }
}

// internal/ui/ui.go:817 - Direct filesystem access
cacheDir := filepath.Join(homeDir, ".cache", "moonbit")
if err := os.MkdirAll(cacheDir, 0700); err != nil {
    return err
}
```

**Impact**: Tight coupling between presentation and data layers

**Tasks**:
1. Move filesystem operations from UI package to appropriate service layer
2. UI should only receive messages from service layer
3. Create clear data flow: UI → Service → Filesystem

### 🟡 **High: HumanizeBytes Duplication**

**Locations**: `internal/cli/root.go` (~line 180), `internal/ui/ui.go` (~line 240)

**Issue**: Same function implemented multiple times

**Evidence**:
```go
// internal/cli/root.go:180 - First implementation
func humanizeBytes(bytes uint64) string {
    // ...
}

// internal/ui/ui.go:240 - DUPLICATE
func humanizeBytes(bytes uint64) string {
    // ...
}
```

**Impact**: Maintenance burden, potential inconsistencies

**Tasks**:
1. Create `internal/utils/format.go` with shared `HumanizeBytes()` function
2. Remove duplicate implementations
3. Update all imports

### 🟡 **High: Missing Input Validation**

**Locations**: Multiple command execution points

**Issues**:
1. Package manager commands accept user inputs without validation
2. Docker commands lack parameter sanitization
3. No validation for file paths in some operations

**Evidence**:
```go
// internal/cli/root.go:123 - No validation before execution
cmd := exec.Command("sudo", args...)

// internal/cli/root.go:795 - Command execution without validation
removeCmd = exec.Command("sudo", "pacman", "-Rns", "$(pacman -Qtdq)")

// internal/cleaner/cleaner.go:144 - File deletion with minimal validation
if err := c.deleteFile(fileInfo.Path, category.ShredEnabled); err != nil {
```

**Tasks**:
1. Add input validation functions for command parameters
2. Validate file paths before filesystem operations
3. Add parameter bounds checking for numeric inputs
4. Create `internal/validation/validate.go` with utilities

### 🟡 **Medium: Missing Audit Logging**

**Issue**: Privileged operations (sudo, package manager changes) have no audit trail

**Evidence**:
```go
// internal/cli/root.go:123 - No logging of privileged operation
cmd := exec.Command("sudo", args...)

// internal/cli/root.go:583 - Docker operations not logged
pruneCmd := exec.Command("docker", "image", "prune", "-a", "-f")

// internal/ui/ui.go:1831, 1877 - Systemd commands not logged
cmd := exec.Command("systemctl", "is-enabled", timerName)
cmd := exec.Command("systemctl", "enable", "--now", timerName)
```

**Impact**: No visibility into what operations were performed, difficult to troubleshoot

**Tasks**:
1. Add audit logging for all package manager operations
2. Log systemd timer changes
3. Create log rotation and retention policy
4. Create `internal/audit/logger.go`

## Critical Issues (Phase 2)

### 🔴 **Critical: Incomplete Error Handling**

**Locations**: Multiple locations across codebase

**Issues**:
1. Some error paths silently continue without logging
2. Error messages lack context for user debugging
3. Partial failure scenarios not properly handled

**Evidence**:
```go
// internal/cleaner/cleaner.go:331 - Silent failure
if err := c.backupFile(file.Path, backupFilesDir); err != nil {
    // Log error but continue with other files
    continue
}

// internal/scanner/scanner.go:210 - Silent skip
if os.IsNotExist(err) {
    return nil // Skip non-existent paths silently
}

// internal/scanner/scanner.go:227 - Silent skip without user notification
if err != nil {
    return nil // Skip files we can't access
}

// internal/cleaner/cleaner.go:373 - Error lost in backup
if err != nil {
    return err  // Not logged, error not propagated
}

// internal/cleaner/cleaner.go:428 - Silent skip in restore
if _, err := os.Stat(srcPath); err != nil {
    continue // Skip missing backup files
}

// internal/cleaner/cleaner.go:433-434 - Silent failure
if err := os.MkdirAll(targetDir, 0755); err != nil {
    continue
}
```

**Impact**: Users cannot understand why operations fail, difficult to debug, silent data loss potential

**Tasks**:
1. Add comprehensive error logging for all failure paths
2. Improve error messages with context and actionable information
3. Implement proper error aggregation for batch operations
4. Add structured error types with user-friendly messages
5. Ensure no silent failures in critical operations

### 🔴 **Critical: Missing Error Aggregation**

**Location**: `internal/cleaner/cleaner.go` (lines 117-151)

**Issue**: Batch operations don't aggregate errors for reporting

**Evidence**:
```go
// internal/cleaner/cleaner.go:117-151
filesDeleted := 0
bytesFreed := uint64(0)
var errors []string

for _, fileInfo := range category.Files {
    // ...
    if err := c.deleteFile(fileInfo.Path, category.ShredEnabled); err != nil {
        errors = append(errors, fmt.Sprintf("failed to delete %s: %v", fileInfo.Path, err))
        continue
    }
    // ...
}

// Errors collected but not counted for statistics
// User doesn't know how many succeeded vs failed in summary
```

**Impact**: Users don't see clear success/failure breakdown in batch operations

**Tasks**:
1. Add success/error counters to batch operations
2. Report detailed statistics (files attempted, succeeded, failed)
3. Provide list of all failures with reasons
4. Format: "Deleted X of Y files successfully. Failed files: [list]"

## Additional Gaps from Deep Analysis

### 🟠 **Medium: Missing Features**

**1. Duplicate File Cleanup Incomplete**
- Location: CLI `duplicates` command
- Issue: Scanning exists but `clean` command not implemented
- Evidence: README mentions duplicate removal but functionality missing
- Task: Implement `moonbit duplicates clean` command

**2. Docker Cleanup Not in TUI**
- Location: `internal/ui/ui.go`
- Issue: Docker cleanup only available via CLI
- Evidence: TUI main menu lacks Docker option
- Task: Add Docker cleanup to TUI main menu

### 🟠 **Medium: Configuration Issues**

**1. Safety Config Default Inconsistency**
- Location: `internal/cleaner/cleaner.go:65` vs `internal/cleaner/cleaner.go:500`
- Issue: MaxDeletionSize differs between functions (512000 vs 51200 MB)
- Evidence: NewCleaner sets 500GB, GetDefaultSafetyConfig sets 50GB
- Impact: Inconsistent safety limits
- Task: Align default values and document correct limit

**2. Unused Configuration Values**
- Location: `internal/config/config.go`
- Issue: Some config fields defined but not used
- Evidence: `Config.Scan.EnableAll`, `Config.Scan.DryRunDefault` appear unused
- Task: Implement or remove unused config fields

**3. Hardcoded Worker Count**
- Location: `internal/scanner/scanner.go`
- Issue: Worker count hardcoded to 4
- Impact: Not optimized for different hardware
- Task: Make worker count configurable

### 🟠 **Medium: Test Coverage Gaps**

**1. Missing Integration Tests**
- No tests for CLI + filesystem + scanner interaction
- No tests for TUI interaction flows
- Missing tests for systemd timer management
- Task: Add integration test suite

**2. Missing Package Tests**
- No tests for `cmd/` packages
- No tests for `installer/` package
- Configuration package missing unit tests
- Task: Add basic test coverage for all packages

**3. Edge Case Tests Missing**
- Scanner: deeply nested directories with ignore patterns
- Cleaner: error recovery during multi-file operations
- Duplicate finder: hash collisions, very large file sets
- Task: Add edge case test scenarios

### 🟠 **Medium: Performance Gaps**

**1. Memory Efficiency**
- Location: `internal/scanner/scanner.go`, `internal/duplicates/duplicates.go`
- Issues:
  - Scanner loads all file information into memory before processing
  - Duplicate finder keeps all file metadata in memory during hashing
  - No streaming or pagination for large result sets
- Impact: High memory usage on large scans
- Task: Implement streaming processing, pagination

**2. File Operations Optimization**
- Scanner walks entire directory trees without early termination for empty directories
- Duplicate detection doesn't leverage file size pre-filtering effectively
- Task: Add early termination, better pre-filtering

### 🟠 **Medium: Code Quality Issues**

**1. Complex Functions**
- Location: `internal/ui/ui.go` - Update() method (~400 lines)
- Issue: TUI Update() function overly complex with multiple nested conditionals
- Impact: Hard to maintain, test, and debug
- Task: Refactor into smaller, focused methods

**2. Magic Numbers**
- Location: Various files
- Examples:
  - Timeouts hardcoded without explanation
  - File size limits in tests don't match implementation defaults
  - Directory scanning depth limits not constants
- Task: Extract to named constants

**3. Poor Naming**
- Location: `internal/scanner/scanner.go`
- Issue: Scanner variable named `s` (single letter)
- Impact: Reduced readability
- Task: Use descriptive names

## Implementation Phases

### **Phase 1: Architecture Refactoring (Week 1)**
```
Day 1-2: Extract session cache management into internal/session
Day 3: Refactor UI package to remove filesystem operations
Day 4: Add comprehensive input validation utilities
Day 5: Add audit logging for privileged operations
```

### **Phase 2: Error Handling Improvements (Week 2)**
```
Day 1-2: Add structured error logging throughout codebase
Day 3: Improve error messages with context and actionable information
Day 4: Implement error aggregation for batch operations
Day 5: Add comprehensive tests for error scenarios
```

### **Phase 3: Code Quality & Features (Week 3)**
```
Day 1: Fix configuration inconsistencies
Day 2: Implement missing duplicate cleanup feature
Day 3: Add Docker cleanup to TUI
Day 4: Refactor complex functions
Day 5: Add missing tests for uncovered packages
```

## Detailed Task Breakdown

### **Phase 1 Tasks**

#### Task 1.1: Session Cache Management
- **Files**: `internal/session/cache.go`, `internal/cli/root.go`, `internal/ui/ui.go`
- **Changes**:
  - Create new `internal/session` package
  - Implement `SessionCache` struct with methods:
    - `Save(cache *SessionCache) error`
    - `Load() (*SessionCache, error)`
    - `Clear() error`
    - `Path() string`
  - Move cache logic from CLI and UI to new package
  - Update all references
- **Benefits**: Single source of truth for cache operations

#### Task 1.2: UI Package Refactoring
- **Files**: `internal/ui/ui.go`, `internal/cli/root.go`
- **Changes**:
  - Move `saveSessionCache()` and `loadSessionCache()` to session package
  - Remove direct filesystem operations from UI package
  - UI Update() method returns actions, service layer handles persistence
  - Create service layer for TUI business logic if needed
- **Benefits**: Clear separation between presentation and data layers

#### Task 1.3: Input Validation
- **Files**: `internal/validation/validate.go`, all command execution files
- **Changes**:
  - Create validation package with functions:
    - `ValidateFilePath(path string) error`
    - `ValidatePackage(pkg string) error`
    - `ValidateSize(size uint64) error`
    - `ValidateMode(mode string) error`
  - Add validation before all user input usage
  - Return user-friendly error messages
- **Benefits**: Early error detection, better user experience

#### Task 1.4: Audit Logging
- **Files**: `internal/audit/logger.go`, command execution files
- **Changes**:
  - Create audit logging package
  - Log all privileged operations:
    - Package manager commands
    - Docker operations
    - Systemd changes
    - File deletions
  - Format: timestamp, operation, parameters, result
  - Implement log rotation (7 days retention)
- **Benefits**: Visibility into operations, easier troubleshooting

#### Task 1.5: Remove Code Duplication
- **Files**: `internal/utils/format.go`, `internal/cli/root.go`, `internal/ui/ui.go`
- **Changes**:
  - Create `internal/utils/format.go`
  - Move `HumanizeBytes()` to shared location
  - Remove duplicate implementations
  - Update all imports
- **Benefits**: Single implementation, easier maintenance

### **Phase 2 Tasks**

#### Task 2.1: Structured Error Logging
- **Files**: All `.go` files with error handling
- **Changes**:
  - Add structured logging at error points
  - Include context: operation, file, parameters
  - Use log levels: debug, info, warn, error
  - Ensure no silent failures
- **Examples**:
  ```go
  log.Error("backup failed",
      "file", file.Path,
      "error", err,
      "operation", "backup")
  ```

#### Task 2.2: Actionable Error Messages
- **Files**: All `.go` files with user-facing errors
- **Changes**:
  - Create custom error types with user-friendly messages
  - Include suggested fixes in error messages
  - Translate technical errors to user language
  - Add error codes for programmatic handling
- **Examples**:
  ```go
  return fmt.Errorf("cannot delete %s: file is protected "+
      "System path. Protected paths: /bin, /usr/bin, /etc", path)
  ```

#### Task 2.3: Error Aggregation
- **Files**: `internal/cleaner/cleaner.go`, `internal/scanner/scanner.go`
- **Changes**:
  - Collect all errors in batch operations
  - Return aggregated error with details
  - Report success/failure statistics
  - Allow partial completion with error details
- **Example**: "Deleted 95 files successfully. Failed to delete 5 files: [list of errors]"

#### Task 2.4: Error Scenario Tests
- **Files**: `*_test.go` files
- **Changes**:
  - Add tests for all error paths
  - Test partial failure scenarios
  - Test error message quality
  - Test recovery from errors
  - Add integration tests for error flows

### **Phase 3 Tasks**

#### Task 3.1: Configuration Consistency
- **Files**: `internal/cleaner/cleaner.go`, `internal/cleaner/cleaner_test.go`
- **Changes**:
  - Align MaxDeletionSize defaults (500GB vs 50GB)
  - Document correct limit and reasoning
  - Add validation to prevent mismatches
  - Remove or implement unused config fields

#### Task 3.2: Missing Features
- **Files**: `internal/cli/root.go`, `internal/ui/ui.go`
- **Changes**:
  - Implement `moonbit duplicates clean` command
  - Add Docker cleanup to TUI main menu
  - Ensure feature parity between CLI and TUI
- **Benefits**: Complete promised feature set

#### Task 3.3: Code Quality Refactoring
- **Files**: `internal/ui/ui.go`, `internal/scanner/scanner.go`
- **Changes**:
  - Break down Update() method into smaller functions
  - Rename poorly named variables (e.g., `s` → `scanner`)
  - Extract magic numbers to constants
  - Reduce cyclomatic complexity
- **Benefits**: Improved maintainability and readability

#### Task 3.4: Test Coverage
- **Files**: New test files
- **Changes**:
  - Add tests for cmd packages
  - Add tests for installer package
  - Add integration tests for complex flows
  - Add edge case tests (deep nesting, hash collisions)
- **Target**: >80% coverage for all packages

## Success Metrics

### **Phase 1 Success Criteria**
- [ ] Session cache logic completely centralized
- [ ] No filesystem operations in UI package
- [ ] All user inputs validated before use
- [ ] All privileged operations logged
- [ ] Audit logs readable and structured
- [ ] No duplicate utility functions

### **Phase 2 Success Criteria**
- [ ] All error paths logged
- [ ] Error messages include actionable information
- [ ] Batch operations report partial failures
- [ ] Test coverage for error scenarios > 90%
- [ ] No silent failures in critical operations

### **Phase 3 Success Criteria**
- [ ] Configuration values consistent and documented
- [ ] All README features implemented
- [ ] Function complexity reduced (cyclomatic <10)
- [ ] Test coverage >80% for all packages
- [ ] Magic numbers extracted to constants

## Risk Assessment

### **Technical Risks**
1. **Breaking changes during refactoring** - Mitigation: Extensive testing before each phase
2. **Performance regression** - Mitigation: Benchmark critical paths
3. **Regression in error handling** - Mitigation: Comprehensive error scenario tests
4. **Feature parity issues** - Mitigation: Test both CLI and TUI after changes

### **Resource Risks**
1. **Time estimation** - Contingency: 20% buffer added to each phase
2. **Complexity** - Mitigation: Break down into smaller, independent tasks
3. **Test coverage gaps** - Mitigation: Add tests before refactoring

## Verification Process

Each phase completion requires:
1. **Code review** for architectural correctness
2. **Test suite execution** with 100% pass rate
3. **Integration testing** on multiple Linux distributions
4. **Manual testing** of error scenarios
5. **Performance benchmarking** against baseline

## Appendix: Gap Summary

### A. Session Cache Duplication
**Files**: `internal/cli/root.go`, `internal/ui/ui.go`
**Lines**: Multiple cache save/load operations
**Severity**: High
**Status**: ⏳ Pending (Phase 1, Task 1.1)

### B. UI Filesystem Operations
**File**: `internal/ui/ui.go`
**Lines**: ~492, ~817
**Severity**: High
**Status**: ⏳ Pending (Phase 1, Task 1.2)

### C. Code Duplication
**Files**: `internal/cli/root.go`, `internal/ui/ui.go`
**Lines**: HumanizeBytes duplicate
**Severity**: High
**Status**: ⏳ Pending (Phase 1, Task 1.5)

### D. Missing Input Validation
**Files**: `internal/cli/root.go`, `internal/ui/ui.go`, command execution points
**Lines**: Multiple locations
**Severity**: High
**Status**: ⏳ Pending (Phase 1, Task 1.3)

### E. Missing Audit Logging
**Files**: Command execution files
**Lines**: Multiple exec.Command calls
**Severity**: Medium
**Status**: ⏳ Pending (Phase 1, Task 1.4)

### F. Incomplete Error Handling
**Files**: Multiple locations
**Lines**: cleaner.go:331, scanner.go:210, 227, cleaner.go:373, 428, 433
**Issues**:
1. Silent failures (continue on error without logging)
2. Generic error messages
3. No error aggregation
4. Missing context in errors
**Severity**: Critical
**Status**: ⏳ Pending (Phase 2, Task 2.1-2.4)

### G. Missing Error Aggregation
**File**: `internal/cleaner/cleaner.go`
**Lines**: 117-151
**Severity**: Critical
**Status**: ⏳ Pending (Phase 2, Task 2.3)

### H. Test Failure (Fixed)
**File**: `internal/cleaner/cleaner_test.go`
**Issue**: Size limit mismatch between test (50GB) and implementation (500GB)
**Status**: ✅ Fixed during analysis

### I. Race Conditions
**Assessment**: No race conditions found in core logic
**Status**: ✅ Verified - Race detector passes on all packages

### J. Missing Features
**Files**: CLI duplicates command, TUI main menu
**Severity**: Medium
**Status**: ⏳ Pending (Phase 3, Task 3.2)

### K. Configuration Issues
**Files**: `internal/cleaner/cleaner.go`, `internal/config/config.go`
**Severity**: Medium
**Status**: ⏳ Pending (Phase 3, Task 3.1)

### L. Test Coverage Gaps
**Files**: cmd/ packages, installer/, config/
**Severity**: Medium
**Status**: ⏳ Pending (Phase 3, Task 3.4)

### M. Code Quality Issues
**Files**: `internal/ui/ui.go`, `internal/scanner/scanner.go`
**Severity**: Medium
**Status**: ⏳ Pending (Phase 3, Task 3.3)

---
*Last Updated: 2025-01-05*
*Next Review: 14 days after Phase 1 completion*