package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/cleaner"
	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/duplicates"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	"github.com/Nomadcxx/moonbit/internal/ui"
	"github.com/spf13/cobra"
)

var (
	dryRun   bool
	scanMode string // "quick", "deep", or "" (all)
)

var rootCmd = &cobra.Command{
	Use:   "moonbit",
	Short: "MoonBit – system cleaner TUI",
	Long: `MoonBit is a Go-based TUI application for system cleaning and privacy scrubbing.
It provides interactive scanning, previewing, and selective deletion of temporary files,
caches, logs, and application data on Linux (Arch-primary).

Features:
• Interactive TUI with beautiful theming (sysc-greet inspired)
• Safe dry-runs and undo mechanisms
• Parallel scanning with progress tracking
• Multiple cleaning categories (Pacman cache, temporary files, browser cache, etc.)
• JSON output for automation and launcher integration`,
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

		ScanAndSave()
	},
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean discovered files",
	Long:  "Clean files discovered in the last scan",
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
	fmt.Println("🔐 MoonBit requires root access for system-wide operations")
	fmt.Println("Please enter your password when prompted...")
	fmt.Println()

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
	modeLabel := "Comprehensive"
	if mode == "quick" {
		modeLabel = "Quick"
	} else if mode == "deep" {
		modeLabel = "Deep"
	}
	
	fmt.Printf("🧹 MoonBit %s System Scan\n", modeLabel)
	fmt.Println("=====================================")

	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create scanner
	s := scanner.NewScanner(cfg)

	// Aggregate results from all selected categories
	var totalSize uint64
	var totalFiles int
	var scanResults config.Category
	scanResults.Name = "Total Cleanable"
	scanResults.Files = []config.FileInfo{}

	// Check available categories dynamically
	availableCategories := detectAvailableCategories()

	// Create combined category list
	allCategories := append([]config.Category{}, cfg.Categories...)
	allCategories = append(allCategories, availableCategories...)

	for i, category := range allCategories {
		// Filter by mode
		if mode == "quick" && !category.Selected {
			continue // Quick mode: only Selected:true categories (safe, fast)
		}
		// Deep mode scans everything (no filter needed)
		
		// Check if this category path exists (for categories from config)
		exists := false
		for _, path := range category.Paths {
			if _, err := os.Stat(path); err == nil {
				exists = true
				break
			}
		}

		if exists || category.Name == "Thumbnail Cache" {
			fmt.Printf("🔍 Scanning %s (%d/%d)...\n", category.Name, i+1, len(allCategories))

			progressCh := make(chan scanner.ScanMsg, 10)
			go s.ScanCategory(context.Background(), &category, progressCh)

			// Collect results for this category
			for msg := range progressCh {
				if msg.Complete != nil {
					fmt.Printf("   ✅ Found %d files (%s) in %s\n",
						msg.Complete.Stats.FileCount,
						humanizeBytes(msg.Complete.Stats.Size),
						category.Name)

					// Add to totals
					totalSize += msg.Complete.Stats.Size
					totalFiles += msg.Complete.Stats.FileCount
					scanResults.Files = append(scanResults.Files, msg.Complete.Stats.Files...)
					break
				}
				if msg.Error != nil {
					fmt.Printf("   ❌ Error scanning %s: %v\n", category.Name, msg.Error)
					break
				}
			}
		} else {
			fmt.Printf("⚠️  Skipping %s (path not found)\n", category.Name)
		}

		// Small delay between scans
		time.Sleep(100 * time.Millisecond)
	}

	// Create session cache
	cache := &config.SessionCache{
		ScanResults: &scanResults,
		TotalSize:   totalSize,
		TotalFiles:  totalFiles,
		ScannedAt:   time.Now(),
	}

	// Save to cache file
	if err := saveSessionCache(cache); err != nil {
		return fmt.Errorf("failed to save session cache: %w", err)
	}

	fmt.Println("\n📊 SCAN RESULTS")
	fmt.Println("================")
	fmt.Printf("🎯 Total cleanable files: %d\n", totalFiles)
	fmt.Printf("💾 Total space to save: %s\n", humanizeBytes(totalSize))
	fmt.Printf("⏱️  Scan completed at: %s\n", time.Now().Format("15:04:05"))

	return nil
}

