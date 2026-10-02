package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/Nomadcxx/moonbit/internal/cleaner"
	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/docker"
	"github.com/Nomadcxx/moonbit/internal/duplicates"
	"github.com/Nomadcxx/moonbit/internal/paths"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	"github.com/Nomadcxx/moonbit/internal/session"
	"github.com/Nomadcxx/moonbit/internal/ui"
	"github.com/Nomadcxx/moonbit/internal/utils"
	"github.com/Nomadcxx/moonbit/internal/validation"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var (
	fromLauncher      bool
	dryRun            bool
	cleanForce        bool
	scanNoPrompt      bool
	listCategories    bool
	includeCategories []string
	excludeCategories []string
	// cleanScannedAt, when set, binds a clean to the scan the caller
	// reviewed: CleanSession refuses a cache from any other scan.
	cleanScannedAt time.Time
	scanMode       string // "quick", "deep", or "" (all)
)

// Constants for scan operations
const (
	// ScanDelayBetweenCategories is the delay between scanning different categories
	// to prevent overwhelming the filesystem and provide smoother progress updates
	ScanDelayBetweenCategories = 100 * time.Millisecond
)

var rootCmd = &cobra.Command{
	Use:   "moonbit",
	Short: "MoonBit - System Cleaner for Linux",
	Long: S.ASCIIHeader() + "\n" +
		S.Muted("A modern system cleaner for Linux\n") +
		S.Muted("Clean caches, logs, and temporary files with ease\n\n") +
		S.Bold("Features:\n") +
		"  • Interactive TUI and powerful CLI\n" +
		"  • Safe dry-runs by default\n" +
		"  • Quick and Deep scan modes\n" +
		"  • Support for all major package managers\n" +
		"  • Docker cleanup and duplicate file detection",
	Run: func(cmd *cobra.Command, args []string) {
		// Started from the application menu: we have no terminal, so find one
		// and re-exec inside it. See RunFromLauncher for why this is not pkexec.
		if fromLauncher {
			if err := RunFromLauncher(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if !hasControllingTerminal() {
				return // handed off to the terminal
			}
		}

		// Check for root access and re-exec with sudo if needed
		if !isRunningAsRoot() {
			reexecWithSudo()
			return
		}

		// Start Bubble Tea UI with moonbit model
		ui.Start()
	},
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan system for cleanable files",
	Long:  "Scan the system for cleanable files and cache locations",
	Run: func(cmd *cobra.Command, args []string) {
		if jsonOut {
			initJSON()
		}
		if scanMode != "" {
			if err := validation.ValidateMode(scanMode); err != nil {
				if jsonOut {
					jsonTerminal(err)
				}
				fmt.Fprintf(os.Stderr, "Invalid scan mode: %v\n", err)
				os.Exit(1)
			}
		}

		if listCategories {
			if jsonOut {
				jsonTerminal(errors.New("--json cannot be combined with --list-categories"))
			}
			if err := ListCategories(scanMode); err != nil {
				fmt.Fprintf(os.Stderr, "List categories failed: %v\n", err)
				os.Exit(1)
			}
			return
		}

		if !isRunningAsRoot() {
			if jsonOut {
				// No TTY to type a sudo password into; the caller (pkexec) must
				// elevate before invoking.
				jsonTerminal(errors.New("scan --json requires root"))
			}
			reexecWithSudo()
			return
		}

		if err := ScanAndSave(); err != nil {
			if jsonOut {
				jsonTerminal(err)
			}
			fmt.Fprintf(os.Stderr, "Scan failed: %v\n", err)
			os.Exit(1)
		}
		if jsonOut {
			return // scanAllCategories already emitted the terminal done event
		}
		if scanNoPrompt {
			return
		}

		fmt.Println()
		fmt.Print(S.Bold("Would you like to clean these files now? [y/N]: "))

		var response string
		fmt.Scanln(&response)

		if strings.ToLower(response) == "y" || strings.ToLower(response) == "yes" {
			fmt.Println()
			if err := CleanSession(false); err != nil {
				fmt.Fprintf(os.Stderr, "Clean failed: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Println(S.Muted("\nFiles not cleaned. Run 'moonbit clean' to clean them later."))
		}
	},
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean files from last scan",
	Long:  "Clean files discovered in the last scan\n\nBy default, this previews what would be deleted. Use --force to actually delete files.",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return applyCleanFlags(cmd)
	},
	Run: func(cmd *cobra.Command, args []string) {
		if jsonOut {
			initJSON()
		}
		if !isRunningAsRoot() && !dryRun {
			if jsonOut {
				jsonTerminal(errors.New("clean --json --force requires root"))
			}
			reexecWithSudo()
			return
		}

		if err := CleanSession(dryRun); err != nil {
			if jsonOut {
				jsonTerminal(err)
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func applyCleanFlags(cmd *cobra.Command) error {
	mode, err := cmd.Flags().GetString("mode")
	if err != nil {
		return fmt.Errorf("failed to read mode flag: %w", err)
	}
	if err := validation.ValidateMode(mode); err != nil {
		return err
	}

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return fmt.Errorf("failed to read force flag: %w", err)
	}
	if force {
		dryRun = false
		return nil
	}

	preview, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return fmt.Errorf("failed to read dry-run flag: %w", err)
	}
	dryRun = preview
	return nil
}

// isRunningAsRoot checks if the current process has root privileges
func isRunningAsRoot() bool {
	return os.Geteuid() == 0
}

// reexecWithSudo re-executes the current command with sudo
func reexecWithSudo() {
	fmt.Println(S.ASCIIHeader())
	fmt.Println(S.Warning("⚠ Root Access Required"))
	fmt.Println(S.Separator())
	fmt.Println("MoonBit needs root access to scan and clean system-wide caches.")
	fmt.Println(S.Muted("You will be prompted for your password...\n"))

	// Get the current executable path
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Unable to determine executable path: %v\n", err)
		os.Exit(1)
	}

	// Build the sudo command with all original arguments
	args := append([]string{exe}, os.Args[1:]...)
	cmd := exec.Command("sudo", args...)

	// Connect stdin/stdout/stderr to maintain interactivity
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run the command
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// ScanAndSave runs a comprehensive scan and saves results to cache
func ScanAndSave() error {
	return ScanAndSaveWithMode(scanMode)
}

// ScanAndSaveWithMode runs a scan filtered by mode (quick/deep)
func ScanAndSaveWithMode(mode string) error {
	if !jsonOut {
		displayScanHeader(mode)
	}

	cfg, s, err := initializeScanner()
	if err != nil {
		return err
	}

	categories, err := prepareScanCategories(mode, cfg)
	if err != nil {
		return err
	}
	categories, err = applyCategorySelection(categories, includeCategories, excludeCategories)
	if err != nil {
		return err
	}

	scannedAt := time.Now()
	totalSize, totalFiles, scanResults, err := scanAllCategories(s, categories, scannedAt)
	if err != nil {
		return err
	}

	if err := saveScanResults(totalSize, totalFiles, scanResults, scannedAt); err != nil {
		return err
	}

	if !jsonOut {
		displayScanResults(totalFiles, totalSize)
	}
	return nil
}

func ListCategories(mode string) error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	categories, err := prepareScanCategories(mode, cfg)
	if err != nil {
		return err
	}
	categories, err = applyCategorySelection(categories, includeCategories, excludeCategories)
	if err != nil {
		return err
	}

	writeCategoryList(os.Stdout, categories)
	return nil
}

func writeCategoryList(out io.Writer, categories []config.Category) {
	fmt.Fprintln(out, S.Header("Categories"))
	fmt.Fprintln(out, S.Separator())
	for _, category := range categories {
		selected := "deep"
		if category.Selected {
			selected = "quick"
		}
		fmt.Fprintf(out, "%s\t%s\t%s\n", category.Name, category.Risk.String(), selected)
	}
}

// displayScanHeader shows the scan header with appropriate mode label
func displayScanHeader(mode string) {
	modeLabel := "Comprehensive"
	if mode == "quick" {
		modeLabel = "Quick"
	} else if mode == "deep" {
		modeLabel = "Deep"
	}

	fmt.Println(S.ASCIIHeader())
	fmt.Println(S.Header(fmt.Sprintf("%s Scan", modeLabel)))
	fmt.Println(S.Separator())
}

// initializeScanner loads config and creates a scanner instance
func initializeScanner() (*config.Config, *scanner.Scanner, error) {
	if configPath, err := paths.ConfigFile(); err == nil && !jsonOut {
		fmt.Printf("Using config: %s\n", configPath)
	}

	cfg, err := config.Load("")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	s := scanner.NewScanner(cfg)
	return cfg, s, nil
}

// prepareScanCategories filters and prepares categories based on scan mode
func prepareScanCategories(mode string, cfg *config.Config) ([]config.Category, error) {
	availableCategories := detectAvailableCategories()
	allCategories := append([]config.Category{}, cfg.Categories...)
	allCategories = append(allCategories, availableCategories...)

	// Filter by mode
	var filteredCategories []config.Category
	for _, category := range allCategories {
		if mode == "quick" && !category.Selected {
			continue // Quick mode: only Selected:true categories (safe, fast)
		}
		// Deep mode scans everything (no filter needed)
		filteredCategories = append(filteredCategories, category)
	}

	return filteredCategories, nil
}

// scanAllCategories scans all provided categories and aggregates results
func scanAllCategories(s *scanner.Scanner, categories []config.Category, scannedAt time.Time) (uint64, int, config.Category, error) {
	started := time.Now()
	var totalSize uint64
	var totalFiles int
	var categoriesScanned int
	var failedCategories []string
	var scanResults config.Category
	scanResults.Name = "Total Cleanable"
	scanResults.Files = []config.FileInfo{}

	for i, category := range categories {
		if jsonOut && jsonCtx.Err() != nil {
			return totalSize, totalFiles, scanResults, jsonCtx.Err()
		}
		if dropReadOnlyPaths(&category) && len(category.Paths) == 0 {
			if jsonOut {
				jsonEmit.emit("category_skipped", map[string]any{"name": category.Name, "reason": "read-only"})
			} else {
				fmt.Printf("Skipping %s (read-only to this process)\n", category.Name)
			}
			continue
		}
		if !categoryPathExists(&category) {
			if !jsonOut {
				fmt.Printf("Skipping %s (not found)\n", category.Name)
			}
			continue
		}

		if jsonOut {
			jsonEmit.emit("category", map[string]any{
				"name": category.Name, "i": i + 1, "total": len(categories),
			})
		} else {
			fmt.Printf("Scanning %s (%d/%d)...\n", category.Name, i+1, len(categories))
		}

		categoryStarted := time.Now()
		stats, err := scanSingleCategory(s, &category)
		categoryDuration := time.Since(categoryStarted)
		if err != nil {
			if jsonOut {
				if jsonCtx.Err() != nil {
					return totalSize, totalFiles, scanResults, jsonCtx.Err()
				}
				jsonEmit.emit("category_error", map[string]any{"name": category.Name, "msg": err.Error()})
			} else {
				fmt.Printf("  Error: %v\n", err)
			}
			failedCategories = append(failedCategories, fmt.Sprintf("%s: %v", category.Name, err))
			continue
		}

		if stats != nil {
			var categorySize uint64
			for _, file := range stats.Files {
				categorySize += file.Size
			}

			if jsonOut {
				done := map[string]any{
					"name":        category.Name,
					"files":       len(stats.Files),
					"bytes":       categorySize,
					"duration_ms": categoryDuration.Milliseconds(),
				}
				if category.Action == config.ActionTruncate {
					done["truncate"] = true
				}
				jsonEmit.emit("category_done", done)
			} else {
				fmt.Println(formatScanCategoryResult(len(stats.Files), categorySize, categoryDuration))
			}

			totalSize += categorySize
			totalFiles += len(stats.Files)
			categoriesScanned++
			scanResults.Files = append(scanResults.Files, stats.Files...)
		}

		// Small delay between scans to prevent overwhelming the filesystem
		time.Sleep(ScanDelayBetweenCategories)
	}

	if jsonOut {
		done := map[string]any{
			"files":       totalFiles,
			"bytes":       totalSize,
			"categories":  categoriesScanned,
			"duration_ms": time.Since(started).Milliseconds(),
			"scanned_at":  scannedAt,
		}
		if len(failedCategories) > 0 {
			done["failed"] = failedCategories
		}
		jsonEmit.emit("done", done)
	} else {
		fmt.Println(formatScanSummary(categoriesScanned, totalFiles, totalSize, time.Since(started)))
		if len(failedCategories) > 0 {
			fmt.Println(formatScanFailureSummary(failedCategories))
		}
	}
	return totalSize, totalFiles, scanResults, nil
}

func formatScanFailureSummary(failures []string) string {
	unit := "categories"
	if len(failures) == 1 {
		unit = "category"
	}
	return fmt.Sprintf("Partial scan: %d %s failed: %s", len(failures), unit, strings.Join(failures, "; "))
}

func formatScanCategoryResult(files int, size uint64, duration time.Duration) string {
	return fmt.Sprintf("  Found %d files (%s) in %s", files, utils.HumanizeBytes(size), duration.Round(100*time.Millisecond))
}

func formatScanSummary(categoriesScanned int, files int, bytes uint64, duration time.Duration) string {
	return fmt.Sprintf(
		"Scan summary: categories_scanned=%d files=%d bytes=%d duration=%s",
		categoriesScanned,
		files,
		bytes,
		duration.Round(100*time.Millisecond),
	)
}

// categoryPathExists checks if any path in the category exists
func categoryPathExists(category *config.Category) bool {
	// Special case for Thumbnail Cache (handled differently)
	if category.Name == "Thumbnail Cache" {
		return true
	}

	for _, path := range category.Paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		matches, err := filepath.Glob(path)
		if err != nil {
			continue
		}
		for _, match := range matches {
			if _, err := os.Stat(match); err == nil {
				return true
			}
		}
	}
	return false
}

// readOnlyFS reports whether path sits on a filesystem this process cannot
// write, such as /home under the daemon unit's ProtectHome=read-only. Nothing
// there can be deleted, so a scan must not offer it.
var readOnlyFS = func(path string) bool {
	var st unix.Statfs_t
	return unix.Statfs(path, &st) == nil && st.Flags&unix.ST_RDONLY != 0
}

// dropReadOnlyPaths removes the category paths on read-only filesystems and
// reports whether it removed any. A glob is judged by its literal prefix.
func dropReadOnlyPaths(category *config.Category) bool {
	var kept []string
	for _, p := range category.Paths {
		root := p
		if i := strings.IndexAny(p, "*?["); i >= 0 {
			root = filepath.Dir(p[:i])
		}
		if readOnlyFS(root) {
			continue
		}
		kept = append(kept, p)
	}
	dropped := len(kept) != len(category.Paths)
	category.Paths = kept
	return dropped
}

// scanSingleCategory scans a single category and returns its stats
func scanSingleCategory(s *scanner.Scanner, category *config.Category) (*config.Category, error) {
	progressCh := make(chan scanner.ScanMsg, 10)
	go s.ScanCategory(jsonContext(), category, progressCh)

	for msg := range progressCh {
		if msg.Progress != nil && jsonOut && jsonThrottle.ready() {
			jsonEmit.emit("scan", map[string]any{
				"files": msg.Progress.FilesScanned,
				"bytes": msg.Progress.Bytes,
				"dir":   msg.Progress.CurrentDir,
			})
		}
		if msg.Complete != nil {
			return msg.Complete.Stats, nil
		}
		if msg.Error != nil {
			return nil, msg.Error
		}
	}

	return nil, fmt.Errorf("scan completed without results")
}

// jsonContext returns the cancellable context in --json mode, plain background otherwise.
func jsonContext() context.Context {
	if jsonOut {
		return jsonCtx
	}
	return context.Background()
}

// saveScanResults creates and saves the session cache
func saveScanResults(totalSize uint64, totalFiles int, scanResults config.Category, scannedAt time.Time) error {
	cache := &config.SessionCache{
		ScanResults: &scanResults,
		TotalSize:   totalSize,
		TotalFiles:  totalFiles,
		ScannedAt:   scannedAt,
	}

	sessionMgr, err := session.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create session manager: %w", err)
	}

	if err := sessionMgr.Save(cache); err != nil {
		return fmt.Errorf("failed to save session cache: %w", err)
	}

	if !jsonOut {
		fmt.Printf("Saved scan cache: %s\n", sessionMgr.Path())
	}
	return nil
}

func commandLine(command string, args ...string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}

func cleanFileActionCounts(files []config.FileInfo) (deleted, truncated int) {
	for _, file := range files {
		if file.CategoryAction == config.ActionTruncate {
			truncated++
		} else {
			deleted++
		}
	}
	return deleted, truncated
}

// cleanBegin emits the clean-start event with a per-category rollup of what
// will actually be deleted (after revalidation). Full paths live in the cache;
// the panel only needs counts to render the review list.
func cleanBegin(cache *config.SessionCache, files []config.FileInfo, dryRun bool) {
	if !jsonOut {
		return
	}
	jsonEmit.emit("clean_begin", map[string]any{
		"files":      cache.TotalFiles,
		"bytes":      cache.TotalSize,
		"dry_run":    dryRun,
		"categories": categoryRollup(files),
	})
}

// CategoryStat is a per-category file/byte rollup of a file list.
type CategoryStat struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes uint64 `json:"bytes"`
}

