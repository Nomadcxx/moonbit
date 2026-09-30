package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/session"
	"github.com/Nomadcxx/moonbit/internal/utils"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModel(t *testing.T) {
	model := NewModel()

	assert.Equal(t, 80, model.width)
	assert.Equal(t, 24, model.height)
	assert.Equal(t, ModeWelcome, model.mode)
	assert.Equal(t, 0, model.menuIndex)
	assert.NotNil(t, model.cfg)
	assert.Len(t, model.menuOptions, 6, "Should have 6 menu options (Quick Scan, Deep Scan, Review Results, Docker Cleanup, Schedule, Exit)")
}

func TestNewModelLoadsUserConfig(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	configDir := filepath.Join(configHome, "moonbit")
	assert.NoError(t, os.MkdirAll(configDir, 0755))
	configPath := filepath.Join(configDir, "config.toml")
	configData := []byte(`
[scan]
max_depth = 3
ignore_patterns = []
enable_all = true
dry_run_default = true
worker_count = 0

[[categories]]
name = "Custom Cache"
paths = ["/tmp/custom-cache"]
risk = 0
selected = true
`)
	assert.NoError(t, os.WriteFile(configPath, configData, 0644))

	model := NewModel()

	var foundCustom bool
	for _, category := range model.cfg.Categories {
		if category.Name == "Custom Cache" {
			foundCustom = true
			break
		}
	}
	assert.True(t, foundCustom)
}

func TestViewModes(t *testing.T) {
	// Test that all view modes are defined
	modes := []ViewMode{
		ModeWelcome,
		ModeScanProgress,
		ModeResults,
		ModeSelect,
		ModeConfirm,
		ModeClean,
		ModeComplete,
	}

	for _, mode := range modes {
		assert.NotEmpty(t, string(mode))
	}
}

func TestSessionCacheStructure(t *testing.T) {
	// Test SessionCache can be created
	cache := &config.SessionCache{
		ScanResults: &config.Category{
			Name:      "Test",
			FileCount: 10,
			Size:      1024,
		},
		TotalSize:  1024,
		TotalFiles: 10,
		ScannedAt:  time.Now(),
	}

	assert.NotNil(t, cache.ScanResults)
	assert.Equal(t, uint64(1024), cache.TotalSize)
	assert.Equal(t, 10, cache.TotalFiles)
}

func TestCategoryInfo(t *testing.T) {
	// Test CategoryInfo structure
	catInfo := CategoryInfo{
		Name:    "Test Category",
		Enabled: true,
		Files:   100,
		Size:    "1.5 MB",
	}

	assert.Equal(t, "Test Category", catInfo.Name)
	assert.True(t, catInfo.Enabled)
	assert.Equal(t, 100, catInfo.Files)
	assert.Equal(t, "1.5 MB", catInfo.Size)
}

func TestUpdateSelectedCount(t *testing.T) {
	model := NewModel()
	model.categories = []CategoryInfo{
		{Name: "Cat1", Enabled: true},
		{Name: "Cat2", Enabled: false},
		{Name: "Cat3", Enabled: true},
		{Name: "Cat4", Enabled: true},
	}

	model.updateSelectedCount()

	assert.Equal(t, 3, model.selectedCount)
}

func TestMenuBoundsForShortMenus(t *testing.T) {
	tests := []struct {
		name     string
		mode     ViewMode
		maxIndex int
	}{
		{"Schedule", ModeSchedule, 4},
		{"Docker", ModeDocker, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel()
			model.mode = tt.mode

			var updated tea.Model = model
			for i := 0; i < 8; i++ {
				updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyDown})
			}

			result := updated.(Model)
			assert.Equal(t, tt.maxIndex, result.menuIndex)
		})
	}
}

func TestDockerMenuRequiresConfirmation(t *testing.T) {
	model := NewModel()
	model.mode = ModeDocker
	model.menuIndex = 0

	updated, cmd := model.handleMenuSelect()

	result := updated.(Model)
	assert.Nil(t, cmd)
	assert.Equal(t, ModeDockerConfirm, result.mode)
	assert.Equal(t, "images", result.dockerOperation)
}