// CleanSession executes the actual cleaning based on session cache
func CleanSession(dryRun bool) error {
	modeLabel := "Standard"
	if scanMode == "quick" {
		modeLabel = "Quick"
	} else if scanMode == "deep" {
		modeLabel = "Deep"
	}
	
	fmt.Printf("🧹 MoonBit %s Cleaning Session\n", modeLabel)
	fmt.Println("===========================")

	// Load session cache
	cache, err := loadSessionCache()
	if err != nil {
		return fmt.Errorf("no scan results found - run scan first: %w", err)
	}

	if cache.TotalFiles == 0 {
		fmt.Println("✅ No files to clean!")
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
			fmt.Printf("✅ No files to clean in %s mode!\n", scanMode)
			return nil
		}
	}

	c := cleaner.NewCleaner(cfg)
	ctx := context.Background()

	if dryRun {
		fmt.Printf("🔍 DRY RUN - Would delete %d files (%s)\n",
			cache.TotalFiles, humanizeBytes(cache.TotalSize))

		// Show preview of what would be cleaned
		if cache.ScanResults != nil && len(cache.ScanResults.Files) > 0 {
			fmt.Println("\n📋 Files that would be deleted:")
			for i, file := range cache.ScanResults.Files {
				if i >= 10 { // Limit preview
					fmt.Printf("   ... and %d more files\n", len(cache.ScanResults.Files)-10)
					break
				}
				fmt.Printf("   %s (%s)\n", file.Path, humanizeBytes(file.Size))
			}
		}
		
		fmt.Println("\n💡 Use --force flag to actually delete files:")
		fmt.Println("   moonbit clean --force")
		return nil
	}

	// Actual cleaning using cleaner package
	fmt.Printf("🗑️  Deleting %d files (%s)...\n",
		cache.TotalFiles, humanizeBytes(cache.TotalSize))

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
					humanizeBytes(msg.Progress.BytesFreed))
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

	fmt.Printf("\n✅ CLEANING COMPLETE!\n")
	fmt.Printf("   🗑️  Deleted: %d files\n", deletedFiles)
	fmt.Printf("   💾 Freed up: %s\n", humanizeBytes(deletedBytes))

	if len(errors) > 0 {
		fmt.Printf("   ⚠️  Errors: %d files could not be deleted\n", len(errors))
		if len(errors) <= 5 {
			for _, err := range errors {
				fmt.Printf("      - %s\n", err)
			}
		}
	}

	fmt.Printf("   ⚡ Scan data cleared\n")

	// Clear session cache
	if err := os.Remove(getSessionCachePath()); err != nil && !os.IsNotExist(err) {
		// Log warning but don't fail - cleaning succeeded even if cache clear failed
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

// Session cache functions
func getSessionCachePath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".cache", "moonbit", "scan_results.json")
}

func saveSessionCache(cache *config.SessionCache) error {
	cacheDir := filepath.Dir(getSessionCachePath())
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(getSessionCachePath(), data, 0600)
}