// categoryRollup aggregates files by CategoryName in first-seen order.
func categoryRollup(files []config.FileInfo) []CategoryStat {
	var order []string
	agg := map[string]*CategoryStat{}
	for _, f := range files {
		name := f.CategoryName
		if name == "" {
			name = "Other"
		}
		if _, ok := agg[name]; !ok {
			agg[name] = &CategoryStat{Name: name}
			order = append(order, name)
		}
		agg[name].Files++
		agg[name].Bytes += f.Size
	}
	cats := make([]CategoryStat, 0, len(order))
	for _, n := range order {
		cats = append(cats, *agg[n])
	}
	return cats
}

// displayScanResults shows the final scan results summary
func displayScanResults(totalFiles int, totalSize uint64) {
	fmt.Println()
	fmt.Println(S.Header("Scan Results"))
	fmt.Println(S.Separator())
	fmt.Printf("  %s %d\n", S.Bold("Files found:"), totalFiles)
	fmt.Printf("  %s %s\n", S.Bold("Space available:"), S.Success(utils.HumanizeBytes(totalSize)))
}

// CleanSession executes the actual cleaning based on session cache
func CleanSession(dryRun bool) error {
	modeLabel := "Standard"
	if scanMode == "quick" {
		modeLabel = "Quick"
	} else if scanMode == "deep" {
		modeLabel = "Deep"
	}

	if !jsonOut {
		fmt.Println(S.ASCIIHeader())
		fmt.Println(S.Header(fmt.Sprintf("%s Clean", modeLabel)))
		fmt.Println(S.Separator())
	}

	// Load session cache
	sessionMgr, err := session.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create session manager: %w", err)
	}
	if !jsonOut {
		fmt.Printf("Using scan cache: %s\n", sessionMgr.Path())
	}
	cache, err := sessionMgr.Load()
	if err != nil {
		return fmt.Errorf("no scan results found - run scan first: %w", err)
	}
	if cache.ScanResults == nil {
		return fmt.Errorf("invalid scan results: missing scan result details")
	}
	if !cleanScannedAt.IsZero() && !cache.ScannedAt.Equal(cleanScannedAt) {
		return fmt.Errorf("the reviewed scan was replaced by a newer one; scan again before cleaning")
	}

	if cache.TotalFiles == 0 {
		if !jsonOut {
			fmt.Println("No files to clean.")
		} else {
			jsonEmit.emit("clean_done", map[string]any{"deleted": 0, "freed": 0, "errors": []string{}})
		}
		return nil
	}

	// Load config and create cleaner
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Re-derive the delete list from config before anything acts on it. The cache
	// is user-writable and consumed as root, so its paths are claims to verify,
	// not instructions to follow.
	cache, err = revalidateSessionCache(cache, cfg)
	if err != nil {
		return err
	}
	if cache.TotalFiles == 0 {
		if !jsonOut {
			fmt.Println("No files to clean: nothing in the scan cache could be verified against the current config.")
		} else {
			jsonEmit.emit("clean_done", map[string]any{"deleted": 0, "freed": 0, "errors": []string{}})
		}
		return nil
	}

	// Filter cache by mode if specified
	if scanMode != "" {
		cache = filterCacheByMode(cache, cfg, scanMode)
		if cache.TotalFiles == 0 {
			if !jsonOut {
				fmt.Printf("No files to clean in %s mode.\n", scanMode)
			} else {
				jsonEmit.emit("clean_done", map[string]any{"deleted": 0, "freed": 0, "errors": []string{}})
			}
			return nil
		}
	}

	cache, err = filterCacheByCategorySelection(cache, includeCategories, excludeCategories)
	if err != nil {
		return err
	}
	if cache.TotalFiles == 0 {
		if !jsonOut {
			fmt.Println("No files to clean after category filters.")
		} else {
			jsonEmit.emit("clean_done", map[string]any{"deleted": 0, "freed": 0, "errors": []string{}})
		}
		return nil
	}

	c := cleaner.NewCleaner(cfg)
	ctx := jsonContext()

	if dryRun {
		cleanBegin(cache, cache.ScanResults.Files, true)
		if jsonOut {
			jsonEmit.emit("clean_done", map[string]any{
				"deleted": 0, "freed": uint64(0), "errors": []string{}, "dry_run": true,
			})
			return nil
		}
		deleted, truncated := cleanFileActionCounts(cache.ScanResults.Files)
		deletedLabel, truncatedLabel := "files", "files"
		if deleted == 1 {
			deletedLabel = "file"
		}
		if truncated == 1 {
			truncatedLabel = "file"
		}
		fmt.Printf("DRY RUN - Would delete %d %s and truncate %d %s (%s)\n",
			deleted, deletedLabel, truncated, truncatedLabel, utils.HumanizeBytes(cache.TotalSize))

		// Show preview of what would be cleaned
		if cache.ScanResults != nil && len(cache.ScanResults.Files) > 0 {
			fmt.Println("\n📋 Files that would be cleaned:")
			for i, file := range cache.ScanResults.Files {
				if i >= 10 { // Limit preview
					fmt.Printf("   ... and %d more files\n", len(cache.ScanResults.Files)-10)
					break
				}
				fmt.Printf("   %s (%s)\n", file.Path, utils.HumanizeBytes(file.Size))
			}
		}

		fmt.Println("\n💡 Use --force flag to actually delete files:")
		fmt.Println("   moonbit clean --force")
		return nil
	}

	// Actual cleaning using cleaner package
	cleanBegin(cache, cache.ScanResults.Files, false)
	if !jsonOut {
		fmt.Printf("🗑️  Deleting %d files (%s)...\n",
			cache.TotalFiles, utils.HumanizeBytes(cache.TotalSize))
	}

	progressCh := make(chan cleaner.CleanMsg, 10)
	// Errors are delivered over progressCh; the return value is redundant here.
	go func() { _ = c.CleanCategory(ctx, cache.ScanResults, dryRun, progressCh) }()

	var deletedBytes uint64
	var deletedFiles int
	var truncatedFiles int
	var errorsList []string

	// Process cleaning messages
	for msg := range progressCh {
		if msg.Progress != nil {
			if jsonOut && jsonThrottle.ready() {
				jsonEmit.emit("clean", map[string]any{
					"done":  msg.Progress.FilesProcessed,
					"total": msg.Progress.TotalFiles,
					"freed": msg.Progress.BytesFreed,
					"file":  msg.Progress.CurrentFile,
				})
			}
			// Progress update every 100 files
			if !jsonOut && msg.Progress.FilesProcessed%100 == 0 && msg.Progress.FilesProcessed > 0 {
				fmt.Printf("   Progress: %d/%d files (%s)\n",
					msg.Progress.FilesProcessed,
					msg.Progress.TotalFiles,
					utils.HumanizeBytes(msg.Progress.BytesFreed))
			}
		}

		if msg.Complete != nil {
			deletedFiles = msg.Complete.FilesDeleted
			truncatedFiles = msg.Complete.FilesTruncated
			deletedBytes = msg.Complete.BytesFreed
			errorsList = msg.Complete.Errors

			if !jsonOut && msg.Complete.BackupCreated {
				fmt.Printf("   📦 Backup created: %s\n", msg.Complete.BackupPath)
			}
			break
		}

		if msg.Error != nil {
			return fmt.Errorf("cleaning failed: %w", msg.Error)
		}
	}

	if jsonOut {
		if ctx.Err() != nil {
			return fmt.Errorf("clean cancelled: %w", ctx.Err())
		}
		if errorsList == nil {
			errorsList = []string{}
		}
		jsonEmit.emit("clean_done", map[string]any{
			"deleted": deletedFiles, "freed": deletedBytes, "errors": errorsList,
		})
		if len(errorsList) > 0 {
			// Terminal event already carried the error detail.
			return nil
		}
		_ = clearSessionCache()
		return nil
	}

	fmt.Println()
	fmt.Println(S.Header("Cleaning Complete"))
	fmt.Println(S.Separator())
	fmt.Printf("  %s %d\n", S.Bold("Files deleted:"), deletedFiles)
	fmt.Printf("  %s %d\n", S.Bold("Files truncated:"), truncatedFiles)
	fmt.Printf("  %s %s\n", S.Bold("Space freed:"), S.Success(utils.HumanizeBytes(deletedBytes)))

	if len(errorsList) > 0 {
		fmt.Printf("  %s %d files could not be deleted\n", S.Warning("Errors:"), len(errorsList))
		if len(errorsList) <= 5 {
			for _, err := range errorsList {
				fmt.Printf("      - %s\n", err)
			}
		}
		return fmt.Errorf("cleaning incomplete: %d file(s) could not be deleted", len(errorsList))
	}

	fmt.Printf("   ⚡ Scan data cleared\n")

	if err := clearSessionCache(); err != nil && !os.IsNotExist(err) {
		fmt.Printf("   ⚠️  Warning: Could not clear cache file: %v\n", err)
	}

	return nil
}

