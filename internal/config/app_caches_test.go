package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppCacheCategoriesIncludeConservativeTargets(t *testing.T) {
	home := filepath.ToSlash(t.TempDir())

	categories := AppCacheCategories(home)
	paths := strings.Join(categoryPaths(categories), "\n")

	assert.Contains(t, paths, home+"/.config/Cursor/Cache")
	assert.Contains(t, paths, home+"/.config/Cursor/Code Cache")
	assert.Contains(t, paths, home+"/.cache/cursor-compile-cache")
	assert.Contains(t, paths, home+"/.cache/claude")
	assert.Contains(t, paths, home+"/.cache/claude-desktop")
	assert.Contains(t, paths, home+"/.cache/opencode")
	assert.Contains(t, paths, home+"/.local/share/opencode/log")
	assert.Contains(t, paths, home+"/.cache/lutris")
	assert.Contains(t, paths, home+"/.local/share/lutris/logs")
	assert.Contains(t, paths, home+"/.var/app/com.usebottles.bottles/cache")
	assert.Contains(t, paths, home+"/.var/app/*/cache/tmp")
	assert.Contains(t, paths, home+"/.cache/pnpm")
	assert.Contains(t, paths, home+"/.cache/deno")
}

func TestAppCacheCategoriesExcludeInvasiveOrProtectedTargets(t *testing.T) {
	home := filepath.ToSlash(t.TempDir())

	categories := AppCacheCategories(home)
	namesAndPaths := strings.ToLower(strings.Join(categoryNamesAndPaths(categories), "\n"))

	blockedFragments := []string{
		"teams",
		"steam",
		"compatdata",
		"shader",
		"huggingface",
		"torch",
		"chroma",
		"onnx",
		"browser",
		"firefox",
		"chromium",
		"brave",
		"librewolf",
		"local storage",
		"session storage",
		"indexeddb",
		"/opencode/storage",
		"/opencode/project",
		"/opencode/repos",
		"/opencode/plugins",
		"/bottles/runners",
		"/lutris/runners",
		"appdata",
		"system.reg",
		"user.reg",
		"userdef.reg",
	}

	for _, fragment := range blockedFragments {
		assert.NotContains(t, namesAndPaths, fragment)
	}
}

func TestDefaultConfigDoesNotIncludeBrowserCacheCategory(t *testing.T) {
	cfg := DefaultConfig()
	namesAndPaths := strings.ToLower(strings.Join(categoryNamesAndPaths(cfg.Categories), "\n"))

	assert.NotContains(t, namesAndPaths, "browser cache")
	assert.NotContains(t, namesAndPaths, ".mozilla")
	assert.NotContains(t, namesAndPaths, "firefox")
	assert.NotContains(t, namesAndPaths, "chromium")
	assert.NotContains(t, namesAndPaths, "brave")
}

func TestDefaultUserCacheExcludesModelBrowserAndSteamTrees(t *testing.T) {
	cfg := DefaultConfig()
	userCache := findCategory(t, cfg.Categories, "User Cache")
	excludes := strings.ToLower(strings.Join(userCache.ExcludePatterns, "\n"))

	assert.Contains(t, excludes, "huggingface")
	assert.Contains(t, excludes, "torch")
	assert.Contains(t, excludes, "chroma")
	assert.Contains(t, excludes, "mozilla")
	assert.Contains(t, excludes, "chromium")
	assert.Contains(t, excludes, "brave")
	assert.Contains(t, excludes, "steam")
}

func TestAppCacheCategoriesCarrySafetyExclusions(t *testing.T) {
	home := filepath.ToSlash(t.TempDir())

	categories := AppCacheCategories(home)
	flatpak := findCategory(t, categories, "Flatpak App Caches")
	bottles := findCategory(t, categories, "Bottles Prefix Temp")
	lutris := findCategory(t, categories, "Lutris Prefix Temp")

	flatpakExcludes := strings.ToLower(strings.Join(flatpak.ExcludePatterns, "\n"))
	assert.Contains(t, flatpakExcludes, "steam")
	assert.Contains(t, flatpakExcludes, "shader")
	assert.Contains(t, flatpakExcludes, "download")

	for _, category := range []Category{bottles, lutris} {
		excludes := strings.ToLower(strings.Join(category.ExcludePatterns, "\n"))
		assert.Contains(t, excludes, "appdata")
		assert.Contains(t, excludes, "runner")
		assert.Contains(t, excludes, "shader")
		assert.Contains(t, excludes, "steam")
		assert.Contains(t, excludes, "reg")
	}
}

func TestAppCacheCategoriesAreDeepOnlyByDefault(t *testing.T) {
	home := filepath.ToSlash(t.TempDir())

	for _, category := range AppCacheCategories(home) {
		assert.False(t, category.Selected, "%s should be deep-only by default", category.Name)
	}
}

func categoryPaths(categories []Category) []string {
	var paths []string
	for _, category := range categories {
		paths = append(paths, category.Paths...)
	}
	return paths
}

func categoryNamesAndPaths(categories []Category) []string {
	var values []string
	for _, category := range categories {
		values = append(values, category.Name)
		values = append(values, category.Paths...)
		values = append(values, category.Filters...)
	}
	return values
}

func findCategory(t *testing.T, categories []Category, name string) Category {
	t.Helper()
	for _, category := range categories {
		if category.Name == name {
			return category
		}
	}
	t.Fatalf("category %q not found", name)
	return Category{}
}

