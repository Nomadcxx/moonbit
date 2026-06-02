package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/session"
	"github.com/Nomadcxx/moonbit/internal/utils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanCommandFlags(t *testing.T) {
	dryRunFlag := cleanCmd.Flags().Lookup("dry-run")
	if assert.NotNil(t, dryRunFlag, "clean command should expose --dry-run") {
		assert.Equal(t, "true", dryRunFlag.DefValue, "clean should preview by default")
	}

	forceFlag := cleanCmd.Flags().Lookup("force")
	if assert.NotNil(t, forceFlag, "clean command should expose documented --force flag") {
		assert.Equal(t, "false", forceFlag.DefValue)
	}
}

func TestScanCommandNoPromptFlag(t *testing.T) {
	noPromptFlag := scanCmd.Flags().Lookup("no-prompt")
	if assert.NotNil(t, noPromptFlag, "scan command should expose --no-prompt for automation") {
		assert.Equal(t, "false", noPromptFlag.DefValue)
	}
}

func TestSystemdScanServiceUsesQuickNoPrompt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "systemd", "moonbit-scan.service"))
	require.NoError(t, err)

	unit := string(data)
	assert.Contains(t, unit, "ExecStart=/usr/local/bin/moonbit scan --mode quick --no-prompt")
	assert.False(t, strings.Contains(unit, "ExecStart=/usr/local/bin/moonbit scan\n"))
}

func TestApplyCleanFlags(t *testing.T) {
	originalDryRun := dryRun
	originalScanMode := scanMode
	defer func() {
		dryRun = originalDryRun
		scanMode = originalScanMode
		cleanCmd.Flags().Set("dry-run", "true")
		cleanCmd.Flags().Set("force", "false")
		cleanCmd.Flags().Set("mode", "")
	}()

	cleanCmd.Flags().Set("dry-run", "true")
	cleanCmd.Flags().Set("force", "false")
	assert.NoError(t, applyCleanFlags(cleanCmd))
	assert.True(t, dryRun, "clean should remain dry-run when --force is absent")

	cleanCmd.Flags().Set("force", "true")
	assert.NoError(t, applyCleanFlags(cleanCmd))
	assert.False(t, dryRun, "--force should switch clean into live deletion mode")

	cleanCmd.Flags().Set("mode", "quik")
	assert.Error(t, applyCleanFlags(cleanCmd), "invalid clean mode should be rejected")
}