// detectAvailableCategories dynamically finds available cleaning targets
func detectAvailableCategories() []config.Category {
	return config.DynamicCategories()
}

func clearSessionCache() error {
	sessionMgr, err := session.NewManager()
	if err != nil {
		return err
	}
	return sessionMgr.Clear()
}

// revalidateSessionCache verifies the on-disk scan cache against config and
// reports what it discarded. Returns an error only when the cache as a whole is
// unusable (stale, unverifiable); individual bad entries are dropped.
func revalidateSessionCache(cache *config.SessionCache, cfg *config.Config) (*config.SessionCache, error) {
	verified, report, err := validation.RevalidateCache(
		cache, config.AuthoritativeCategories(cfg), validation.CacheOptions{})
	if err != nil {
		return nil, err
	}

	if report.TotalDropped() > 0 && !jsonOut {
		fmt.Printf("%s %d of %d scanned files no longer verify against config and will be skipped (%s)\n",
			S.Warning("Note:"), report.TotalDropped(),
			report.TotalDropped()+report.Accepted, report.Summary())
	}

	return verified, nil
}

// filterCacheByMode filters cached files based on clean mode
func filterCacheByMode(cache *config.SessionCache, cfg *config.Config, mode string) *config.SessionCache {
	if mode == "" {
		return cache // No filtering
	}

	// Build fallback metadata for cache files created before file-level provenance existed.
	type pathRule struct {
		path     string
		risk     config.RiskLevel
		selected bool
	}
	var rules []pathRule
	for _, cat := range cfg.Categories {
		for _, path := range cat.Paths {
			rules = append(rules, pathRule{path: path, risk: cat.Risk, selected: cat.Selected})
		}
	}

	// Filter files
	var filteredFiles []config.FileInfo
	var filteredSize uint64

	for _, file := range cache.ScanResults.Files {
		risk := file.CategoryRisk
		selected := file.CategorySelected
		if file.CategoryName == "" {
			risk = config.Low
			selected = true
			for _, rule := range rules {
				if paths.MatchesPathOrDescendant(rule.path, file.Path) {
					risk = rule.risk
					selected = rule.selected
					break
				}
			}
		}

		// Apply mode filter
		if mode == "quick" && (risk != config.Low || !selected) {
			continue // Quick mode: only Low risk
		}
		// Deep mode includes everything

		filteredFiles = append(filteredFiles, file)
		filteredSize += file.Size
	}

	return &config.SessionCache{
		ScanResults: &config.Category{
			Name:      cache.ScanResults.Name,
			Files:     filteredFiles,
			FileCount: len(filteredFiles),
			Size:      filteredSize,
			Risk:      cache.ScanResults.Risk,
		},
		TotalSize:  filteredSize,
		TotalFiles: len(filteredFiles),
		ScannedAt:  cache.ScannedAt,
	}
}