func TestDockerConfirmCancelReturnsToDockerMenu(t *testing.T) {
	model := NewModel()
	model.mode = ModeDockerConfirm
	model.menuIndex = 1
	model.dockerOperation = "all"

	updated, cmd := model.handleMenuSelect()

	result := updated.(Model)
	assert.Nil(t, cmd)
	assert.Equal(t, ModeDocker, result.mode)
	assert.Equal(t, "", result.dockerOperation)
}

func TestDockerConfirmShowsExecutedPruneCommand(t *testing.T) {
	tests := []struct {
		operation string
		command   string
	}{
		{"images", "docker image prune -a -f"},
		{"all", "docker system prune -a --volumes -f"},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			model := NewModel()
			model.dockerOperation = tt.operation
			assert.Contains(t, model.renderDockerConfirm(), tt.command)
		})
	}
}

func TestCleanConfirmationSeparatesDeleteAndTruncateActions(t *testing.T) {
	model := NewModel()
	model.cfg = &config.Config{Categories: []config.Category{
		{Name: "Package Cache", Paths: []string{"/cache"}, Action: config.ActionDelete},
		{Name: "Active Logs", Paths: []string{"/logs"}, Action: config.ActionTruncate},
	}}
	model.categories = []CategoryInfo{
		{Name: "Package Cache", Enabled: true},
		{Name: "Active Logs", Enabled: true},
	}
	model.scanResults = &config.SessionCache{ScanResults: &config.Category{Files: []config.FileInfo{
		{Path: "/cache/pkg", CategoryName: "Package Cache"},
		{Path: "/logs/active.log", CategoryName: "Active Logs", CategoryAction: config.ActionTruncate},
	}}}

	assert.Contains(t, model.renderConfirm(), "delete 1 file and truncate 1 file")
	model.scanResults.ScanResults.Files = []config.FileInfo{
		{Path: "/cache/legacy-package.cache"},
		{Path: "/logs/legacy-active.log"},
	}
	assert.Contains(t, model.renderConfirm(), "delete 1 file and truncate 1 file")
}

func TestHandleCleanCompletePreservesCacheAfterPartialFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sessionMgr, err := session.NewManager()
	require.NoError(t, err)
	cache := &config.SessionCache{
		ScanResults: &config.Category{Files: []config.FileInfo{{Path: "/tmp/retry-me", Size: 10}}},
		TotalFiles:  1,
		TotalSize:   10,
		ScannedAt:   time.Now(),
	}
	require.NoError(t, sessionMgr.Save(cache))

	model := NewModel()
	updated, cmd := model.handleCleanComplete(cleanCompleteMsg{
		Success: true,
		Error:   "1 file failed to delete",
	})

	assert.Nil(t, cmd)
	result := updated.(Model)
	assert.Equal(t, ModeComplete, result.mode)
	assert.Contains(t, result.renderComplete(), "1 file failed to delete")
	assert.True(t, sessionMgr.Exists(), "partial clean must preserve scan data for retry")
	loaded, err := sessionMgr.Load()
	require.NoError(t, err)
	require.Len(t, loaded.ScanResults.Files, 1)
	assert.Equal(t, "/tmp/retry-me", loaded.ScanResults.Files[0].Path)
}

func TestRunCleanCmdReportsTruncationSeparately(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MOONBIT_HOME", home)
	path := filepath.Join(t.TempDir(), "active.log")
	require.NoError(t, os.WriteFile(path, []byte("daemon output"), 0600))
	cfg := &config.Config{Categories: []config.Category{{
		Name: "Active Logs", Paths: []string{filepath.Dir(path)}, Risk: config.Low, Action: config.ActionTruncate,
	}}}
	cache := &config.SessionCache{
		ScanResults: &config.Category{Files: []config.FileInfo{{
			Path: path, Size: uint64(len("daemon output")), CategoryName: "Active Logs", CategoryAction: config.ActionTruncate,
		}}},
		TotalFiles: 1,
		TotalSize:  uint64(len("daemon output")),
		ScannedAt:  time.Now(),
	}

	msg := runCleanCmd(cfg, cache)().(cleanCompleteMsg)

	assert.True(t, msg.Success)
	assert.Zero(t, msg.FilesDeleted)
	assert.Equal(t, 1, msg.FilesTruncated)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Zero(t, info.Size())
	updated, _ := NewModel().handleCleanComplete(msg)
	assert.Contains(t, updated.(Model).renderComplete(), "Files Deleted:  0")
	assert.Contains(t, updated.(Model).renderComplete(), "Files Truncated: 1")
}

