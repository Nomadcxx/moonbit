# App Cache Deep-Clean Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add conservative deep-clean app-cache targets without touching browser caches, model caches, Steam shader/download caches, Teams, or user session history.

**Architecture:** Introduce declarative app-cache rules that expand into regular `config.Category` values. Extend scan results to retain category provenance, then update filtering and tests so quick/deep behavior uses that provenance instead of guessing from path prefixes.

**Tech Stack:** Go, Cobra CLI, existing `internal/config`, `internal/scanner`, `internal/session`, `internal/cli`, and `internal/ui` packages.

---

### Task 1: Add Category Provenance To Scan Results

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/scanner/scanner.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/ui/ui.go`
- Test: `internal/scanner/scanner_test.go`
- Test: `internal/cli/root_test.go`

**Steps:**
1. Add `CategoryName string`, `CategoryRisk RiskLevel`, and `CategorySelected bool` fields to `config.FileInfo`.
2. Update scanner file appends so each file records the current category name, risk, and selected flag.
3. Update `filterCacheByMode` so quick mode keeps files with `CategoryRisk == config.Low` and `CategorySelected == true`.
4. Preserve category provenance when UI creates filtered categories for cleaning.
5. Add tests proving overlapping paths do not cause quick/deep filtering to depend on map iteration or raw path-prefix order.
6. Run `go test ./internal/config ./internal/scanner ./internal/cli ./internal/ui`.

### Task 2: Add App Cache Rule Expansion

**Files:**
- Create: `internal/config/app_caches.go`
- Test: `internal/config/app_caches_test.go`
- Modify: `internal/config/config.go`

**Steps:**
1. Define an `AppCacheRule` type with `Name`, `Roots`, `Leaves`, `ExcludePathPatterns`, `Risk`, `Selected`, and optional `MinAgeDays`.
2. Implement `AppCacheCategories(userHome string) []Category`.
3. Expand rules into path patterns like `<root>/<leaf>` rather than scanning whole app roots.
4. Add default categories from `AppCacheCategories` inside `DefaultConfig`.
5. Test that generated paths include intended safe cache leaves and exclude banned app families.

### Task 3: IDE And AI/Agent Cache Rules

**Files:**
- Modify: `internal/config/app_caches.go`
- Test: `internal/config/app_caches_test.go`

**Steps:**
1. Add IDE/editor rules for Cursor, VS Code, Code OSS, VSCodium, Antigravity, and Sublime Text.
2. Add Claude Desktop/CLI cache rules for `~/.cache/claude*` and safe `~/.config/Claude` cache leaves only.
3. Add opencode rules only for `~/.cache/opencode`, `~/.cache/oh-my-opencode`, `~/.local/share/opencode/log`, and age-limited `tool-output`/`snapshot`.
4. Add Cursor agent old-version cache as medium risk with a keep-latest strategy deferred unless the existing scanner can express it safely.
5. Test that opencode protected paths such as `storage`, `project`, `repos`, `plugins`, and config files are absent.

### Task 4: Wine, Bottles, And Lutris Rules

**Files:**
- Modify: `internal/config/app_caches.go`
- Test: `internal/config/app_caches_test.go`
- Test: `internal/scanner/scanner_test.go`

**Steps:**
1. Add Bottles temp/log rules for common native and Flatpak locations, including safe leaves under bottle prefixes such as `drive_c/windows/temp` and `drive_c/users/*/Temp`.
2. Add Lutris temp/log rules for `~/.cache/lutris`, `~/.local/share/lutris/logs`, and safe temp leaves inside known Lutris Wine prefixes where expressible.
3. Do not add rules for Wine prefixes wholesale, bottle definitions, runners, runtime assets, downloaded installers, registries, `AppData`, game directories, saves, shader caches, or Steam `compatdata`.
4. Mark Wine-prefix temp cleanup as medium risk and unselected by default unless it is a pure app cache/log directory.
5. Test fake Bottles and Lutris trees where temp/log files are included and save/config/runner/registry/shader/download paths are absent.

### Task 5: Non-Browser Electron And Flatpak Rules

**Files:**
- Modify: `internal/config/app_caches.go`
- Test: `internal/config/app_caches_test.go`
- Test: `internal/scanner/scanner_test.go`

**Steps:**
1. Add non-browser Electron app rules for Discord-family apps, GitHub Desktop, Signal restricted cache leaves, Proton Mail, Tidal, Goose, ChatGPT Nativefier, and Copilot Nativefier.
2. Do not add Teams.
3. Do not add browser profile expansion for Chromium, Firefox, Brave, Edge, Zen, or LibreWolf in this pass.
4. Add Flatpak rules for `~/.var/app/*/cache`, `~/.var/app/*/cache/tmp`, and `~/.var/app/*/cache/fontconfig`.
5. Exclude Flatpak Steam paths and shader/download cache leaves.
6. Test fake Flatpak app trees where Steam cache files are skipped and normal cache/tmp files are included.

### Task 6: Language/Build Cache Rules

**Files:**
- Modify: `internal/config/app_caches.go`
- Test: `internal/config/app_caches_test.go`

**Steps:**
1. Add low-risk build caches for bun, pnpm, yarn, deno, uv cache, node-gyp, clangd, ccache, golangci-lint, and TypeScript.
2. Add Playwright cache as medium risk and unselected by default because browser binaries can be expensive to re-download.
3. Do not add model caches such as HuggingFace, Torch, Chroma, ONNX, or local LLM stores.
4. Test that model-cache paths are not present in defaults.

### Task 7: End-To-End Verification And Gap Pass

**Files:**
- Modify as needed: `docs/plans/2026-06-02-app-cache-deep-clean-design.md`
- Modify as needed: `docs/plans/2026-06-02-systemd-automation-gap-design.md`

**Steps:**
1. Run `go test ./...`.
2. Run `go test ./... -race`.
3. Run `go vet ./...`.
4. Run `go build -o /tmp/moonbit-app-cache-build ./cmd`.
5. Run `go build -o /tmp/moonbit-installer-app-cache-build ./cmd/installer`.
6. Run a dry scan against the local environment and inspect whether excluded categories stay absent.
7. Produce a fresh feature gap analysis focused on remaining deep-clean opportunities and systemd dogfooding observability.