func TestAppCacheCategoriesCoverAIHarnesses(t *testing.T) {
	home := filepath.ToSlash(t.TempDir())
	categories := AppCacheCategories(home)

	logs := strings.Join(findCategory(t, categories, "AI Agent CLI Logs & Temp").Paths, "\n")
	assert.Contains(t, logs, home+"/.codex/cache")
	assert.Contains(t, logs, home+"/.hermes/cache")
	assert.Contains(t, logs, home+"/.qwen/debug")

	worker := findCategory(t, categories, "Cursor Agent Worker Logs")
	assert.Equal(t, ActionTruncate, worker.Action, "the live worker holds its log open")

	sessions := findCategory(t, categories, "AI Agent Old Sessions")
	assert.Equal(t, Medium, sessions.Risk)
	assert.Equal(t, 30, sessions.MinAgeDays)
	assert.Contains(t, strings.Join(sessions.Paths, "\n"), home+"/.pi/agent/sessions")
	all := strings.Join(categoryPaths(categories), "\n")
	assert.NotContains(t, all, "/.gemini/tmp", "gemini tmp holds the user's named chat checkpoints")
	assert.NotContains(t, all, "/.qwen/tmp", "qwen (a gemini fork) keeps checkpoints there too")
}

func TestOldCLIVersionsKeepsActiveAndNewest(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	put := func(rel string, age time.Duration, dir bool) string {
		p := filepath.Join(home, rel)
		if dir {
			assert.NoError(t, os.MkdirAll(p, 0o755))
		} else {
			assert.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			assert.NoError(t, os.WriteFile(p, []byte("x"), 0o755))
		}
		assert.NoError(t, os.Chtimes(p, now.Add(-age), now.Add(-age)))
		return p
	}
	link := func(target, rel string) {
		p := filepath.Join(home, rel)
		assert.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		assert.NoError(t, os.Symlink(target, p))
	}

	// claude: active is pinned to a middle release, so oldest is the only junk.
	oldest := put(".local/share/claude/versions/1.0", 3*time.Hour, false)
	active := put(".local/share/claude/versions/2.0", 2*time.Hour, false)
	put(".local/share/claude/versions/3.0", time.Hour, false)
	link(active, ".local/bin/claude")

	// cursor-agent: a dangling launcher means the active release is unknown.
	put(".local/share/cursor-agent/versions/a", 2*time.Hour, true)
	put(".local/share/cursor-agent/versions/b", time.Hour, true)
	link(filepath.Join(home, "gone"), ".local/bin/cursor-agent")

	// copilot: no pointer, newest is active.
	copilotOld := put(".cache/copilot/pkg/linux-x64/1.0.86", 2*time.Hour, true)
	put(".cache/copilot/pkg/linux-x64/1.0.91", time.Hour, true)

	assert.ElementsMatch(t, []string{oldest, copilotOld}, oldCLIVersions(home))

	// A stray file newer than every copilot release must not displace the newest.
	put(".cache/copilot/pkg/linux-x64/install.lock", 0, false)
	assert.ElementsMatch(t, []string{oldest, copilotOld}, oldCLIVersions(home))

	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	start := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		if err := cmd.Start(); err != nil {
			t.Skipf("cannot start %s: %v", name, err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	}
	gone := func(p string) func() bool {
		return func() bool { return !slices.Contains(oldCLIVersions(home), p) }
	}

	// A session still executing an old binary release keeps it.
	bin, err := os.ReadFile(sleepBin)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(oldest, bin, 0o755))
	assert.NoError(t, os.Chtimes(oldest, now.Add(-3*time.Hour), now.Add(-3*time.Hour)))
	require.Contains(t, oldCLIVersions(home), oldest)
	start(oldest, "30")
	assert.Eventually(t, gone(oldest), 2*time.Second, 20*time.Millisecond)

	// A JS release runs under node: only an argument points into it.
	script := filepath.Join(copilotOld, "index.js")
	assert.NoError(t, os.WriteFile(script, nil, 0o644))
	assert.NoError(t, os.Chtimes(copilotOld, now.Add(-2*time.Hour), now.Add(-2*time.Hour)))
	require.Contains(t, oldCLIVersions(home), copilotOld)
	start("sh", "-c", "sleep 30; :", "sh", script)
	assert.Eventually(t, gone(copilotOld), 2*time.Second, 20*time.Millisecond)
}

func TestFilteredCategoryDoesNotCoverDetectedOne(t *testing.T) {
	detected := Category{Name: "Old AI CLI Versions", Paths: []string{"/home/u/.cache/copilot/pkg/linux-x64/1.0.86"}}
	filtered := Category{Name: "User Cache", Paths: []string{"/home/u/.cache"}, Filters: []string{`\.tmp$`}}
	plain := Category{Name: "Copilot", Paths: []string{"/home/u/.cache/copilot"}}

	assert.False(t, coveredByConfig(detected, []Category{filtered}))
	assert.True(t, coveredByConfig(detected, []Category{plain}))

	// On the same root the user's filter still suppresses the detected copy.
	thumbs := Category{Name: "Thumbnail Cache", Paths: []string{"/home/u/.cache/thumbnails"}}
	narrowed := Category{Name: "Thumbnails", Paths: []string{"/home/u/.cache/thumbnails"}, Filters: []string{`\.png$`}}
	assert.True(t, coveredByConfig(thumbs, []Category{narrowed}))
}