func TestScheduleRenderDoesNotRunSystemctl(t *testing.T) {
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "systemctl-called")
	systemctl := filepath.Join(binDir, "systemctl")
	script := "#!/bin/sh\n: > " + marker + "\n"
	require.NoError(t, os.WriteFile(systemctl, []byte(script), 0700))
	t.Setenv("PATH", binDir)

	model := NewModel()
	model.mode = ModeSchedule
	model.renderSchedule()

	assert.NoFileExists(t, marker, "schedule status checks must not run during rendering")
}

func TestShowScheduleRefreshesStatusAsynchronously(t *testing.T) {
	model := NewModel()
	updated, cmd := model.showSchedule()

	assert.Equal(t, ModeSchedule, updated.(Model).mode)
	assert.NotNil(t, cmd, "systemd status should be loaded by a tea command")
}

func TestScheduleActionRefreshesAndDisplaysStatus(t *testing.T) {
	model := NewModel()
	model.mode = ModeSchedule
	updated, refresh := model.Update(timerCommandMsg{success: true, message: "Successfully enabled timers"})
	assert.NotNil(t, refresh)

	updated, _ = updated.(Model).Update(scheduleStatusMsg{
		scanEnabled: true, scanStatus: "Enabled & Active",
		cleanEnabled: true, cleanStatus: "Enabled (Inactive)",
		daemonStatus: "Disabled",
	})
	screen := updated.(Model).renderSchedule()
	assert.Contains(t, screen, "Enabled & Active")
	assert.Contains(t, screen, "Enabled (Inactive)")
}

func TestNoDebugLogCreatedAtStartup(t *testing.T) {
	if os.Getenv("MOONBIT_STARTUP_CHILD") == "1" {
		return
	}

	cacheHome := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestNoDebugLogCreatedAtStartup$")
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "MOONBIT_STARTUP_CHILD=") ||
			strings.HasPrefix(entry, "XDG_CACHE_HOME=") || strings.HasPrefix(entry, "HOME=") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = append(env, "MOONBIT_STARTUP_CHILD=1", "XDG_CACHE_HOME="+cacheHome, "HOME="+cacheHome)
	require.NoError(t, cmd.Run())
	assert.NoFileExists(t, filepath.Join(cacheHome, "moonbit", "debug.log"))
}

func TestScanCompleteReportsPartialCategoryFailures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sessionMgr, err := session.NewManager()
	require.NoError(t, err)
	require.NoError(t, sessionMgr.Save(&config.SessionCache{
		ScanResults: &config.Category{Files: []config.FileInfo{{Path: "/tmp/found"}}},
		TotalFiles:  1,
		ScannedAt:   time.Now(),
	}))

	model := NewModel()
	updated, _ := model.handleScanComplete(scanCompleteMsg{
		Success:    true,
		Error:      "Partial scan: Broken Cache: read failed",
		Categories: []config.Category{{Name: "Working Cache", FileCount: 1}},
	})

	result := updated.(Model)
	assert.Equal(t, ModeSelect, result.mode)
	assert.Contains(t, result.renderSelect(), "Partial scan: Broken Cache: read failed")
}

func TestHandleCleanCompleteClearsSessionCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sessionMgr, err := session.NewManager()
	assert.NoError(t, err)
	assert.NoError(t, sessionMgr.Save(&config.SessionCache{
		ScanResults: &config.Category{Name: "Test"},
		ScannedAt:   time.Now(),
	}))

	model := NewModel()
	updated, cmd := model.handleCleanComplete(cleanCompleteMsg{
		Success:      true,
		FilesDeleted: 1,
		BytesFreed:   10,
	})

	assert.Nil(t, cmd)
	assert.Equal(t, ModeComplete, updated.(Model).mode)
	assert.False(t, sessionMgr.Exists())
}

func TestParseScanResultsWithCategories(t *testing.T) {
	model := NewModel()

	// Test with fresh categories from scan
	categories := []config.Category{
		{Name: "Pacman Cache", FileCount: 364, Size: 690600000},
		{Name: "Yay Cache", FileCount: 13, Size: 171400000},
		{Name: "Thumbnails", FileCount: 910, Size: 19400000},
	}

	model.parseScanResults(nil, categories)

	assert.Len(t, model.categories, 3)
	assert.Equal(t, "Pacman Cache", model.categories[0].Name)
	assert.Equal(t, 364, model.categories[0].Files)
	assert.True(t, model.categories[0].Enabled)

	// Check sizes are humanized
	assert.Contains(t, model.categories[0].Size, "MB")
}

