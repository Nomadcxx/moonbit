package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/scanner"
)

// SessionCache stores scan results for the current session
type SessionCache struct {
	ScanResults *config.Category `json:"scan_results"`
	TotalSize   uint64           `json:"total_size"`
	TotalFiles  int              `json:"total_files"`
	ScannedAt   time.Time        `json:"scanned_at"`
}

// ScanAndSave runs a comprehensive scan and saves results to cache
func ScanAndSave(cfg *config.Config) (*SessionCache, error) {
	fmt.Println("🧹 MoonBit Comprehensive System Scan")
	fmt.Println("=====================================")

	// Create scanner
	s := scanner.NewScanner(cfg)

	// Aggregate results from all selected categories
	var totalSize uint64
	var totalFiles int
	var scanResults config.Category
	scanResults.Name = "Total Cleanable"
	scanResults.Files = []config.FileInfo{}

	// Scan each category
	allCategories := []config.Category{}

	// Detect available categories dynamically
	availableCategories := detectAvailableCategories()
	for _, cat := range cfg.Categories {
		// Check if this category path exists
		exists := false
		for _, path := range cat.Paths {
			if _, err := os.Stat(path); err == nil {
				exists = true
				break
			}
		}
		if exists {
			allCategories = append(allCategories, cat)
		}
	}

	if len(availableCategories) > 0 {
		allCategories = append(allCategories, availableCategories...)
	}

	// Add detected categories
	for _, category := range availableCategories {
		fmt.Printf("📁 Found %s: %v\n", category.Name, category.Paths)
	}

	// Scan each category
	progressCh := make(chan scanner.ScanMsg, 100)

	for i, category := range allCategories {
		fmt.Printf("🔍 Scanning %s (%d/%d)...\n", category.Name, i+1, len(allCategories))

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

				break // Move to next category
			}
			if msg.Error != nil {
				fmt.Printf("   ❌ Error scanning %s: %v\n", category.Name, msg.Error)
				break
			}
		}

		// Small delay between scans
		time.Sleep(100 * time.Millisecond)
	}

	// Create session cache
	cache := &SessionCache{
		ScanResults: &scanResults,
		TotalSize:   totalSize,
		TotalFiles:  totalFiles,
		ScannedAt:   time.Now(),
	}

	// Save to cache file
	if err := saveSessionCache(cache); err != nil {
		return nil, fmt.Errorf("failed to save session cache: %w", err)
	}

	fmt.Println("\n📊 SCAN RESULTS")
	fmt.Println("================")
	fmt.Printf("🎯 Total cleanable files: %d\n", totalFiles)
	fmt.Printf("💾 Total space to save: %s\n", humanizeBytes(totalSize))
	fmt.Printf("⏱️  Scan completed at: %s\n", time.Now().Format("15:04:05"))

	return cache, nil
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

	// Actual cleaning
	fmt.Printf("🗑️  Deleting %d files (%s)...\n",
		cache.TotalFiles, humanizeBytes(cache.TotalSize))

	var deletedBytes uint64
	var deletedFiles int

	for _, file := range cache.ScanResults.Files {
		if err := os.Remove(file.Path); err != nil {
			fmt.Printf("   ⚠️  Failed to delete %s: %v\n", file.Path, err)
			continue
		}

		deletedBytes += file.Size
		deletedFiles++

		// Progress update every 100 files
		if deletedFiles%100 == 0 {
			fmt.Printf("   Progress: %d/%d files (%s)\n",
				deletedFiles, len(cache.ScanResults.Files),
				humanizeBytes(deletedBytes))
		}
	}

	fmt.Printf("\n✅ CLEANING COMPLETE!\n")
	fmt.Printf("   🗑️  Deleted: %d files\n", deletedFiles)
	fmt.Printf("   💾 Freed up: %s\n", humanizeBytes(deletedBytes))
	fmt.Printf("   ⚡ Scan data cleared\n")

	// Clear session cache
	os.Remove(getSessionCachePath())

	return nil
}

// detectAvailableCategories dynamically finds available cleaning targets
func detectAvailableCategories() []config.Category {
	var categories []config.Category

	// Check for Docker images (if docker installed)
	if _, err := os.Stat("/var/lib/docker"); err == nil {
		// This is a simplified check - real implementation would use docker API
		categories = append(categories, config.Category{
			Name:     "Docker Images",
			Paths:    []string{"/var/lib/docker"},
			Risk:     config.Low,
			Selected: false, // Default off - more risky
		})
	}

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

func saveSessionCache(cache *SessionCache) error {
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

func loadSessionCache() (*SessionCache, error) {
	data, err := os.ReadFile(getSessionCachePath())
	if err != nil {
		return nil, err
	}

	var cache SessionCache
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