func applyCategorySelection(categories []config.Category, includes, excludes []string) ([]config.Category, error) {
	includeSet := normalizedNameSet(includes)
	excludeSet := normalizedNameSet(excludes)
	knownNames := make(map[string]string, len(categories))
	for _, category := range categories {
		knownNames[normalizeCategoryName(category.Name)] = category.Name
	}

	for name := range includeSet {
		if _, exists := knownNames[name]; !exists {
			return nil, fmt.Errorf("unknown category: %s", name)
		}
	}
	for name := range excludeSet {
		if _, exists := knownNames[name]; !exists {
			return nil, fmt.Errorf("unknown category: %s", name)
		}
	}

	var selected []config.Category
	for _, category := range categories {
		name := normalizeCategoryName(category.Name)
		if len(includeSet) > 0 && !includeSet[name] {
			continue
		}
		if excludeSet[name] {
			continue
		}
		selected = append(selected, category)
	}
	return selected, nil
}

func filterCacheByCategorySelection(cache *config.SessionCache, includes, excludes []string) (*config.SessionCache, error) {
	if len(includes) == 0 && len(excludes) == 0 {
		return cache, nil
	}
	if cache == nil || cache.ScanResults == nil {
		return cache, nil
	}

	knownNames := make(map[string]string)
	for _, file := range cache.ScanResults.Files {
		if file.CategoryName == "" {
			continue
		}
		knownNames[normalizeCategoryName(file.CategoryName)] = file.CategoryName
	}
	if len(knownNames) == 0 && len(cache.ScanResults.Files) > 0 {
		return nil, fmt.Errorf("category filters require scan cache category metadata; run moonbit scan again")
	}

	includeSet := normalizedNameSet(includes)
	excludeSet := normalizedNameSet(excludes)
	for name := range includeSet {
		if _, exists := knownNames[name]; !exists {
			return nil, fmt.Errorf("unknown category in scan cache: %s", name)
		}
	}
	for name := range excludeSet {
		if _, exists := knownNames[name]; !exists {
			return nil, fmt.Errorf("unknown category in scan cache: %s", name)
		}
	}

	var filteredFiles []config.FileInfo
	var filteredSize uint64
	for _, file := range cache.ScanResults.Files {
		name := normalizeCategoryName(file.CategoryName)
		if len(includeSet) > 0 && !includeSet[name] {
			continue
		}
		if excludeSet[name] {
			continue
		}
		filteredFiles = append(filteredFiles, file)
		filteredSize += file.Size
	}

	return &config.SessionCache{
		ScanResults: &config.Category{
			Name:      cache.ScanResults.Name,
			Files:     filteredFiles,
			FileCount: len(filteredFiles),
			Size:      filteredSize,
			Risk:      cache.ScanResults.Risk,
		},
		TotalSize:  filteredSize,
		TotalFiles: len(filteredFiles),
		ScannedAt:  cache.ScannedAt,
	}, nil
}

func normalizedNameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		normalized := normalizeCategoryName(name)
		if normalized == "" {
			continue
		}
		set[normalized] = true
	}
	return set
}

func normalizeCategoryName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage backups",
	Long:  "List and restore backups created before cleaning operations",
}

var backupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available backups",
	Run: func(cmd *cobra.Command, args []string) {
		backups, err := cleaner.ListBackups()
		if err != nil {
			fmt.Printf("Failed to list backups: %v\n", err)
			return
		}

		if len(backups) == 0 {
			fmt.Println("No backups found")
			return
		}

		fmt.Println("📦 Available Backups:")
		fmt.Println("===================")
		for i, backup := range backups {
			fmt.Printf("%d. %s\n", i+1, backup)
		}
	},
}