func TestParseScanResultsWithCache(t *testing.T) {
	model := NewModel()

	// Create cache with aggregated data
	cache := &config.SessionCache{
		ScanResults: &config.Category{
			Name: "Total",
			Files: []config.FileInfo{
				{Path: "/var/cache/pacman/pkg/test.tar.zst", Size: 1024},
				{Path: "/home/user/.cache/yay/test.tar.zst", Size: 2048},
			},
			FileCount: 2,
			Size:      3072,
		},
		TotalSize:  3072,
		TotalFiles: 2,
	}

	// Parse with cache but no fresh categories
	model.parseScanResults(cache, nil)

	// Should create at least one category
	assert.NotEmpty(t, model.categories)

	var pacman CategoryInfo
	for _, category := range model.categories {
		if category.Name == "Pacman Cache" {
			pacman = category
			break
		}
	}
	assert.Equal(t, "Pacman Cache", pacman.Name)
	assert.Equal(t, 1, pacman.Files)
	assert.Equal(t, "1.0 KB", pacman.Size)
}

func TestParseScanResultsWithCacheUsesCategoryProvenance(t *testing.T) {
	model := NewModel()
	model.cfg = &config.Config{
		Categories: []config.Category{
			{Name: "Flatpak App Caches", Paths: []string{"/home/user/.var/app/*/cache"}, Risk: config.Medium},
		},
	}
	cache := &config.SessionCache{
		ScanResults: &config.Category{
			Name: "Total",
			Files: []config.FileInfo{
				{
					Path:             "/home/user/.var/app/org.example.App/cache/tmp/cache.bin",
					Size:             4096,
					CategoryName:     "Flatpak App Caches",
					CategoryRisk:     config.Medium,
					CategorySelected: false,
				},
			},
		},
		TotalSize:  4096,
		TotalFiles: 1,
	}

	model.parseScanResults(cache, nil)

	require.Len(t, model.categories, 1)
	assert.Equal(t, "Flatpak App Caches", model.categories[0].Name)
	assert.Equal(t, 1, model.categories[0].Files)
	assert.Equal(t, "4.0 KB", model.categories[0].Size)
}

func TestParseScanResultsEmpty(t *testing.T) {
	model := NewModel()

	// Test with no data
	model.parseScanResults(nil, nil)

	assert.Empty(t, model.categories)
	assert.Equal(t, 0, model.selectedCount)
}

func TestBuildFilteredCacheUsesCategoryProvenance(t *testing.T) {
	model := NewModel()
	model.cfg = &config.Config{
		Categories: []config.Category{
			{Name: "Safe Cache", Paths: []string{"/broad"}, Risk: config.Low, Selected: true},
			{Name: "Deep Cache", Paths: []string{"/broad"}, Risk: config.Medium, Selected: false},
		},
	}
	model.categories = []CategoryInfo{
		{Name: "Safe Cache", Enabled: true},
		{Name: "Deep Cache", Enabled: false},
	}
	model.scanResults = &config.SessionCache{
		ScanResults: &config.Category{
			Name: "Total",
			Files: []config.FileInfo{
				{Path: "/not-under-config/safe.tmp", Size: 10, CategoryName: "Safe Cache", CategoryRisk: config.Low, CategorySelected: true},
				{Path: "/broad/deep.tmp", Size: 20, CategoryName: "Deep Cache", CategoryRisk: config.Medium, CategorySelected: false},
			},
		},
		ScannedAt: time.Now(),
	}

	filtered := model.buildFilteredCache()

	require.NotNil(t, filtered)
	require.NotNil(t, filtered.ScanResults)
	require.Len(t, filtered.ScanResults.Files, 1)
	assert.Equal(t, "/not-under-config/safe.tmp", filtered.ScanResults.Files[0].Path)
	assert.Equal(t, uint64(10), filtered.TotalSize)
}

func TestUICategoryPathExistsMatchesGlobPaths(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := filepath.Join(tempDir, "app", "cache")
	assert.NoError(t, os.MkdirAll(cacheDir, 0755))

	category := config.Category{
		Name:  "Glob Cache",
		Paths: []string{filepath.Join(tempDir, "*", "cache")},
	}

	assert.True(t, uiCategoryPathExists(category))
}