func loadSessionCache() (*config.SessionCache, error) {
	data, err := os.ReadFile(getSessionCachePath())
	if err != nil {
		return nil, err
	}

	var cache config.SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
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

// humanizeBytes converts bytes to human-readable format
func humanizeBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
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
			fmt.Printf("❌ Failed to list backups: %v\n", err)
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
		fmt.Println("🐳 Cleaning unused Docker images...")

		// Check if docker is available
		checkCmd := exec.Command("docker", "version")
		if err := checkCmd.Run(); err != nil {
			fmt.Println("❌ Docker is not installed or not running")
			return
		}

		// Show current usage
		dfCmd := exec.Command("docker", "system", "df")
		dfCmd.Stdout = os.Stdout
		dfCmd.Stderr = os.Stderr
		dfCmd.Run()

		fmt.Println("\n🗑️  Running: docker image prune -a")

		pruneCmd := exec.Command("docker", "image", "prune", "-a", "-f")
		pruneCmd.Stdout = os.Stdout
		pruneCmd.Stderr = os.Stderr

		if err := pruneCmd.Run(); err != nil {
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
		fmt.Println("🐳 Cleaning all unused Docker resources...")

		// Check if docker is available
		checkCmd := exec.Command("docker", "version")
		if err := checkCmd.Run(); err != nil {
			fmt.Println("❌ Docker is not installed or not running")
			return
		}

		// Show current usage
		fmt.Println("\n📊 Current Docker disk usage:")
		dfCmd := exec.Command("docker", "system", "df")
		dfCmd.Stdout = os.Stdout
		dfCmd.Stderr = os.Stderr
		dfCmd.Run()

		fmt.Println("\n🗑️  Running: docker system prune -a --volumes")

		pruneCmd := exec.Command("docker", "system", "prune", "-a", "--volumes", "-f")
		pruneCmd.Stdout = os.Stdout
		pruneCmd.Stderr = os.Stderr

		if err := pruneCmd.Run(); err != nil {
			fmt.Printf("❌ Failed to prune Docker resources: %v\n", err)
			return
		}

		// Show new usage
		fmt.Println("\n📊 Updated Docker disk usage:")
		dfCmd2 := exec.Command("docker", "system", "df")
		dfCmd2.Stdout = os.Stdout
		dfCmd2.Stderr = os.Stderr
		dfCmd2.Run()

		fmt.Println("\n✅ Docker cleanup complete!")
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
			homeDir, _ := os.UserHomeDir()
			paths = []string{homeDir}
		}

		minSize, _ := cmd.Flags().GetInt64("min-size")

		fmt.Println("🔍 Scanning for duplicate files...")
		fmt.Printf("📁 Paths: %v\n", paths)
		fmt.Printf("📏 Minimum size: %s\n\n", humanizeBytes(uint64(minSize)))

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
						humanizeBytes(uint64(progress.BytesScanned)))
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
		fmt.Printf("Wasted space: %s\n\n", humanizeBytes(uint64(result.WastedSpace)))

		if len(result.Groups) == 0 {
			fmt.Println("✅ No duplicate files found!")
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
				humanizeBytes(uint64(group.Size)),
				humanizeBytes(uint64(group.TotalSize)))

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
	Use:   "clean",
	Short: "Remove duplicate files (interactive)",
	Long:  "Interactively select and remove duplicate files found in the last scan",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🚧 Interactive duplicate removal not yet implemented")
		fmt.Println("💡 For now, use 'moonbit duplicates find' to see duplicates")
		fmt.Println("   Manual removal recommended until interactive UI is complete")
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
			fmt.Println("❌ Removing orphaned packages requires root access")
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

	// Detect package manager
	var listCmd, removeCmd *exec.Cmd

	if _, err := exec.LookPath("pacman"); err == nil {
		// Arch Linux (Pacman)
		fmt.Println("📦 Detected: Pacman (Arch/Manjaro)")
		listCmd = exec.Command("pacman", "-Qtdq")
		if !dryRun {
			fmt.Println("\n🗑️  Running: sudo pacman -Rns $(pacman -Qtdq)")
			removeCmd = exec.Command("sudo", "pacman", "-Rns", "$(pacman -Qtdq)")
		}
	} else if _, err := exec.LookPath("apt"); err == nil {
		// Debian/Ubuntu (APT)
		fmt.Println("📦 Detected: APT (Debian/Ubuntu)")
		listCmd = exec.Command("apt-mark", "showauto")
		if !dryRun {
			fmt.Println("\n🗑️  Running: sudo apt autoremove")
			removeCmd = exec.Command("sudo", "apt", "autoremove", "-y")
		}
	} else if _, err := exec.LookPath("dnf"); err == nil {
		// Fedora/RHEL (DNF)
		fmt.Println("📦 Detected: DNF (Fedora/RHEL)")
		listCmd = exec.Command("dnf", "repoquery", "--extras")
		if !dryRun {
			fmt.Println("\n🗑️  Running: sudo dnf autoremove")
			removeCmd = exec.Command("sudo", "dnf", "autoremove", "-y")
		}
	} else if _, err := exec.LookPath("zypper"); err == nil {
		// openSUSE (Zypper)
		fmt.Println("📦 Detected: Zypper (openSUSE)")
		if !dryRun {
			fmt.Println("\n🗑️  Running: sudo zypper packages --orphaned")
			removeCmd = exec.Command("sudo", "zypper", "remove", "--clean-deps", "-y")
		}
	} else {
		fmt.Println("❌ No supported package manager found")
		fmt.Println("   Supported: pacman, apt, dnf, zypper")
		return
	}

	// List orphans
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
		return
	}

	// Remove orphans
	if removeCmd != nil {
		removeCmd.Stdout = os.Stdout
		removeCmd.Stderr = os.Stderr
		if err := removeCmd.Run(); err != nil {
			fmt.Printf("❌ Failed to remove orphaned packages: %v\n", err)
			return
		}
		fmt.Println("\n✅ Orphaned packages removed successfully!")
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

	fmt.Println("\n✅ Old kernels removed successfully!")
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

	duplicatesFindCmd.Flags().Int64("min-size", 1024, "Minimum file size to consider (bytes)")

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
	
	cleanCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", true, "Preview what would be deleted without actually deleting")
	cleanCmd.Flags().StringVarP(&scanMode, "mode", "m", "", "Clean mode: 'quick' (safe caches only) or 'deep' (all categories)")

	// Force flag should set dryRun to false
	var forceFlag bool
	cleanCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Actually delete files (disable dry-run)")
	cleanCmd.PreRun = func(cmd *cobra.Command, args []string) {
		if forceFlag {
			dryRun = false
		}
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