var backupRestoreCmd = &cobra.Command{
	Use:   "restore [backup-name]",
	Short: "Restore files from a backup",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		backupName := args[0]

		backupDir, err := paths.DataDir("backups")
		if err != nil {
			fmt.Printf("❌ Failed to determine backup directory: %v\n", err)
			return
		}
		backupPath := filepath.Join(backupDir, backupName)

		fmt.Printf("🔄 Restoring backup: %s\n", backupName)

		if err := cleaner.RestoreBackup(backupPath); err != nil {
			fmt.Printf("❌ Failed to restore backup: %v\n", err)
			return
		}

		fmt.Println("✅ Backup restored successfully!")
	},
}

var journalCmd = &cobra.Command{
	Use:   "journal",
	Short: "Manage the systemd journal",
	Long: "Reclaim systemd journal space using journalctl's supported vacuum interface.\n\n" +
		"MoonBit deliberately does not delete files under /var/log/journal directly: journald\n" +
		"mmaps its active journal files, so unlinking them corrupts the journal, loses rotation\n" +
		"state, and reclaims nothing until journald restarts.",
}

var journalVacuumCmd = &cobra.Command{
	Use:   "vacuum",
	Short: "Reclaim systemd journal space",
	Long:  "Runs journalctl --vacuum-size / --vacuum-time to shrink the journal safely",
	RunE: func(cmd *cobra.Command, args []string) error {
		size, _ := cmd.Flags().GetString("size")
		age, _ := cmd.Flags().GetString("time")
		// Mirror `clean`: preview by default, --force applies.
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if force, _ := cmd.Flags().GetBool("force"); force {
			dryRun = false
		}

		if size == "" && age == "" {
			return fmt.Errorf("specify --size (e.g. 500M) or --time (e.g. 14d)")
		}

		if _, err := exec.LookPath("journalctl"); err != nil {
			return fmt.Errorf("journalctl not found; this system does not use systemd-journald")
		}

		auditLog, _ := audit.NewLogger()
		if auditLog != nil {
			defer auditLog.Close()
		}

		fmt.Println(S.Header("Systemd Journal"))
		fmt.Println(S.Separator())

		usage := exec.Command("journalctl", "--disk-usage")
		usage.Stdout = os.Stdout
		usage.Stderr = os.Stderr
		usage.Run()

		var vacuumArgs []string
		if size != "" {
			vacuumArgs = append(vacuumArgs, "--vacuum-size="+size)
		}
		if age != "" {
			vacuumArgs = append(vacuumArgs, "--vacuum-time="+age)
		}

		if dryRun {
			var invocation []string
			if size != "" {
				invocation = append(invocation, "--size="+size)
			}
			if age != "" {
				invocation = append(invocation, "--time="+age)
			}
			fmt.Printf("\nDRY RUN - would run: journalctl %s\n", strings.Join(vacuumArgs, " "))
			fmt.Println("\n💡 Use --force to actually vacuum the journal:")
			fmt.Printf("   moonbit journal vacuum %s --force\n", strings.Join(invocation, " "))
			return nil
		}

		if !isRunningAsRoot() {
			reexecWithSudo()
		}

		fmt.Printf("\n🗑️  Running: journalctl %s\n", strings.Join(vacuumArgs, " "))
		vacuum := exec.Command("journalctl", vacuumArgs...)
		vacuum.Stdout = os.Stdout
		vacuum.Stderr = os.Stderr
		err := vacuum.Run()

		if auditLog != nil {
			result := "success"
			if err != nil {
				result = "failed"
			}
			auditLog.LogSystemdOperation("journal_vacuum", strings.Join(vacuumArgs, " "), result, err)
		}
		if err != nil {
			return fmt.Errorf("journal vacuum failed: %w", err)
		}

		fmt.Println()
		usageAfter := exec.Command("journalctl", "--disk-usage")
		usageAfter.Stdout = os.Stdout
		usageAfter.Stderr = os.Stderr
		usageAfter.Run()

		fmt.Println(S.Success("✅ Journal vacuumed"))
		return nil
	},
}

var dockerCmd = &cobra.Command{
	Use:   "docker",
	Short: "Clean Docker resources",
	Long:  "Clean unused Docker images, containers, volumes, and build cache using Docker CLI",
}

var dockerImagesCmd = &cobra.Command{
	Use:   docker.OperationImages,
	Short: "Remove unused Docker images",
	Run: func(cmd *cobra.Command, args []string) {
		spec, ok := docker.PruneSpecFor(docker.OperationImages)
		if !ok {
			fmt.Println("❌ Invalid Docker cleanup operation")
			return
		}
		auditLog, _ := audit.NewLogger()
		if auditLog != nil {
			defer auditLog.Close()
		}

		fmt.Println("🐳 Cleaning unused Docker images...")

		checkCmd := exec.Command("docker", "version")
		if err := checkCmd.Run(); err != nil {
			fmt.Println("❌ Docker is not installed or not running")
			if auditLog != nil {
				auditLog.LogDockerOperation(spec.AuditOperation, []string{}, "failed", err)
			}
			return
		}

		dfCmd := exec.Command("docker", "system", "df")
		dfCmd.Stdout = os.Stdout
		dfCmd.Stderr = os.Stderr
		dfCmd.Run()

		fmt.Printf("\n🗑️  Running: docker %s\n", strings.Join(spec.Args, " "))

		pruneCmd := exec.Command("docker", spec.Args...)
		pruneCmd.Stdout = os.Stdout
		pruneCmd.Stderr = os.Stderr

		err := pruneCmd.Run()
		if auditLog != nil {
			result := "success"
			if err != nil {
				result = "failed"
			}
			auditLog.LogDockerOperation(spec.AuditOperation, spec.Args, result, err)
		}

		if err != nil {
			fmt.Printf("❌ Failed to prune images: %v\n", err)
			return
		}

		fmt.Println("✅ Docker images cleaned successfully!")
	},
}

var dockerAllCmd = &cobra.Command{
	Use:   docker.OperationAll,
	Short: "Remove all unused Docker resources",
	Run: func(cmd *cobra.Command, args []string) {
		spec, ok := docker.PruneSpecFor(docker.OperationAll)
		if !ok {
			fmt.Println("❌ Invalid Docker cleanup operation")
			return
		}
		auditLog, _ := audit.NewLogger()
		if auditLog != nil {
			defer auditLog.Close()
		}

		fmt.Println("🐳 Cleaning all unused Docker resources...")

		checkCmd := exec.Command("docker", "version")
		if err := checkCmd.Run(); err != nil {
			fmt.Println("❌ Docker is not installed or not running")
			if auditLog != nil {
				auditLog.LogDockerOperation(spec.AuditOperation, []string{}, "failed", err)
			}
			return
		}

		fmt.Println("\n📊 Current Docker disk usage:")
		dfCmd := exec.Command("docker", "system", "df")
		dfCmd.Stdout = os.Stdout
		dfCmd.Stderr = os.Stderr
		dfCmd.Run()

		fmt.Printf("\n🗑️  Running: docker %s\n", strings.Join(spec.Args, " "))

		pruneCmd := exec.Command("docker", spec.Args...)
		pruneCmd.Stdout = os.Stdout
		pruneCmd.Stderr = os.Stderr

		err := pruneCmd.Run()
		if auditLog != nil {
			result := "success"
			if err != nil {
				result = "failed"
			}
			auditLog.LogDockerOperation(spec.AuditOperation, spec.Args, result, err)
		}

		if err != nil {
			fmt.Printf("❌ Failed to prune Docker resources: %v\n", err)
			return
		}

		fmt.Println("\n📊 Updated Docker disk usage:")
		dfCmd2 := exec.Command("docker", "system", "df")
		dfCmd2.Stdout = os.Stdout
		dfCmd2.Stderr = os.Stderr
		dfCmd2.Run()

		fmt.Println("\nDocker cleanup complete!")
	},
}

var duplicatesCmd = &cobra.Command{
	Use:   "duplicates",
	Short: "Find and remove duplicate files",
	Long:  "Scan for duplicate files based on content hashing and optionally remove them",
}