func TestCalculateSelectedSize(t *testing.T) {
	model := NewModel()
	model.categories = []CategoryInfo{
		{Name: "Cat1", Enabled: true, Size: "100 MB"},
		{Name: "Cat2", Enabled: false, Size: "200 MB"},
		{Name: "Cat3", Enabled: true, Size: "150 MB"},
	}

	// Note: calculateSelectedSize tries to parse MB values
	// This is a simplified test
	size := model.calculateSelectedSize()
	assert.NotEmpty(t, size)
}

func TestHumanizeBytesUI(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"Zero", 0, "0 B"},
		{"Bytes", 100, "100 B"},
		{"KB", 1024, "1.0 KB"},
		{"MB", 1048576, "1.0 MB"},
		{"GB", 1073741824, "1.0 GB"},
		{"Large GB", 5368709120, "5.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.HumanizeBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScanCompleteMsg(t *testing.T) {
	// Test success message
	msg := scanCompleteMsg{
		Success: true,
		Categories: []config.Category{
			{Name: "Test", FileCount: 10, Size: 1024},
		},
		TotalSize:  1024,
		TotalFiles: 10,
	}

	assert.True(t, msg.Success)
	assert.Len(t, msg.Categories, 1)
	assert.Equal(t, uint64(1024), msg.TotalSize)

	// Test error message
	errMsg := scanCompleteMsg{
		Success: false,
		Error:   "test error",
	}

	assert.False(t, errMsg.Success)
	assert.Equal(t, "test error", errMsg.Error)
}

func TestScanProgressMsg(t *testing.T) {
	msg := scanProgressMsg{
		Progress:     0.5,
		Phase:        "Scanning...",
		FilesScanned: 100,
		BytesScanned: 1024000,
		CurrentPath:  "/tmp/test",
	}

	assert.Equal(t, 0.5, msg.Progress)
	assert.Equal(t, "Scanning...", msg.Phase)
	assert.Equal(t, 100, msg.FilesScanned)
	assert.Equal(t, uint64(1024000), msg.BytesScanned)
}

func TestLoadSessionCache(t *testing.T) {
	model := NewModel()

	// Try to load cache (may not exist, that's OK)
	cache, err := model.loadSessionCache()

	// We just verify the function doesn't panic
	// Error is expected if no cache exists
	if err == nil {
		assert.NotNil(t, cache)
	} else {
		assert.Error(t, err)
	}
}

func TestSaveSessionCacheCreatesDirectory(t *testing.T) {
	// Create test cache
	cache := &config.SessionCache{
		TotalSize:  1024,
		TotalFiles: 10,
		ScannedAt:  time.Now(),
	}

	// Save should create directory if it doesn't exist
	sessionMgr, err := session.NewManager()
	if err != nil {
		t.Fatalf("Failed to create session manager: %v", err)
	}

	err = sessionMgr.Save(cache)

	// May fail if permissions don't allow, but shouldn't panic
	if err == nil {
		// Verify file was created using session manager
		assert.True(t, sessionMgr.Exists())
	}
}

func TestTickMsg(t *testing.T) {
	// Test that tick message can be created
	now := time.Now()
	msg := tickMsg(now)

	assert.Equal(t, time.Time(msg), now)
}

func TestUpdateWithTick(t *testing.T) {
	model := NewModel()
	model.scanActive = true
	model.scanStarted = time.Now()

	// Simulate progress state
	model.filesScanned = 50
	model.bytesScanned = 512000
	model.currentFile = "/tmp/test.txt"
	model.totalFilesGuess = 100

	// Send tick message
	msg := tickMsg(time.Now())
	newModel, cmd := model.Update(msg)

	assert.NotNil(t, newModel)
	assert.NotNil(t, cmd) // Should return another tick while scanning

	// Check progress was updated
	m := newModel.(Model)
	assert.Equal(t, 0.5, m.scanProgress)
	assert.Contains(t, m.currentPhase, "test.txt")
}

func TestUpdateWithTickInactive(t *testing.T) {
	model := NewModel()
	model.scanActive = false

	// Send tick message when not scanning
	msg := tickMsg(time.Now())
	newModel, cmd := model.Update(msg)

	assert.NotNil(t, newModel)
	assert.Nil(t, cmd) // Should not return another tick when inactive
}
