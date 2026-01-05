package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/Nomadcxx/moonbit/internal/cleaner"
	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/duplicates"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	"github.com/Nomadcxx/moonbit/internal/session"
	"github.com/Nomadcxx/moonbit/internal/ui"
	"github.com/Nomadcxx/moonbit/internal/utils"
	"github.com/Nomadcxx/moonbit/internal/validation"
	"github.com/spf13/cobra"
)

var (
	dryRun   bool
	scanMode string // "quick", "deep", or "" (all)
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
		// Check for root access and re-exec with sudo if needed
		if !isRunningAsRoot() {
			reexecWithSudo()
			return
		}

		// Start Bubble Tea UI with MoonBit model
		ui.Start()
	},
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan system for cleanable files",
	Long:  "Scan the system for cleanable files and cache locations",
	Run: func(cmd *cobra.Command, args []string) {
		if !isRunningAsRoot() {
			reexecWithSudo()
			return
		}

		if scanMode != "" {
			if err := validation.ValidateMode(scanMode); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid scan mode: %v\n", err)
				os.Exit(1)
			}
		}

		if err := ScanAndSave(); err != nil {
			fmt.Fprintf(os.Stderr, "Scan failed: %v\n", err)
			os.Exit(1)
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
	Long:  "Clean files discovered in the last scan\n\nBy default, files are DELETED. Use --dry-run to preview first.",
	Run: func(cmd *cobra.Command, args []string) {
		if !isRunningAsRoot() && !dryRun {
			reexecWithSudo()
			return
		}

		if err := CleanSession(dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
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
	displayScanHeader(mode)

	cfg, s, err := initializeScanner()
	if err != nil {
		return err
	}

	categories, err := prepareScanCategories(mode, cfg)
	if err != nil {
		return err
	}

	totalSize, totalFiles, scanResults, err := scanAllCategories(s, categories)
	if err != nil {
		return err
	}

	if err := saveScanResults(totalSize, totalFiles, scanResults); err != nil {
		return err
	}

	displayScanResults(totalFiles, totalSize)
	return nil
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
func scanAllCategories(s *scanner.Scanner, categories []config.Category) (uint64, int, config.Category, error) {
	var totalSize uint64
	var totalFiles int
	var scanResults config.Category
	scanResults.Name = "Total Cleanable"
	scanResults.Files = []config.FileInfo{}

	for i, category := range categories {
		if !categoryPathExists(&category) {
			fmt.Printf("Skipping %s (not found)\n", category.Name)
			continue
		}

		fmt.Printf("Scanning %s (%d/%d)...\n", category.Name, i+1, len(categories))

		stats, err := scanSingleCategory(s, &category)
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
			continue
		}

		if stats != nil {
			var categorySize uint64
			for _, file := range stats.Files {
				categorySize += file.Size
			}

			fmt.Printf("  Found %d files (%s)\n",
				len(stats.Files),
				utils.HumanizeBytes(categorySize))

			totalSize += categorySize
			totalFiles += len(stats.Files)
			scanResults.Files = append(scanResults.Files, stats.Files...)
		}

		// Small delay between scans to prevent overwhelming the filesystem
		time.Sleep(ScanDelayBetweenCategories)
	}

	return totalSize, totalFiles, scanResults, nil
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
	}
	return false
}

// scanSingleCategory scans a single category and returns its stats
func scanSingleCategory(s *scanner.Scanner, category *config.Category) (*config.Category, error) {
	progressCh := make(chan scanner.ScanMsg, 10)
	go s.ScanCategory(context.Background(), category, progressCh)

	for msg := range progressCh {
		if msg.Complete != nil {
			return msg.Complete.Stats, nil
		}
		if msg.Error != nil {
			return nil, msg.Error
		}
	}

	return nil, fmt.Errorf("scan completed without results")
}

// saveScanResults creates and saves the session cache
func saveScanResults(totalSize uint64, totalFiles int, scanResults config.Category) error {
	cache := &config.SessionCache{
		ScanResults: &scanResults,
		TotalSize:   totalSize,
		TotalFiles:  totalFiles,
		ScannedAt:   time.Now(),
	}

	sessionMgr, err := session.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create session manager: %w", err)
	}

	if err := sessionMgr.Save(cache); err != nil {
		return fmt.Errorf("failed to save session cache: %w", err)
	}

	return nil
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

	fmt.Println(S.ASCIIHeader())
	fmt.Println(S.Header(fmt.Sprintf("%s Clean", modeLabel)))
	fmt.Println(S.Separator())

	// Load session cache
	sessionMgr, err := session.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create session manager: %w", err)
	}
	cache, err := sessionMgr.Load()
	if err != nil {
		return fmt.Errorf("no scan results found - run scan first: %w", err)
	}

	if cache.TotalFiles == 0 {
		fmt.Println("No files to clean.")
		return nil
	}

	// Load config and create cleaner
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Filter cache by mode if specified
	if scanMode != "" {
		cache = filterCacheByMode(cache, cfg, scanMode)
		if cache.TotalFiles == 0 {
			fmt.Printf("No files to clean in %s mode.\n", scanMode)
			return nil
		}
	}

	c := cleaner.NewCleaner(cfg)
	ctx := context.Background()

	if dryRun {
		fmt.Printf("DRY RUN - Would delete %d files (%s)\n",
			cache.TotalFiles, utils.HumanizeBytes(cache.TotalSize))

		// Show preview of what would be cleaned
		if cache.ScanResults != nil && len(cache.ScanResults.Files) > 0 {
			fmt.Println("\n📋 Files that would be deleted:")
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
	fmt.Printf("🗑️  Deleting %d files (%s)...\n",
		cache.TotalFiles, utils.HumanizeBytes(cache.TotalSize))

	progressCh := make(chan cleaner.CleanMsg, 10)
	go c.CleanCategory(ctx, cache.ScanResults, dryRun, progressCh)

	var deletedBytes uint64
	var deletedFiles int
	var errors []string

	// Process cleaning messages
	for msg := range progressCh {
		if msg.Progress != nil {
			// Progress update every 100 files
			if msg.Progress.FilesProcessed%100 == 0 && msg.Progress.FilesProcessed > 0 {
				fmt.Printf("   Progress: %d/%d files (%s)\n",
					msg.Progress.FilesProcessed,
					msg.Progress.TotalFiles,
					utils.HumanizeBytes(msg.Progress.BytesFreed))
			}
		}

		if msg.Complete != nil {
			deletedFiles = msg.Complete.FilesDeleted
			deletedBytes = msg.Complete.BytesFreed
			errors = msg.Complete.Errors

			if msg.Complete.BackupCreated {
				fmt.Printf("   📦 Backup created: %s\n", msg.Complete.BackupPath)
			}
			break
		}

		if msg.Error != nil {
			return fmt.Errorf("cleaning failed: %w", msg.Error)
		}
	}

	fmt.Println()
	fmt.Println(S.Header("Cleaning Complete"))
	fmt.Println(S.Separator())
	fmt.Printf("  %s %d\n", S.Bold("Files deleted:"), deletedFiles)
	fmt.Printf("  %s %s\n", S.Bold("Space freed:"), S.Success(utils.HumanizeBytes(deletedBytes)))

	if len(errors) > 0 {
		fmt.Printf("  %s %d files could not be deleted\n", S.Warning("Errors:"), len(errors))
		if len(errors) <= 5 {
			for _, err := range errors {
				fmt.Printf("      - %s\n", err)
			}
		}
	}

	fmt.Printf("   ⚡ Scan data cleared\n")

	if err := clearSessionCache(); err != nil && !os.IsNotExist(err) {
		fmt.Printf("   ⚠️  Warning: Could not clear cache file: %v\n", err)
	}

	return nil
}

// detectAvailableCategories dynamically finds available cleaning targets
func detectAvailableCategories() []config.Category {
	var categories []config.Category

	// Check for thumbnails directory
	if _, err := os.Stat(os.Getenv("HOME") + "/.cache/thumbnails"); err == nil {
		categories = append(categories, config.Category{
			Name:     "Thumbnail Cache",
			Paths:    []string{os.Getenv("HOME") + "/.cache/thumbnails"},
			Risk:     config.Low,
			Selected: true,
		})
	}

	return categories
}

func clearSessionCache() error {
	sessionMgr, err := session.NewManager()
	if err != nil {
		return err
	}
	return sessionMgr.Clear()
}

// filterCacheByMode filters cached files based on clean mode
func filterCacheByMode(cache *config.SessionCache, cfg *config.Config, mode string) *config.SessionCache {
	if mode == "" {
		return cache // No filtering
	}

	// Build map of category paths to risk levels
	riskByPath := make(map[string]config.RiskLevel)
	for _, cat := range cfg.Categories {
		for _, path := range cat.Paths {
			riskByPath[path] = cat.Risk
		}
	}

	// Filter files
	var filteredFiles []config.FileInfo
	var filteredSize uint64

	for _, file := range cache.ScanResults.Files {
		// Find which category this file belongs to
		risk := config.Low // Default to Low
		for catPath, catRisk := range riskByPath {
			if strings.HasPrefix(file.Path, catPath) {
				risk = catRisk
				break
			}
		}

		// Apply mode filter
		if mode == "quick" && risk != config.Low {
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
		},
		TotalSize:  filteredSize,
		TotalFiles: len(filteredFiles),
		ScannedAt:  cache.ScannedAt,
	}
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

		// Get full backup path
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			homeDir, _ := os.UserHomeDir()
			dataHome = filepath.Join(homeDir, ".local", "share")
		}
		backupPath := filepath.Join(dataHome, "moonbit", "backups", backupName)

		fmt.Printf("🔄 Restoring backup: %s\n", backupName)

		if err := cleaner.RestoreBackup(backupPath); err != nil {
			fmt.Printf("❌ Failed to restore backup: %v\n", err)
			return
		}

		fmt.Println("✅ Backup restored successfully!")
	},
}