func printDuplicateScanErrors(result *duplicates.ScanResult) bool {
	if !result.Incomplete {
		return false
	}
	fmt.Printf("\n⚠️ Scan incomplete; results may omit files (%d issue(s)):\n", len(result.ScanErrors))
	for _, scanErr := range result.ScanErrors {
		fmt.Printf("  %s\n", scanErr)
	}
	return true
}

var duplicatesFindCmd = &cobra.Command{
	Use:   "find [paths...]",
	Short: "Find duplicate files",
	Long:  "Scan specified paths (or home directory) for duplicate files",
	Run: func(cmd *cobra.Command, args []string) {
		paths := args
		if len(paths) == 0 {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get home directory: %v\n", err)
				os.Exit(1)
			}
			paths = []string{homeDir}
		}

		minSize, _ := cmd.Flags().GetInt64("min-size")

		fmt.Println("🔍 Scanning for duplicate files...")
		fmt.Printf("📁 Paths: %v\n", paths)
		fmt.Printf("📏 Minimum size: %s\n\n", utils.HumanizeBytes(uint64(minSize)))

		opts := duplicates.ScanOptions{
			Paths:   paths,
			MinSize: minSize,
		}

		scanner := duplicates.NewScanner(opts)
		progressCh := make(chan duplicates.ScanProgress, 10)
		progressDone := make(chan struct{})

		// Show progress
		go func() {
			defer close(progressDone)
			for progress := range progressCh {
				if progress.Phase != "" {
					fmt.Printf("\r%s - %d files scanned (%s)",
						progress.Phase,
						progress.FilesScanned,
						utils.HumanizeBytes(uint64(progress.BytesScanned)))
				}
			}
		}()

		result, err := scanner.Scan(progressCh)
		<-progressDone
		if err != nil {
			fmt.Printf("\n❌ Error: %v\n", err)
			return
		}
		printDuplicateScanErrors(result)

		fmt.Printf("\n\n📊 Scan Results\n")
		fmt.Println("================")
		fmt.Printf("Files scanned: %d\n", result.FilesScanned)
		fmt.Printf("Duplicate groups: %d\n", len(result.Groups))
		fmt.Printf("Duplicate files: %d\n", result.TotalDupes)
		fmt.Printf("Wasted space: %s\n\n", utils.HumanizeBytes(uint64(result.WastedSpace)))

		if len(result.Groups) == 0 {
			if result.Incomplete {
				fmt.Println("No duplicates found in scanned paths.")
			} else {
				fmt.Println("No duplicate files found.")
			}
			return
		}

		// Show top 10 groups
		limit := 10
		if len(result.Groups) < limit {
			limit = len(result.Groups)
		}

		fmt.Printf("📋 Top %d Duplicate Groups (by wasted space):\n", limit)
		for i, group := range result.Groups[:limit] {
			fmt.Printf("\n%d. %d duplicates × %s = %s wasted\n",
				i+1,
				len(group.Files),
				utils.HumanizeBytes(uint64(group.Size)),
				utils.HumanizeBytes(uint64(group.TotalSize)))

			for j, file := range group.Files {
				var marker string
				if j == 0 {
					marker = "✓ " // Keep oldest
				} else {
					marker = "✗ " // Duplicate
				}
				fmt.Printf("  %s %s\n", marker, file.Path)
			}
		}

		fmt.Printf("\n💡 Use 'moonbit duplicates clean' to interactively remove duplicates\n")
	},
}

var duplicatesCleanCmd = &cobra.Command{
	Use:   "clean [paths...]",
	Short: "Remove duplicate files (interactive)",
	Long:  "Interactively scan and remove duplicate files. Scans specified paths (or home directory) and allows you to select which duplicates to remove.",
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := args
		if len(paths) == 0 {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			paths = []string{homeDir}
		}

		minSize, _ := cmd.Flags().GetInt64("min-size")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		fmt.Println(S.Header("🔍 Scanning for duplicate files..."))
		fmt.Printf("📁 Paths: %v\n", paths)
		fmt.Printf("📏 Minimum size: %s\n", utils.HumanizeBytes(uint64(minSize)))
		if dryRun {
			fmt.Println(S.Warning("🔒 DRY-RUN mode: No files will be deleted\n"))
		} else {
			fmt.Println(S.Error("⚠️  LIVE mode: Files will be permanently deleted\n"))
		}

		opts := duplicates.ScanOptions{
			Paths:   paths,
			MinSize: minSize,
		}

		scanner := duplicates.NewScanner(opts)
		progressCh := make(chan duplicates.ScanProgress, 10)
		progressDone := make(chan struct{})

		// Show progress
		go func() {
			defer close(progressDone)
			for progress := range progressCh {
				if progress.Phase != "" {
					fmt.Printf("\r%s - %d files scanned (%s)",
						progress.Phase,
						progress.FilesScanned,
						utils.HumanizeBytes(uint64(progress.BytesScanned)))
				}
			}
		}()

		result, err := scanner.Scan(progressCh)
		<-progressDone
		if err != nil {
			return fmt.Errorf("scan duplicates: %w", err)
		}
		if printDuplicateScanErrors(result) {
			// An unreadable subtree could contain the oldest copy; partial scans
			// cannot safely honor the keep-oldest rule.
			fmt.Println(S.Error("Refusing cleanup because the duplicate scan is incomplete."))
			return fmt.Errorf("duplicate scan is incomplete; cleanup refused")
		}

		fmt.Printf("\n\n%s\n", S.Header("📊 Scan Results"))
		fmt.Println(S.Separator())
		fmt.Printf("Files scanned: %d\n", result.FilesScanned)
		fmt.Printf("Duplicate groups: %d\n", len(result.Groups))
		fmt.Printf("Duplicate files: %d\n", result.TotalDupes)
		fmt.Printf("Wasted space: %s\n\n", utils.HumanizeBytes(uint64(result.WastedSpace)))

		if len(result.Groups) == 0 {
			fmt.Println(S.Success("✅ No duplicate files found."))
			return nil
		}

		// Interactive selection
		fmt.Println(S.Info("💡 For each duplicate group, the oldest file will be kept."))
		fmt.Println(S.Info("   All other duplicates in the group will be removed.\n"))

		var filesToRemove []duplicates.FileInfo
		totalSpaceToFree := int64(0)

		for i, group := range result.Groups {
			fmt.Printf("\n%s Group %d/%d: %d duplicates × %s = %s wasted\n",
				S.Bold("📦"),
				i+1,
				len(result.Groups),
				len(group.Files),
				utils.HumanizeBytes(uint64(group.Size)),
				utils.HumanizeBytes(uint64(group.TotalSize)))

			// Show files (oldest first, keep first one)
			groupFilesToRemove := []duplicates.FileInfo{}
			groupSpaceToFree := int64(0)
			for j, file := range group.Files {
				if j == 0 {
					fmt.Printf("  %s %s %s\n", S.Success("✓ KEEP"), S.Muted("(oldest)"), file.Path)
				} else {
					fmt.Printf("  %s %s\n", S.Error("✗ REMOVE"), file.Path)
					groupFilesToRemove = append(groupFilesToRemove, file)
					groupSpaceToFree += file.Size
				}
			}

			// Ask for confirmation for each group
			if !dryRun {
				fmt.Printf("\n%s Remove %d duplicate(s) from this group? [y/N]: ", S.Warning("⚠️"), len(group.Files)-1)
				var response string
				fmt.Scanln(&response)

				if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
					fmt.Println(S.Muted("  Skipped this group."))
					continue
				}
			}

			// Add to removal list if confirmed (or if dry-run, add all)
			filesToRemove = append(filesToRemove, groupFilesToRemove...)
			totalSpaceToFree += groupSpaceToFree
		}

		if len(filesToRemove) == 0 {
			fmt.Println(S.Info("\n💡 No files selected for removal."))
			return nil
		}

		// Final confirmation
		fmt.Printf("\n%s\n", S.Separator())
		fmt.Printf("%s Summary:\n", S.Bold("📋"))
		fmt.Printf("  Files to remove: %d\n", len(filesToRemove))
		fmt.Printf("  Space to free: %s\n", utils.HumanizeBytes(uint64(totalSpaceToFree)))

		if dryRun {
			fmt.Printf("\n%s DRY-RUN: Would remove %d files (%s)\n",
				S.Info("🔒"),
				len(filesToRemove),
				utils.HumanizeBytes(uint64(totalSpaceToFree)))
			fmt.Println(S.Info("   Run without --dry-run to actually delete files."))
			return nil
		}

		fmt.Printf("\n%s Remove %d duplicate file(s)? [y/N]: ", S.Error("⚠️"), len(filesToRemove))
		var finalResponse string
		fmt.Scanln(&finalResponse)

		if strings.ToLower(finalResponse) != "y" && strings.ToLower(finalResponse) != "yes" {
			fmt.Println(S.Muted("Cancelled. No files were removed."))
			return nil
		}

		// Keep this preflight for per-path user feedback; RemoveDuplicates repeats
		// validation at the destructive-operation boundary.
		var validatedPaths []duplicates.FileInfo
		for _, file := range filesToRemove {
			if err := validation.ValidateFilePath(file.Path); err != nil {
				fmt.Printf("%s Skipping invalid path: %s (%v)\n", S.Warning("⚠️"), file.Path, err)
				continue
			}
			validatedPaths = append(validatedPaths, file)
		}

		if len(validatedPaths) == 0 {
			fmt.Println(S.Error("❌ No valid paths to remove after validation."))
			return fmt.Errorf("no valid duplicate paths to remove")
		}

		if len(validatedPaths) < len(filesToRemove) {
			fmt.Printf("%s %d path(s) failed validation and were skipped.\n", S.Warning("⚠️"), len(filesToRemove)-len(validatedPaths))
		}

		// Remove duplicates
		fmt.Printf("\n%s Removing duplicate files...\n", S.Info("🗑️"))
		removed, freedSpace, errors := duplicates.RemoveDuplicates(validatedPaths)

		fmt.Printf("\n%s\n", S.Separator())
		if len(errors) > 0 {
			fmt.Printf("%s Completed with %d error(s):\n", S.Warning("⚠️"), len(errors))
			for _, errMsg := range errors {
				fmt.Printf("  %s\n", S.Error(errMsg))
			}
		}

		if removed > 0 {
			fmt.Printf("%s Successfully removed %d file(s)\n", S.Success("✅"), removed)
			fmt.Printf("%s Freed space: %s\n", S.Success("💾"), utils.HumanizeBytes(uint64(freedSpace)))
		}

		if removed < len(validatedPaths) {
			fmt.Printf("%s %d file(s) could not be removed\n", S.Warning("⚠️"), len(validatedPaths)-removed)
			return fmt.Errorf("removed %d of %d selected duplicate files", removed, len(validatedPaths))
		}
		return nil
	},
}

