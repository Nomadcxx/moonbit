package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Nomadcxx/moonbit/internal/cleaner"
	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	"github.com/Nomadcxx/moonbit/internal/ui"
	"github.com/spf13/cobra"
)

var (
	dryRun bool
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
		// Start Bubble Tea UI with MoonBit model
		ui.Start()
	},
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan system for cleanable files",
	Long:  "Scan the system for cleanable files and cache locations",
	Run: func(cmd *cobra.Command, args []string) {
		// Check if we need sudo for system-wide scanning
		if requiresSudo() {
			fmt.Println("⚠️  WARNING: This scan requires sudo access for some locations.")
			fmt.Println("   Run with: sudo moonbit scan")
			fmt.Println("   Continuing with user-space scan only...")
			fmt.Println()
		}

		ScanAndSave()
	},
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean discovered files",
	Long:  "Clean files discovered in the last scan",
	Run: func(cmd *cobra.Command, args []string) {
		// Check if we need sudo for system-wide cleaning
		if requiresSudo() && !dryRun {
			fmt.Println("⚠️  WARNING: Cleaning system locations requires sudo.")
			fmt.Println("   Run with: sudo moonbit clean --force")
			fmt.Println("   Continuing with dry-run mode...")
			dryRun = true
		}

		if err := CleanSession(dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

// requiresSudo checks if any of the scan targets require root access
func requiresSudo() bool {
	systemPaths := []string{
		"/var/cache/pacman/pkg",
		"/var/tmp",
		"/var/log",
	}

	for _, path := range systemPaths {
		if _, err := os.Stat(path); err == nil {
			return true // At least one system path exists
		}
	}
	return false
}

// ScanAndSave runs a comprehensive scan and saves results to cache
func ScanAndSave() error {
	fmt.Println("🧹 MoonBit Comprehensive System Scan")
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
	fmt.Println("🧹 MoonBit Cleaning Session")
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

	c := cleaner.NewCleaner(cfg)
	ctx := context.Background()

	if dryRun {
		fmt.Printf("🔍 DRY RUN - Would delete %d files (%s)\n",
			cache.TotalFiles, humanizeBytes(cache.TotalSize))

		// Show preview of what would be cleaned
		fmt.Println("\n📋 Files that would be deleted:")
		for i, file := range cache.ScanResults.Files {
			if i >= 10 { // Limit preview
				fmt.Printf("   ... and %d more files\n", len(cache.ScanResults.Files)-10)
				break
			}
			fmt.Printf("   %s (%s)\n", file.Path, humanizeBytes(file.Size))
		}
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

func init() {
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(dockerCmd)

	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupRestoreCmd)

	dockerCmd.AddCommand(dockerImagesCmd)
	dockerCmd.AddCommand(dockerAllCmd)

	cleanCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", true, "Preview what would be deleted without actually deleting")

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