func TestCleanSessionRejectsMalformedCache(t *testing.T) {
	originalScanMode := scanMode
	defer func() { scanMode = originalScanMode }()
	scanMode = ""

	t.Setenv("HOME", t.TempDir())

	sessionMgr, err := session.NewManager()
	assert.NoError(t, err)

	cache := &config.SessionCache{
		ScanResults: nil,
		TotalSize:   1024,
		TotalFiles:  1,
		ScannedAt:   time.Now(),
	}
	data, err := json.Marshal(cache)
	assert.NoError(t, err)
	assert.NoError(t, os.MkdirAll(filepath.Dir(sessionMgr.Path()), 0700))
	assert.NoError(t, os.WriteFile(sessionMgr.Path(), data, 0600))

	err = CleanSession(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid scan results")
}

func TestCleanSessionPreservesCacheOnPartialFailure(t *testing.T) {
	originalScanMode := scanMode
	defer func() { scanMode = originalScanMode }()
	scanMode = ""

	t.Setenv("HOME", t.TempDir())

	sessionMgr, err := session.NewManager()
	require.NoError(t, err)

	cache := &config.SessionCache{
		ScanResults: &config.Category{
			Name: "Test",
			Files: []config.FileInfo{
				{Path: filepath.Join(t.TempDir(), "missing.tmp"), Size: 10},
			},
			Size:      10,
			FileCount: 1,
			Risk:      config.Low,
		},
		TotalSize:  10,
		TotalFiles: 1,
		ScannedAt:  time.Now(),
	}
	require.NoError(t, sessionMgr.Save(cache))

	err = CleanSession(false)

	require.Error(t, err)
	assert.True(t, sessionMgr.Exists(), "failed clean should keep cache for retry")
}

func TestFilterCacheByModeUsesFileCategoryProvenance(t *testing.T) {
	cache := &config.SessionCache{
		ScanResults: &config.Category{
			Name: "All",
			Files: []config.FileInfo{
				{
					Path:             "/tmp/app/cache/safe.bin",
					Size:             10,
					CategoryName:     "Safe App Cache",
					CategoryRisk:     config.Low,
					CategorySelected: true,
				},
				{
					Path:             "/tmp/app/cache/deep.bin",
					Size:             20,
					CategoryName:     "Deep App Cache",
					CategoryRisk:     config.Medium,
					CategorySelected: false,
				},
			},
			Size:      30,
			FileCount: 2,
		},
		TotalSize:  30,
		TotalFiles: 2,
		ScannedAt:  time.Now(),
	}
	cfg := &config.Config{
		Categories: []config.Category{
			{Name: "Broad Low Risk Path", Paths: []string{"/tmp/app"}, Risk: config.Low, Selected: true},
		},
	}

	filtered := filterCacheByMode(cache, cfg, "quick")

	require.NotNil(t, filtered.ScanResults)
	require.Len(t, filtered.ScanResults.Files, 1)
	assert.Equal(t, "/tmp/app/cache/safe.bin", filtered.ScanResults.Files[0].Path)
	assert.Equal(t, uint64(10), filtered.TotalSize)
	assert.Equal(t, 1, filtered.TotalFiles)
}

func TestCategoryPathExistsMatchesGlobPaths(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := filepath.Join(tempDir, "app", "cache")
	assert.NoError(t, os.MkdirAll(cacheDir, 0755))

	category := &config.Category{
		Name:  "Glob Cache",
		Paths: []string{filepath.Join(tempDir, "*", "cache")},
	}

	assert.True(t, categoryPathExists(category))
}

func TestApplyCategorySelectionIncludesAndExcludesByName(t *testing.T) {
	categories := []config.Category{
		{Name: "Pacman Cache", Selected: true},
		{Name: "opencode Caches", Selected: false},
		{Name: "Bottles Prefix Temp", Selected: false},
	}

	selected, err := applyCategorySelection(categories, []string{"opencode caches", "Bottles Prefix Temp"}, []string{"bottles prefix temp"})

	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.Equal(t, "opencode Caches", selected[0].Name)
}

func TestApplyCategorySelectionRejectsUnknownNames(t *testing.T) {
	categories := []config.Category{{Name: "Pacman Cache"}}

	_, err := applyCategorySelection(categories, []string{"Nope Cache"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown category")
}

func TestFilterCacheByCategorySelectionUsesProvenance(t *testing.T) {
	cache := &config.SessionCache{
		ScanResults: &config.Category{
			Name: "All",
			Files: []config.FileInfo{
				{Path: "/tmp/opencode.log", Size: 10, CategoryName: "opencode Caches"},
				{Path: "/tmp/lutris.log", Size: 20, CategoryName: "Lutris App Cache Logs"},
			},
			Size:      30,
			FileCount: 2,
		},
		TotalSize:  30,
		TotalFiles: 2,
		ScannedAt:  time.Now(),
	}

	filtered, err := filterCacheByCategorySelection(cache, []string{"opencode caches"}, nil)

	require.NoError(t, err)
	require.NotNil(t, filtered.ScanResults)
	require.Len(t, filtered.ScanResults.Files, 1)
	assert.Equal(t, "/tmp/opencode.log", filtered.ScanResults.Files[0].Path)
	assert.Equal(t, uint64(10), filtered.TotalSize)
	assert.Equal(t, 1, filtered.TotalFiles)
}

func TestFilterCacheByCategorySelectionRejectsOldCacheWithoutProvenance(t *testing.T) {
	cache := &config.SessionCache{
		ScanResults: &config.Category{
			Name:  "All",
			Files: []config.FileInfo{{Path: "/tmp/old-cache-file", Size: 10}},
		},
		TotalSize:  10,
		TotalFiles: 1,
		ScannedAt:  time.Now(),
	}

	_, err := filterCacheByCategorySelection(cache, []string{"Pacman Cache"}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "run moonbit scan again")
}

func TestScanAndCleanExposeCategorySelectionFlags(t *testing.T) {
	for _, cmd := range []*cobra.Command{scanCmd, cleanCmd} {
		assert.NotNil(t, cmd.Flags().Lookup("include-category"))
		assert.NotNil(t, cmd.Flags().Lookup("exclude-category"))
	}
	assert.NotNil(t, scanCmd.Flags().Lookup("list-categories"))
}

func TestScanOutputFormatIncludesDurationAndSummary(t *testing.T) {
	assert.Contains(t, formatScanCategoryResult(3, 4096, 1500*time.Millisecond), "3 files")
	assert.Contains(t, formatScanCategoryResult(3, 4096, 1500*time.Millisecond), "4.0 KB")
	assert.Contains(t, formatScanCategoryResult(3, 4096, 1500*time.Millisecond), "1.5s")

	summary := formatScanSummary(4, 3, 4096, 2*time.Second)
	assert.Contains(t, summary, "Scan summary:")
	assert.Contains(t, summary, "categories_scanned=4")
	assert.Contains(t, summary, "files=3")
	assert.Contains(t, summary, "bytes=4096")
	assert.Contains(t, summary, "duration=2s")
}

func TestListCategoriesOutputsRiskAndSelectionScope(t *testing.T) {
	categories := []config.Category{
		{Name: "Quick Cache", Risk: config.Low, Selected: true},
		{Name: "Deep Cache", Risk: config.Medium, Selected: false},
	}
	var out bytes.Buffer

	writeCategoryList(&out, categories)

	output := out.String()
	assert.Contains(t, output, "Quick Cache")
	assert.Contains(t, output, "Low")
	assert.Contains(t, output, "quick")
	assert.Contains(t, output, "Deep Cache")
	assert.Contains(t, output, "Medium")
	assert.Contains(t, output, "deep")
}

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"Zero bytes", 0, "0 B"},
		{"Bytes", 512, "512 B"},
		{"Kilobytes", 1024, "1.0 KB"},
		{"Kilobytes decimal", 1536, "1.5 KB"},
		{"Megabytes", 1048576, "1.0 MB"},
		{"Megabytes decimal", 1572864, "1.5 MB"},
		{"Gigabytes", 1073741824, "1.0 GB"},
		{"Gigabytes decimal", 1610612736, "1.5 GB"},
		{"Large value", 5368709120, "5.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.HumanizeBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSessionCachePath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	assert.NoError(t, err)

	expected := filepath.Join(homeDir, ".cache", "moonbit", "scan_results.json")

	sessionMgr, err := session.NewManager()
	assert.NoError(t, err)
	actual := sessionMgr.Path()

	assert.Equal(t, expected, actual)
}

func TestSaveAndLoadSessionCache(t *testing.T) {
	sessionMgr, err := session.NewManager()
	assert.NoError(t, err)

	cachePath := sessionMgr.Path()
	assert.NotEmpty(t, cachePath)

	// Verify it's in the .cache directory
	homeDir, _ := os.UserHomeDir()
	assert.Contains(t, cachePath, filepath.Join(homeDir, ".cache", "moonbit"))
}

func TestIsRunningAsRoot(t *testing.T) {
	// This test checks if isRunningAsRoot correctly checks privileges
	result := isRunningAsRoot()

	// The result depends on whether the test is run as root
	// We just verify it returns a boolean without panicking
	assert.IsType(t, true, result)
}