var pkgCmd = &cobra.Command{
	Use:   "pkg",
	Short: "Package manager cleanup operations",
	Long:  "Remove old kernels, orphaned packages, and unused dependencies using native package managers",
}

var pkgOrphansCmd = &cobra.Command{
	Use:   "orphans",
	Short: "Find and remove orphaned packages",
	Long:  "Detect and remove packages that were installed as dependencies but are no longer needed",
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !dryRun && !isRunningAsRoot() {
			fmt.Println("Removing orphaned packages requires root access")
			fmt.Println("")
			fmt.Println("Please run with sudo:")
			fmt.Println("  sudo moonbit pkg orphans --force")
			return fmt.Errorf("removing orphaned packages requires root access")
		}

		return removeOrphanedPackages(dryRun)
	},
}

var pkgKernelsCmd = &cobra.Command{
	Use:   "kernels",
	Short: "Remove old kernel versions",
	Long:  "Remove old kernel versions while keeping the current and one previous (Debian/Ubuntu only)",
	Run: func(cmd *cobra.Command, args []string) {
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !dryRun && !isRunningAsRoot() {
			fmt.Println("❌ Removing old kernels requires root access")
			fmt.Println("")
			fmt.Println("Please run with sudo:")
			fmt.Println("  sudo moonbit pkg kernels --force")
			os.Exit(1)
		}

		removeOldKernels(dryRun)
	},
}

func removeOrphanedPackages(dryRun bool) error {
	fmt.Println("🧹 Searching for orphaned packages...")

	auditLog, err := audit.NewLogger()
	if err != nil {
		fmt.Printf("⚠️  Warning: Failed to initialize audit log: %v\n", err)
	} else {
		defer auditLog.Close()
	}

	var listCmd *exec.Cmd
	var listedPackages []byte
	var removeArgs []string

	if _, err := exec.LookPath("pacman"); err == nil {
		fmt.Println("📦 Detected: Pacman (Arch/Manjaro)")
		output, err := exec.Command("pacman", "-Qtdq").Output()
		orphans := strings.Fields(string(output))
		if err != nil && len(orphans) == 0 {
			// pacman returns a nonzero status for an empty -Qtdq result. Confirm the
			// package database is readable before treating that as no orphans.
			_, verifyErr := exec.Command("pacman", "-Qq").Output()
			if verifyErr != nil {
				fmt.Printf("\n❌ Failed to list orphaned packages: %v\n", err)
				if auditLog != nil {
					auditLog.LogPackageOperation("remove_orphans", []string{"pacman", "-Qtdq"}, "failed", err)
				}
				return fmt.Errorf("pacman -Qtdq failed: %w", err)
			}
		}
		if err != nil && len(orphans) > 0 {
			fmt.Printf("\n❌ Failed to list orphaned packages: %v\n", err)
			if auditLog != nil {
				auditLog.LogPackageOperation("remove_orphans", []string{"pacman", "-Qtdq"}, "failed", err)
			}
			return fmt.Errorf("pacman -Qtdq failed: %w", err)
		}
		if len(orphans) == 0 {
			fmt.Println("\n✅ No orphaned packages found")
			if auditLog != nil {
				auditLog.LogPackageOperation("remove_orphans", []string{}, "success", nil)
			}
			return nil
		}
		listedPackages = []byte(strings.Join(orphans, "\n") + "\n")
		removeArgs = append([]string{"pacman", "-Rns"}, orphans...)
	} else if _, err := exec.LookPath("apt"); err == nil {
		fmt.Println("📦 Detected: APT (Debian/Ubuntu)")
		listCmd = exec.Command("apt-mark", "showauto")
		removeArgs = []string{"apt", "autoremove", "-y"}
	} else if _, err := exec.LookPath("dnf"); err == nil {
		fmt.Println("📦 Detected: DNF (Fedora/RHEL)")
		listCmd = exec.Command("dnf", "repoquery", "--extras")
		removeArgs = []string{"dnf", "autoremove", "-y"}
	} else if _, err := exec.LookPath("zypper"); err == nil {
		fmt.Println("📦 Detected: Zypper (openSUSE)")
		listCmd = exec.Command("zypper", "packages", "--orphaned")
		if !dryRun {
			fmt.Println("\n⚠️  Automatic zypper orphan removal is not implemented yet.")
			fmt.Println("   Review the orphan list above and remove packages manually.")
			if auditLog != nil {
				auditLog.LogPackageOperation("remove_orphans", []string{}, "failed", fmt.Errorf("automatic zypper orphan removal not implemented"))
			}
			return fmt.Errorf("automatic zypper orphan removal is not implemented")
		}
	} else {
		fmt.Println("❌ No supported package manager found")
		fmt.Println("   Supported: pacman, apt, dnf, zypper")
		if auditLog != nil {
			auditLog.LogPackageOperation("remove_orphans", []string{}, "failed", fmt.Errorf("no supported package manager"))
		}
		return fmt.Errorf("no supported package manager found")
	}

	if listCmd != nil || len(listedPackages) > 0 {
		fmt.Println("\n📋 Orphaned packages:")
		if listedPackages != nil {
			fmt.Print(string(listedPackages))
		} else {
			listCmd.Stdout = os.Stdout
			listCmd.Stderr = os.Stderr
			if err := listCmd.Run(); err != nil {
				fmt.Printf("\n❌ Failed to list orphaned packages: %v\n", err)
				if auditLog != nil {
					auditLog.LogPackageOperation("remove_orphans", removeArgs, "failed", err)
				}
				return fmt.Errorf("list orphaned packages: %w", err)
			}
		}
	}

	if dryRun {
		if len(removeArgs) > 0 {
			fmt.Printf("\nDRY RUN - would run: %s\n", commandLine("sudo", removeArgs...))
		} else {
			fmt.Println("\nAutomatic zypper orphan removal is not implemented; no removal command will run.")
		}
		fmt.Println("   Run with --force to actually remove orphaned packages")
		if auditLog != nil {
			auditLog.LogPackageOperation("remove_orphans", removeArgs, "dry-run", nil)
		}
		return nil
	}

	if len(removeArgs) > 0 {
		fmt.Printf("\n🗑️  Running: %s\n", commandLine("sudo", removeArgs...))
		removeCmd := exec.Command("sudo", removeArgs...)
		removeCmd.Stdout = os.Stdout
		removeCmd.Stderr = os.Stderr
		cmdErr := removeCmd.Run()

		if auditLog != nil {
			result := "success"
			if cmdErr != nil {
				result = "failed"
			}
			auditLog.LogPackageOperation("remove_orphans", removeArgs, result, cmdErr)
		}

		if cmdErr != nil {
			fmt.Printf("❌ Failed to remove orphaned packages: %v\n", cmdErr)
			return fmt.Errorf("remove orphaned packages: %w", cmdErr)
		}
		fmt.Println("\nOrphaned packages removed successfully!")
	}
	return nil
}

