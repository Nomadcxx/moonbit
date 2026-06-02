package config

import "path/filepath"

type AppCacheRule struct {
	Name            string
	Roots           []string
	Leaves          []string
	ExcludePatterns []string
	Risk            RiskLevel
	Selected        bool
	MinAgeDays      int
}

func AppCacheCategories(userHome string) []Category {
	userHome = filepath.ToSlash(userHome)
	wineExcludes := wineProtectedPatterns()
	flatpakExcludes := append([]string{}, wineExcludes...)
	flatpakExcludes = append(flatpakExcludes,
		`(?i)com\.valvesoftware\.steam`,
		`(?i)/\.var/app/[^/]*(steam)[^/]*/`,
	)

	rules := []AppCacheRule{
		{
			Name: "IDE App Caches",
			Roots: []string{
				userHome + "/.config/Cursor",
				userHome + "/.config/Code",
				userHome + "/.config/Code - OSS",
				userHome + "/.config/VSCodium",
				userHome + "/.config/Antigravity",
				userHome + "/.config/sublime-text",
			},
			Leaves: []string{
				"Cache",
				"Code Cache",
				"GPUCache",
				"DawnGraphiteCache",
				"DawnWebGPUCache",
				"CachedData",
				"CachedExtensionVSIXs",
				"Crashpad",
				"logs",
			},
			Risk:     Low,
			Selected: false,
		},
		{
			Name:     "Cursor Compile Cache",
			Roots:    []string{userHome + "/.cache/cursor-compile-cache"},
			Leaves:   []string{""},
			Risk:     Low,
			Selected: false,
		},
		{
			Name: "AI Agent App Caches",
			Roots: []string{
				userHome + "/.config/Claude",
				userHome + "/.config/Goose",
				userHome + "/.config/chatgpt-nativefier-*",
				userHome + "/.config/microsoftcopilot-nativefier-*",
			},
			Leaves: []string{
				"Cache",
				"Code Cache",
				"GPUCache",
				"DawnGraphiteCache",
				"DawnWebGPUCache",
				"CachedData",
				"Crashpad",
				"logs",
			},
			Risk:     Low,
			Selected: false,
		},
		{
			Name: "Claude Tool Caches",
			Roots: []string{
				userHome + "/.cache/claude",
				userHome + "/.cache/claude-desktop",
				userHome + "/.cache/claude-desktop-bin",
				userHome + "/.cache/claude-cli-nodejs",
			},
			Leaves:   []string{""},
			Risk:     Low,
			Selected: false,
		},
		{
			Name: "opencode Caches",
			Roots: []string{
				userHome + "/.cache/opencode",
				userHome + "/.cache/oh-my-opencode",
				userHome + "/.local/share/opencode/log",
			},
			Leaves:   []string{""},
			Risk:     Low,
			Selected: false,
		},
		{
			Name: "opencode Old Tool Output",
			Roots: []string{
				userHome + "/.local/share/opencode/tool-output",
				userHome + "/.local/share/opencode/snapshot",
			},
			Leaves:     []string{""},
			Risk:       Medium,
			Selected:   false,
			MinAgeDays: 30,
		},
		{
			Name: "Electron App Caches",
			Roots: []string{
				userHome + "/.config/discord",
				userHome + "/.config/vesktop",
				userHome + "/.config/Equicord",
				userHome + "/.config/legcord",
				userHome + "/.config/GitHub Desktop",
				userHome + "/.config/Signal",
				userHome + "/.config/Signal Beta",
				userHome + "/.config/Proton Mail",
				userHome + "/.config/tidal-hifi",
			},
			Leaves: []string{
				"Cache",
				"Code Cache",
				"GPUCache",
				"DawnGraphiteCache",
				"DawnWebGPUCache",
				"CachedData",
				"Crashpad",
				"logs",
			},
			Risk:     Low,
			Selected: false,
		},
		{
			Name: "Bottles App Cache",
			Roots: []string{
				userHome + "/.cache/bottles",
				userHome + "/.var/app/com.usebottles.bottles/cache",
			},
			Leaves:   []string{""},
			Risk:     Low,
			Selected: false,
		},
		{
			Name: "Bottles Prefix Temp",
			Roots: []string{
				userHome + "/.local/share/bottles/bottles/*/drive_c/windows/temp",
				userHome + "/.local/share/bottles/bottles/*/drive_c/users/*/Temp",
				userHome + "/.var/app/com.usebottles.bottles/data/bottles/bottles/*/drive_c/windows/temp",
				userHome + "/.var/app/com.usebottles.bottles/data/bottles/bottles/*/drive_c/users/*/Temp",
			},
			Leaves:          []string{""},
			ExcludePatterns: wineExcludes,
			Risk:            Medium,
			Selected:        false,
		},
		{
			Name: "Lutris App Cache Logs",
			Roots: []string{
				userHome + "/.cache/lutris",
				userHome + "/.cache/lutris/tmp",
				userHome + "/.cache/lutris/lutris.log",
				userHome + "/.local/share/lutris/logs",
			},
			Leaves:     []string{""},
			Risk:       Low,
			Selected:   false,
			MinAgeDays: 7,
		},
		{
			Name: "Lutris Prefix Temp",
			Roots: []string{
				userHome + "/.local/share/lutris/prefixes/*/drive_c/windows/temp",
				userHome + "/.local/share/lutris/prefixes/*/drive_c/users/*/Temp",
				userHome + "/Games/*/drive_c/windows/temp",
				userHome + "/Games/*/drive_c/users/*/Temp",
			},
			Leaves:          []string{""},
			ExcludePatterns: wineExcludes,
			Risk:            Medium,
			Selected:        false,
		},
		{
			Name: "Generic Wine Prefix Temp",
			Roots: []string{
				userHome + "/.wine/drive_c/windows/temp",
				userHome + "/.wine/drive_c/users/*/Temp",
			},
			Leaves:          []string{""},
			ExcludePatterns: wineExcludes,
			Risk:            Medium,
			Selected:        false,
		},
		{
			Name: "Flatpak App Caches",
			Roots: []string{
				userHome + "/.var/app/*/cache",
				userHome + "/.var/app/*/cache/tmp",
				userHome + "/.var/app/*/cache/fontconfig",
			},
			Leaves:          []string{""},
			ExcludePatterns: flatpakExcludes,
			Risk:            Medium,
			Selected:        false,
		},
		{
			Name: "Language Build Caches",
			Roots: []string{
				userHome + "/.cache/bun",
				userHome + "/.cache/pnpm",
				userHome + "/.cache/yarn",
				userHome + "/.cache/deno",
				userHome + "/.cache/uv",
				userHome + "/.cache/node-gyp",
				userHome + "/.cache/clangd",
				userHome + "/.cache/ccache",
				userHome + "/.cache/golangci-lint",
				userHome + "/.cache/typescript",
			},
			Leaves:   []string{""},
			Risk:     Low,
			Selected: false,
		},
		{
			Name: "Playwright Cache",
			Roots: []string{
				userHome + "/.cache/ms-playwright",
			},
			Leaves:   []string{""},
			Risk:     Medium,
			Selected: false,
		},
	}

	var categories []Category
	for _, rule := range rules {
		categories = append(categories, Category{
			Name:            rule.Name,
			Paths:           expandAppCachePaths(rule.Roots, rule.Leaves),
			ExcludePatterns: rule.ExcludePatterns,
			Risk:            rule.Risk,
			Selected:        rule.Selected,
			ShredEnabled:    false,
			MinAgeDays:      rule.MinAgeDays,
		})
	}
	return categories
}

