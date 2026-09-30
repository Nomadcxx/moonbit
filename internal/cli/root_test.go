package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/duplicates"
	"github.com/Nomadcxx/moonbit/internal/scanner"
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

func TestOrphanPackagePreviewMatchesRemovalCommand(t *testing.T) {
	tests := []struct {
		manager string
		command string
	}{
		{"pacman", "sudo pacman -Rns orphan-a orphan-b"},
		{"apt", "sudo apt autoremove -y"},
		{"dnf", "sudo dnf autoremove -y"},
	}

	for _, tt := range tests {
		t.Run(tt.manager, func(t *testing.T) {
			binDir := t.TempDir()
			t.Setenv("PATH", binDir)
			t.Setenv("MOONBIT_HOME", t.TempDir())
			writeExecutable(t, filepath.Join(binDir, tt.manager), "#!/bin/sh\nprintf 'orphan-a\\norphan-b\\n'\n")
			if tt.manager == "apt" {
				writeExecutable(t, filepath.Join(binDir, "apt-mark"), "#!/bin/sh\nprintf 'orphan-a\\norphan-b\\n'\n")
			}

			output := captureStdout(t, func() { removeOrphanedPackages(true) })
			assert.Contains(t, output, tt.command)
		})
	}
}

func TestPacmanOrphanQueryFailureIsReported(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	t.Setenv("MOONBIT_HOME", t.TempDir())
	writeExecutable(t, filepath.Join(binDir, "pacman"), "#!/bin/sh\nif [ \"$1\" = -Qtdq ]; then echo 'database unavailable' >&2; exit 2; fi\nexit 2\n")

	var commandErr error
	output := captureStdout(t, func() { commandErr = removeOrphanedPackages(true) })
	require.Error(t, commandErr)
	assert.Contains(t, commandErr.Error(), "pacman")
	assert.NotContains(t, output, "No orphaned packages found")
	assert.Contains(t, output, "Failed to list orphaned packages")
}

func TestPacmanEmptyOrphanQueryIsNotReportedAsFailure(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	t.Setenv("MOONBIT_HOME", t.TempDir())
	writeExecutable(t, filepath.Join(binDir, "pacman"), "#!/bin/sh\nif [ \"$1\" = -Qtdq ]; then exit 1; fi\nif [ \"$1\" = -Qq ]; then printf 'installed-package\\n'; exit 0; fi\nexit 2\n")

	var commandErr error
	output := captureStdout(t, func() { commandErr = removeOrphanedPackages(true) })
	require.NoError(t, commandErr)
	assert.Contains(t, output, "No orphaned packages found")
}

func TestKernelPreviewMatchesExecutedCommand(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	t.Setenv("MOONBIT_HOME", t.TempDir())
	writeExecutable(t, filepath.Join(binDir, "apt"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "uname"), "#!/bin/sh\nprintf '6.1.0-test\\n'\n")
	writeExecutable(t, filepath.Join(binDir, "dpkg"), "#!/bin/sh\nprintf 'ii linux-image-6.0-old installed\\n'\n")

	output := captureStdout(t, func() { removeOldKernels(true) })
	assert.Contains(t, output, "sudo apt autoremove -y")
}

func TestCleanPreviewSeparatesDeletesFromTruncations(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "config")
	t.Setenv("HOME", home)
	t.Setenv("MOONBIT_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	originalMode := scanMode
	originalIncludes, originalExcludes := includeCategories, excludeCategories
	scanMode = ""
	includeCategories, excludeCategories = nil, nil
	defer func() {
		scanMode = originalMode
		includeCategories, excludeCategories = originalIncludes, originalExcludes
	}()

	deleteDir, truncateDir := t.TempDir(), t.TempDir()
	deletePath := filepath.Join(deleteDir, "package.cache")
	truncatePath := filepath.Join(truncateDir, "active.log")
	require.NoError(t, os.WriteFile(deletePath, []byte("cache"), 0600))
	require.NoError(t, os.WriteFile(truncatePath, []byte("log"), 0600))
	cfg := config.DefaultConfig()
	cfg.Categories = append(cfg.Categories,
		config.Category{Name: "Test Package Cache", Paths: []string{deleteDir}, Risk: config.Low},
		config.Category{Name: "Test Active Logs", Paths: []string{truncateDir}, Risk: config.Low, Action: config.ActionTruncate},
	)
	require.NoError(t, config.Save(cfg, filepath.Join(configHome, "moonbit", "config.toml")))
	sessionMgr, err := session.NewManager()
	require.NoError(t, err)
	require.NoError(t, sessionMgr.Save(&config.SessionCache{
		ScanResults: &config.Category{Files: []config.FileInfo{
			{Path: deletePath, Size: 5, CategoryName: "Test Package Cache"},
			{Path: truncatePath, Size: 3, CategoryName: "Test Active Logs", CategoryAction: config.ActionTruncate},
		}},
		TotalFiles: 2,
		TotalSize:  8,
		ScannedAt:  time.Now(),
	}))

	output := captureStdout(t, func() { require.NoError(t, CleanSession(true)) })
	assert.Contains(t, output, "DRY RUN - Would delete 1 file and truncate 1 file")
}