func removeOldKernels(dryRun bool) {
	fmt.Println("🧹 Checking for old kernel versions...")
	removeArgs := []string{"apt", "autoremove", "-y"}

	// Check if this is a Debian/Ubuntu system
	if _, err := exec.LookPath("apt"); err != nil {
		fmt.Println("❌ This feature only supports Debian/Ubuntu (APT-based systems)")
		fmt.Println("   Current system does not have APT package manager")
		return
	}

	// Get current kernel version
	unameCmd := exec.Command("uname", "-r")
	currentKernel, err := unameCmd.Output()
	if err != nil {
		fmt.Printf("❌ Could not detect current kernel: %v\n", err)
		return
	}

	currentVersion := string(currentKernel[:len(currentKernel)-1]) // Remove trailing newline
	fmt.Printf("📌 Current kernel: %s\n", currentVersion)

	// List installed kernels
	listCmd := exec.Command("dpkg", "--list")
	output, err := listCmd.Output()
	if err != nil {
		fmt.Printf("❌ Could not list installed packages: %v\n", err)
		return
	}

	fmt.Println("\n📋 Installed kernel packages:")
	lines := string(output)
	kernelCount := 0
	for _, line := range strings.Split(lines, "\n") {
		if strings.Contains(line, "linux-image-") || strings.Contains(line, "linux-headers-") {
			if strings.HasPrefix(line, "ii") {
				fmt.Println("  " + line)
				kernelCount++
			}
		}
	}

	if kernelCount == 0 {
		fmt.Println("  (none found)")
		return
	}

	if dryRun {
		fmt.Printf("\nDRY RUN - would run: %s\n", commandLine("sudo", removeArgs...))
		fmt.Println("   This will keep your current kernel and one previous version")
		fmt.Println("   Use --force to automatically run the command")
		return
	}

	fmt.Printf("\n🗑️  Running: %s\n", commandLine("sudo", removeArgs...))
	fmt.Println("   This will remove old kernels while keeping current + one previous")

	autoremoveCmd := exec.Command("sudo", removeArgs...)
	autoremoveCmd.Stdout = os.Stdout
	autoremoveCmd.Stderr = os.Stderr

	if err := autoremoveCmd.Run(); err != nil {
		fmt.Printf("❌ Failed to remove old kernels: %v\n", err)
		return
	}

	fmt.Println("\nOld kernels removed successfully!")
	fmt.Println("💡 Tip: Your system automatically marks old kernels for autoremoval")
}

func init() {
	rootCmd.Flags().BoolVar(&fromLauncher, "launcher", false,
		"Internal: started from a desktop launcher without a terminal")
	_ = rootCmd.Flags().MarkHidden("launcher")

	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(dockerCmd)
	rootCmd.AddCommand(journalCmd)
	rootCmd.AddCommand(duplicatesCmd)
	rootCmd.AddCommand(pkgCmd)

	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupRestoreCmd)

	journalCmd.AddCommand(journalVacuumCmd)
	journalVacuumCmd.Flags().String("size", "", "Shrink journal to this size (e.g. 500M, 1G)")
	journalVacuumCmd.Flags().String("time", "", "Drop journal entries older than this (e.g. 14d, 1month)")
	journalVacuumCmd.Flags().Bool("dry-run", true, "Preview only (default); pass --force to apply")
	journalVacuumCmd.Flags().Bool("force", false, "Actually vacuum the journal")

	dockerCmd.AddCommand(dockerImagesCmd)
	dockerCmd.AddCommand(dockerAllCmd)

	duplicatesCmd.AddCommand(duplicatesFindCmd)
	duplicatesCmd.AddCommand(duplicatesCleanCmd)

	pkgCmd.AddCommand(pkgOrphansCmd)
	pkgCmd.AddCommand(pkgKernelsCmd)

	duplicatesFindCmd.Flags().Int64("min-size", int64(duplicates.DefaultMinSize), "Minimum file size to consider (bytes)")
	duplicatesCleanCmd.Flags().Int64("min-size", int64(duplicates.DefaultMinSize), "Minimum file size to consider (bytes)")
	duplicatesCleanCmd.Flags().Bool("dry-run", false, "Preview only, don't delete files")

	pkgOrphansCmd.Flags().Bool("dry-run", true, "Preview orphaned packages without removing")
	pkgKernelsCmd.Flags().Bool("dry-run", true, "Preview old kernels without removing")

	// Add --force flag for pkg subcommands
	pkgOrphansCmd.Flags().Bool("force", false, "Actually remove orphaned packages")
	pkgKernelsCmd.Flags().Bool("force", false, "Actually remove old kernels")

	// Override dry-run when --force is used
	pkgOrphansCmd.PreRun = func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")
		if force {
			_ = cmd.Flags().Set("dry-run", "false")
		}
	}
	pkgKernelsCmd.PreRun = func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")
		if force {
			_ = cmd.Flags().Set("dry-run", "false")
		}
	}

	// Scan mode flags
	scanCmd.Flags().StringVarP(&scanMode, "mode", "m", "", "Scan mode: 'quick' (safe caches only) or 'deep' (all categories)")
	scanCmd.Flags().BoolVar(&scanNoPrompt, "no-prompt", false, "Do not prompt to clean after scanning")
	scanCmd.Flags().BoolVar(&listCategories, "list-categories", false, "List categories selected by the current filters and exit")
	scanCmd.Flags().StringSliceVar(&includeCategories, "include-category", nil, "Only include categories by name (repeat or comma-separate)")
	scanCmd.Flags().StringSliceVar(&excludeCategories, "exclude-category", nil, "Exclude categories by name (repeat or comma-separate)")
	scanCmd.Flags().BoolVar(&jsonOut, "json", false, "Emit newline-delimited JSON progress events on stdout (requires root; cancels when stdin closes)")

	cleanCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", true, "Preview only, don't delete files")
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "Actually delete files")
	cleanCmd.Flags().StringVarP(&scanMode, "mode", "m", "", "Clean mode: 'quick' (safe caches only) or 'deep' (all categories)")
	cleanCmd.Flags().StringSliceVar(&includeCategories, "include-category", nil, "Only clean categories by name (repeat or comma-separate)")
	cleanCmd.Flags().StringSliceVar(&excludeCategories, "exclude-category", nil, "Exclude categories by name (repeat or comma-separate)")
	cleanCmd.Flags().BoolVar(&jsonOut, "json", false, "Emit newline-delimited JSON progress events on stdout (cancels when stdin closes)")
}

// SetVersion wires build metadata injected via -ldflags into the root command,
// so `moonbit --version` reports what was actually built.
func SetVersion(version, buildTime string) {
	if version == "" {
		version = "dev"
	}
	if buildTime != "" {
		rootCmd.Version = fmt.Sprintf("%s (built %s)", version, buildTime)
	} else {
		rootCmd.Version = version
	}
}

func Execute() {
	if rootCmd.Version == "" {
		rootCmd.Version = "dev"
	}
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
