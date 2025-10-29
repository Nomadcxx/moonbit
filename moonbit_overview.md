Project Name: MoonBit
Description: A Go-based TUI application for system cleaning and privacy scrubbing, inspired by BleachBit. Focuses on interactive scanning, previewing, and selective deletion of temporary files, caches, logs, and application data on Linux (Arch-primary). Emphasizes safety via dry-runs, previews, and undo mechanisms. Integrates with existing TUI launcher/greeter via CLI hooks.
Version Target: v1.0 (MVP: Core scan/clean cycle).
Estimated Timeline: 4-6 weeks (80-120 hours, solo development).
Success Criteria:
Full system scan <20s on SSD.
90% unit test coverage.
Binary <15MB, installable via go install.
AUR submission ready.
Technical Architecture
Core Components
Scanning Engine: Recursive directory traversal using godirwalk.Walk for efficiency. Aggregates file metadata (size, mod time, path) via os.Stat. Filters by regex/extensions (e.g., .tmp, cache/). Risk scoring: Enum (Low, Medium, High) based on age (<7d = High) and path sensitivity.
TUI Layer: Bubble Tea model (type Model struct { Categories []Category; Selected []string; State State }) with views for list, table, progress. Lip Gloss for styling: lipgloss.Style{ Foreground: lipgloss.Color("9"), Border: lipgloss.RoundedBorder() }.
Cleaning Engine: File ops via os.Remove (simple) or syscall overwrites for shred (loop io.Copy with random bytes, 1-3 passes). Undo: archive/tar to create timestamped backups in XDG_DATA_HOME.
Configuration: TOML schema (config.toml):
[scan]
max_depth = 5
ignore_patterns = ["node_modules", ".git"]
[[categories]]
name = "Pacman Cache"
paths = ["/var/cache/pacman/pkg"]
risk = "Low"
shred = true
CLI Interface: Flags via cobra (optional dep): moonbit scan --category=browser --json for launcher integration.
Data Models
type Category struct {
Name       string
Paths      []string
Filters    []string // regex
Risk       RiskLevel
Size       uint64
FileCount  int
Files      []FileInfo // truncated top-N
Selected   bool
}
type FileInfo struct {
Path string
Size uint64
ModTime time.Time
}
Dependencies
Package: github.com/charmbracelet/bubbletea, Version: ^0.25.0, Purpose: TUI framework
Package: github.com/charmbracelet/lipgloss, Version: ^0.10.0, Purpose: Styling
Package: github.com/charmbracelet/bubbles, Version: ^0.19.0, Purpose: Widgets (list, table, progress)
Package: github.com/karrick/godirwalk, Version: ^1.17.0, Purpose: Fast dir traversal
Package: github.com/BurntSushi/toml, Version: ^1.4.0, Purpose: Config parsing
Package: github.com/dustin/go-humanize, Version: ^1.0.1, Purpose: Byte formatting
Package: github.com/spf13/afero, Version: ^1.11.0, Purpose: FS mocking for tests
go.mod init: go mod init github.com/yourname/moonbit.
Development Phases
Phase 1: Setup and Foundations (Week 1, 10-15 hrs)
Tasks:

Repository init: Create GitHub repo, add .gitignore (Go std), LICENSE (MIT).
Boilerplate: Implement main.go with Bubble Tea program:
type model struct { /* fields / }
func (m model) Init() tea.Cmd { return nil }
func (m model) Update(msg tea.Msg) (model, tea.Cmd) { / handle KeyMsg, WindowSizeMsg / return m, nil }
func (m model) View() string { return lipgloss.JoinVertical(lipgloss.Left, / views */) }
func main() { p := tea.NewProgram(model{}, tea.WithAltScreen()) }
Define cleaners: cleaners.go with 8-10 categories (pull from BleachBit: e.g., ~/.cache/thumbnails, /tmp/* >24h old).
Config loader: config.Load() returning Config struct; defaults to all categories enabled.
Basic CLI: go run . scan --json outputs map[string]Category.
Deliverables: Runnable TUI skeleton; 5 categories defined; make test passes (empty suite).
Milestone: git tag v0.1.0.

Phase 2: Scanning Engine (Weeks 1-2, 15-20 hrs)
Tasks:

Implement ScanCategories(cfg Config) <-chan ScanMsg: Goroutine per category; use godirwalk.WalkOptions{ Unsorted: true, FollowSymbolicLinks: false }.

Msg types: ScanProgress{ Path string; Bytes uint64 }, ScanComplete{ Category string; Stats Category }.
Filters: regexp.MustCompile(cfg.Filters...) on basename.


Arch-specific: Query exec.Command("pacman", "-Q") for installed pkgs; add paths like ~/.cache/yay/{pkg}.
Risk calc: if modTime.After(time.Now().Add(-724time.Hour)) { risk = High }.
Tests: TestScanPacmanCache(t *testing.T) with afero.MemMapFs; assert size >0.
Deliverables: Async scan func; JSON CLI output; benchmarks (go test -bench=. <10s/home).
Milestone: Integrate scan into TUI (progress updates via channel).

Phase 3: TUI Implementation (Weeks 2-3, 20-25 hrs)
Tasks:

Views:

CategoryList: bubbles.List with items Category.Name + " (" + humanize.Bytes(cat.Size) + ")"; toggle on Space.
FileTable: bubbles.Table with columns {Title: "Path", Width: 40}, {Title: "Size", Width: 10}; sort by size desc.
Preview: Custom modal lipgloss.Place(Width(termWidth), Height(20), lipgloss.Center, lipgloss.Center, content).


Key handling:
case msg := m.(tea.KeyMsg); msg.String():
case "ctrl+c", "q": return m, tea.Quit
case "s": return m, ScanCmd(selected)
case "/": m.searchActive = true; /* textinput init */
Styling: Theme struct type Theme struct { Primary lipgloss.Style; Danger lipgloss.Color("9") }; apply to risks.
Fuzzy search: Integrate pterm.Prompt for category filter.
Deliverables: Full UI flow (select > scan > preview); resize handling; help view (? key).
Milestone: git tag v0.2.0; demo GIF in README.

Phase 4: Cleaning and Safety (Weeks 3-4, 15-20 hrs)
Tasks:

CleanCategories(selected []Category, dry bool, passes int) <-chan CleanMsg:

For each file: If dry, log; else for i:=0; i<passes; i++ { randBytes := make([]byte, size); rand.Read(randBytes); os.WriteFile(path, randBytes, 0) }; then os.Remove.
Msg: CleanProgress{ Files int; Freed uint64 }.


Confirmation: Modal with lipgloss.Table summary; Y/N via key.
Undo: tar.Create(backupPath, files) pre-clean; tar.Extract(restorePath) on undo flag.
Tests: TestCleanDryRun asserts no deletions; TestShred100Files measures time.
Deliverables: Dry/real clean; undo restore; CLI --dry --undo.
Milestone: End-to-end cycle tested on real dir.

Phase 5: Testing, Polish, Integration (Weeks 4-5, 10-15 hrs)
Tasks:

E2E: Docker Arch container runs; golden file assertions for scans.
Polish: Warnings (>1GB: "Confirm?"); resume via saved state.
Arch tune: exec.Command("journalctl", "--vacuum-time=2weeks") integration.
Docs: Embed godoc comments; man page via go-md2man.
CI: GitHub Actions YAML for lint (golangci-lint), test, build.
Deliverables: 90% coverage; integration funcs (e.g., ScanSummaryJSON() string).
Milestone: git tag v0.3.0.

Phase 6: Release (Week 5+, 5-10 hrs)
Tasks:

Goreleaser config: .goreleaser.yml for Linux/ARM binaries.
AUR PKGBUILD: Template with pkgver=1.0.0, install to /usr/bin.
Announce: Changelog.md, release notes.
Deliverables: v1.0 binary on GH; AUR PR.

Risks and Mitigations
Risk: Dir traversal perf on /, Impact: High, Mitigation: Config max_depth=3; parallel goroutines with semaphore.
Risk: Permission errors, Impact: Medium, Mitigation: if err == syscall.EACCES { skip++ }; root warn in docs.
Risk: TUI redraw glitches, Impact: Low, Mitigation: tea.WithMouseCellMotion(); test on iTerm/Alacritty.
Testing Framework
Unit: go test ./... -cover (focus engine funcs).
Benchmark: go test -bench=BenchmarkScanHome.
Integration: Scripted runs in container: docker run -v $HOME:/home archlinux moonbit scan.
UI: Manual + e2e via expect (key simulation).