func wineProtectedPatterns() []string {
	return []string{
		`(?i)/(steamapps|compatdata|shadercache|shaders?|glcache|grshadercache|shaderhitcache)(/|$)`,
		`(?i)/(downloading|downloads?|installers?|runtime|runners?|dxvk|vkd3d)(/|$)`,
		`(?i)/drive_c/users/[^/]+/appdata(/|$)`,
		`(?i)/drive_c/(program files|program files \(x86\)|games)(/|$)`,
		`(?i)/drive_c/users/[^/]+/(saved games|documents)(/|$)`,
		`(?i)(^|/)(system|user|userdef)\.reg$`,
		`(?i)(^|/)bottles?\.ya?ml$`,
	}
}

func protectedCachePatterns() []string {
	return []string{
		`(?i)/(\.cache/)?huggingface(/|$)`,
		`(?i)/(\.cache/)?torch(/|$)`,
		`(?i)/(\.cache/)?chroma(/|$)`,
		`(?i)/(\.cache/)?onnx(/|$)`,
		`(?i)/(\.cache/)?mozilla(/|$)`,
		`(?i)/(\.cache/)?firefox(/|$)`,
		`(?i)/(\.cache/)?chromium(/|$)`,
		`(?i)/(\.cache/)?bravesoftware(/|$)`,
		`(?i)/(\.cache/)?google-chrome(/|$)`,
		`(?i)/(\.cache/)?zen(/|$)`,
		`(?i)/(\.cache/)?librewolf(/|$)`,
		`(?i)/(\.cache/)?microsoft-edge[^/]*(/|$)`,
		`(?i)/(\.cache/)?steam(/|$)`,
		`(?i)/\.steam(/|$)`,
		`(?i)/\.local/share/steam(/|$)`,
		`(?i)/\.var/app/com\.valvesoftware\.steam(/|$)`,
	}
}

func expandAppCachePaths(roots, leaves []string) []string {
	var paths []string
	for _, root := range roots {
		for _, leaf := range leaves {
			if leaf == "" {
				paths = append(paths, root)
				continue
			}
			paths = append(paths, root+"/"+leaf)
		}
	}
	return paths
}