var dockerCmd = &cobra.Command{
	Use:   "docker",
	Short: "Clean Docker resources",
	Long:  "Clean unused Docker images, containers, volumes, and build cache using Docker CLI",
}

var dockerImagesCmd = &cobra.Command{
	Use:   "images",
	Short: "Remove unused Docker images",
	Run: func(cmd *cobra.Command, args []string) {
		auditLog, _ := audit.NewLogger()
		if auditLog != nil {
			defer auditLog.Close()
		}

		fmt.Println("🐳 Cleaning unused Docker images...")

		checkCmd := exec.Command("docker", "version")
		if err := checkCmd.Run(); err != nil {
			fmt.Println("❌ Docker is not installed or not running")
			if auditLog != nil {
				auditLog.LogDockerOperation("prune_images", []string{}, "failed", err)
			}
			return
		}

		dfCmd := exec.Command("docker", "system", "df")
		dfCmd.Stdout = os.Stdout
		dfCmd.Stderr = os.Stderr
		dfCmd.Run()

		fmt.Println("\n🗑️  Running: docker image prune -a")

		pruneCmd := exec.Command("docker", "image", "prune", "-a", "-f")
		pruneCmd.Stdout = os.Stdout
		pruneCmd.Stderr = os.Stderr

		err := pruneCmd.Run()
		if auditLog != nil {
			result := "success"
			if err != nil {
				result = "failed"
			}
			auditLog.LogDockerOperation("prune_images", []string{"-a", "-f"}, result, err)
		}

		if err != nil {
			fmt.Printf("❌ Failed to prune images: %v\n", err)
			return
		}

		fmt.Println("✅ Docker images cleaned successfully!")
	},
}

var dockerAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Remove all unused Docker resources",
	Run: func(cmd *cobra.Command, args []string) {
		auditLog, _ := audit.NewLogger()
		if auditLog != nil {
			defer auditLog.Close()
		}

		fmt.Println("🐳 Cleaning all unused Docker resources...")

		checkCmd := exec.Command("docker", "version")
		if err := checkCmd.Run(); err != nil {
			fmt.Println("❌ Docker is not installed or not running")
			if auditLog != nil {
				auditLog.LogDockerOperation("prune_all", []string{}, "failed", err)
			}
			return
		}

		fmt.Println("\n📊 Current Docker disk usage:")
		dfCmd := exec.Command("docker", "system", "df")
		dfCmd.Stdout = os.Stdout
		dfCmd.Stderr = os.Stderr
		dfCmd.Run()

		fmt.Println("\n🗑️  Running: docker system prune -a --volumes")

		pruneCmd := exec.Command("docker", "system", "prune", "-a", "--volumes", "-f")
		pruneCmd.Stdout = os.Stdout
		pruneCmd.Stderr = os.Stderr

		err := pruneCmd.Run()
		if auditLog != nil {
			result := "success"
			if err != nil {
				result = "failed"
			}
			auditLog.LogDockerOperation("prune_all", []string{"-a", "--volumes", "-f"}, result, err)
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

		// Show progress
		go func() {
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
		if err != nil {
			fmt.Printf("\n❌ Error: %v\n", err)
			return
		}

		fmt.Printf("\n\n📊 Scan Results\n")
		fmt.Println("================")
		fmt.Printf("Files scanned: %d\n", result.FilesScanned)
		fmt.Printf("Duplicate groups: %d\n", len(result.Groups))
		fmt.Printf("Duplicate files: %d\n", result.TotalDupes)
		fmt.Printf("Wasted space: %s\n\n", utils.HumanizeBytes(uint64(result.WastedSpace)))

		if len(result.Groups) == 0 {
			fmt.Println("No duplicate files found.")
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
				marker := "  "
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

		// Show progress
		go func() {
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
		if err != nil {
			fmt.Printf("\n%s Error: %v\n", S.Error("❌"), err)
			os.Exit(1)
		}

		fmt.Printf("\n\n%s\n", S.Header("📊 Scan Results"))
		fmt.Println(S.Separator())
		fmt.Printf("Files scanned: %d\n", result.FilesScanned)
		fmt.Printf("Duplicate groups: %d\n", len(result.Groups))
		fmt.Printf("Duplicate files: %d\n", result.TotalDupes)
		fmt.Printf("Wasted space: %s\n\n", utils.HumanizeBytes(uint64(result.WastedSpace)))

		if len(result.Groups) == 0 {
			fmt.Println(S.Success("✅ No duplicate files found."))
			return
		}

		// Interactive selection
		fmt.Println(S.Info("💡 For each duplicate group, the oldest file will be kept."))
		fmt.Println(S.Info("   All other duplicates in the group will be removed.\n"))

		var filesToRemove []string
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
			groupFilesToRemove := []string{}
			groupSpaceToFree := int64(0)
			for j, file := range group.Files {
				if j == 0 {
					fmt.Printf("  %s %s %s\n", S.Success("✓ KEEP"), S.Muted("(oldest)"), file.Path)
				} else {
					fmt.Printf("  %s %s\n", S.Error("✗ REMOVE"), file.Path)
					groupFilesToRemove = append(groupFilesToRemove, file.Path)
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
			return
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
			return
		}

		fmt.Printf("\n%s Remove %d duplicate file(s)? [y/N]: ", S.Error("⚠️"), len(filesToRemove))
		var finalResponse string
		fmt.Scanln(&finalResponse)

		if strings.ToLower(finalResponse) != "y" && strings.ToLower(finalResponse) != "yes" {
			fmt.Println(S.Muted("Cancelled. No files were removed."))
			return
		}

		// Validate paths before deletion
		var validatedPaths []string
		for _, path := range filesToRemove {
			if err := validation.ValidateFilePath(path); err != nil {
				fmt.Printf("%s Skipping invalid path: %s (%v)\n", S.Warning("⚠️"), path, err)
				continue
			}
			validatedPaths = append(validatedPaths, path)
		}

		if len(validatedPaths) == 0 {
			fmt.Println(S.Error("❌ No valid paths to remove after validation."))
			return
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
		}
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
	Run: func(cmd *cobra.Command, args []string) {
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if !dryRun && !isRunningAsRoot() {
			fmt.Println("Removing orphaned packages requires root access")
			fmt.Println("")
			fmt.Println("Please run with sudo:")
			fmt.Println("  sudo moonbit pkg orphans --force")
			os.Exit(1)
		}

		removeOrphanedPackages(dryRun)
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

func removeOrphanedPackages(dryRun bool) {
	fmt.Println("🧹 Searching for orphaned packages...")

	auditLog, err := audit.NewLogger()
	if err != nil {
		fmt.Printf("⚠️  Warning: Failed to initialize audit log: %v\n", err)
	} else {
		defer auditLog.Close()
	}

	var listCmd, removeCmd *exec.Cmd

	if _, err := exec.LookPath("pacman"); err == nil {
		fmt.Println("📦 Detected: Pacman (Arch/Manjaro)")
		listCmd = exec.Command("pacman", "-Qtdq")
		if !dryRun {
			fmt.Println("\n🗑️  Running: sudo pacman -Rns $(pacman -Qtdq)")
			removeCmd = exec.Command("sudo", "pacman", "-Rns", "$(pacman -Qtdq)")
		}
	} else if _, err := exec.LookPath("apt"); err == nil {
		fmt.Println("📦 Detected: APT (Debian/Ubuntu)")
		listCmd = exec.Command("apt-mark", "showauto")
		if !dryRun {
			fmt.Println("\n🗑️  Running: sudo apt autoremove")
			removeCmd = exec.Command("sudo", "apt", "autoremove", "-y")
		}
	} else if _, err := exec.LookPath("dnf"); err == nil {
		fmt.Println("📦 Detected: DNF (Fedora/RHEL)")
		listCmd = exec.Command("dnf", "repoquery", "--extras")
		if !dryRun {
			fmt.Println("\n🗑️  Running: sudo dnf autoremove")
			removeCmd = exec.Command("sudo", "dnf", "autoremove", "-y")
		}
	} else if _, err := exec.LookPath("zypper"); err == nil {
		fmt.Println("📦 Detected: Zypper (openSUSE)")
		if !dryRun {
			fmt.Println("\n🗑️  Running: sudo zypper packages --orphaned")
			removeCmd = exec.Command("sudo", "zypper", "remove", "--clean-deps", "-y")
		}
	} else {
		fmt.Println("❌ No supported package manager found")
		fmt.Println("   Supported: pacman, apt, dnf, zypper")
		if auditLog != nil {
			auditLog.LogPackageOperation("remove_orphans", []string{}, "failed", fmt.Errorf("no supported package manager"))
		}
		return
	}

	if listCmd != nil {
		fmt.Println("\n📋 Orphaned packages:")
		listCmd.Stdout = os.Stdout
		listCmd.Stderr = os.Stderr
		if err := listCmd.Run(); err != nil {
			fmt.Printf("\n⚠️  Could not list orphaned packages (this is normal if there are none)\n")
		}
	}

	if dryRun {
		fmt.Println("\n💡 Dry-run mode: no packages removed")
		fmt.Println("   Run with --force to actually remove orphaned packages")
		if auditLog != nil {
			auditLog.LogPackageOperation("remove_orphans", []string{}, "dry-run", nil)
		}
		return
	}

	if removeCmd != nil {
		removeCmd.Stdout = os.Stdout
		removeCmd.Stderr = os.Stderr
		cmdErr := removeCmd.Run()

		if auditLog != nil {
			result := "success"
			if cmdErr != nil {
				result = "failed"
			}
			auditLog.LogPackageOperation("remove_orphans", []string{}, result, cmdErr)
		}

		if cmdErr != nil {
			fmt.Printf("❌ Failed to remove orphaned packages: %v\n", cmdErr)
			return
		}
		fmt.Println("\nOrphaned packages removed successfully!")
	}
}

func removeOldKernels(dryRun bool) {
	fmt.Println("🧹 Checking for old kernel versions...")

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
		fmt.Println("\n💡 Dry-run mode: run 'sudo apt autoremove' to remove old kernels")
		fmt.Println("   This will keep your current kernel and one previous version")
		fmt.Println("   Use --force to automatically run the command")
		return
	}

	fmt.Println("\n🗑️  Running: sudo apt autoremove")
	fmt.Println("   This will remove old kernels while keeping current + one previous")

	autoremoveCmd := exec.Command("sudo", "apt", "autoremove", "-y")
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
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(dockerCmd)
	rootCmd.AddCommand(duplicatesCmd)
	rootCmd.AddCommand(pkgCmd)

	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupRestoreCmd)

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
			cmd.Flags().Set("dry-run", "false")
		}
	}
	pkgKernelsCmd.PreRun = func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")
		if force {
			cmd.Flags().Set("dry-run", "false")
		}
	}

	// Scan mode flags
	scanCmd.Flags().StringVarP(&scanMode, "mode", "m", "", "Scan mode: 'quick' (safe caches only) or 'deep' (all categories)")

	cleanCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Preview only, don't delete files")
	cleanCmd.Flags().StringVarP(&scanMode, "mode", "m", "", "Clean mode: 'quick' (safe caches only) or 'deep' (all categories)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