func TestDockerOutputMatchesExecutedPruneCommands(t *testing.T) {
	tests := []struct {
		name    string
		command *cobra.Command
		preview string
	}{
		{"images", dockerImagesCmd, "docker image prune -a -f"},
		{"all", dockerAllCmd, "docker system prune -a --volumes -f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			t.Setenv("PATH", binDir)
			t.Setenv("MOONBIT_HOME", t.TempDir())
			writeExecutable(t, filepath.Join(binDir, "docker"), "#!/bin/sh\nexit 0\n")
			output := captureStdout(t, func() { tt.command.Run(tt.command, nil) })
			assert.Contains(t, output, tt.preview)
		})
	}
}

func TestDuplicatesFindReportsIncompleteScan(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "readable.bin")
	require.NoError(t, os.WriteFile(file, make([]byte, duplicates.DefaultMinSize), 0644))
	missing := filepath.Join(root, "missing")

	output := captureStdout(t, func() {
		duplicatesFindCmd.Run(duplicatesFindCmd, []string{root, missing})
	})

	assert.Contains(t, output, "Scan incomplete")
	assert.Contains(t, output, missing)
	assert.Contains(t, output, "Files scanned: 1")
	assert.Contains(t, output, "No duplicates found in scanned paths.")
}

func TestDuplicatesCleanStopsOnIncompleteScan(t *testing.T) {
	root := t.TempDir()
	content := make([]byte, duplicates.DefaultMinSize)
	for _, name := range []string{"first.bin", "second.bin"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), content, 0644))
	}
	missing := filepath.Join(root, "missing")
	previousDryRun, err := duplicatesCleanCmd.Flags().GetBool("dry-run")
	require.NoError(t, err)
	require.NoError(t, duplicatesCleanCmd.Flags().Set("dry-run", "true"))
	t.Cleanup(func() {
		value := "false"
		if previousDryRun {
			value = "true"
		}
		_ = duplicatesCleanCmd.Flags().Set("dry-run", value)
	})

	var commandErr error
	output := captureStdout(t, func() {
		commandErr = duplicatesCleanCmd.RunE(duplicatesCleanCmd, []string{root, missing})
	})

	assert.Contains(t, output, "Scan incomplete")
	assert.Contains(t, output, "Refusing cleanup")
	assert.NotContains(t, output, "Would remove")
	require.Error(t, commandErr)
	assert.Contains(t, commandErr.Error(), "cleanup refused")
}

func TestScanAllCategoriesReportsPartialFailures(t *testing.T) {
	root := t.TempDir()
	workingPath := filepath.Join(root, "working")
	brokenPath := filepath.Join(root, "broken")
	require.NoError(t, os.Mkdir(workingPath, 0700))
	require.NoError(t, os.Mkdir(brokenPath, 0700))
	s := scanner.NewScannerWithFs(config.DefaultConfig(), failingStatFS{path: brokenPath})
	categories := []config.Category{
		{Name: "Working Cache", Paths: []string{workingPath}},
		{Name: "Broken Cache", Paths: []string{brokenPath}},
	}

	output := captureStdout(t, func() {
		_, _, _, err := scanAllCategories(s, categories)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Partial scan: 1 category failed")
	assert.Contains(t, output, "Broken Cache")
	assert.Contains(t, output, "injected stat failure")
}

type failingStatFS struct {
	path string
}

func (fs failingStatFS) Stat(path string) (os.FileInfo, error) {
	if filepath.Clean(path) == filepath.Clean(fs.path) {
		return nil, errors.New("injected stat failure")
	}
	return os.Stat(path)
}

func (fs failingStatFS) Walk(string, filepath.WalkFunc) error { return nil }

func (fs failingStatFS) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0700))
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer
	defer func() { os.Stdout = previous }()

	run()
	require.NoError(t, writer.Close())
	output, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return string(output)
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
