package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
