package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ASCII header from ascii.txt
const asciiHeader = `
██▄▀█ ▄▀▀▀▄ ▄▀▀▀▄ ▄▀  █ █▀▀▀▄ ▀▀█▀▀ ▀▀█▀▀    ▄▀    ▄▀ 
█   █ █   █ █   █ █ ▀▄█ █▀▀▀▄   █     █    ▄▀    ▄▀   
▀   ▀  ▀▀▀   ▀▀▀  ▀   ▀ ▀▀▀▀  ▀▀▀▀▀   ▀   ▀     ▀    

`

// View modes for the TUI (following sysc-greet pattern)
type ViewMode string

const (
	ModeMain         ViewMode = "main"
	ModeScan         ViewMode = "scan"
	ModeClean        ViewMode = "clean"
	ModeDryRun       ViewMode = "dryrun"
	ModeSettings     ViewMode = "settings"
	ModeScanSettings ViewMode = "scan_settings"
	ModeThemes       ViewMode = "themes"
	ModeAbout        ViewMode = "about"
	ModeConfirm      ViewMode = "confirm"
)

// Scan state for real-time updates
type scanState struct {
	active    bool
	startedAt time.Time
	progress  scanner.ScanProgress
	completed bool
	error     error
	results   []scanResult
	// Progress tracking for UI
	currentPhase      string
	phases            []string
	currentPhaseIndex int
}

// Model represents the MoonBit TUI state
type Model struct {
	// Terminal dimensions
	width  int
	height int

	// Menu system with submenu support (sysc-greet pattern)
	menuOptions []string
	menuIndex   int
	mode        ViewMode

	// Settings submenu state
	showSettings    bool
	settingsSubmenu string
	settingsItems   []string
	settingsIndex   int

	// Current theme
	currentTheme string

	// Configuration and scanners
	cfg       *config.Config
	scanner   *scanner.Scanner
	ctx       context.Context
	scanState scanState

	// Scan summary data
	scanComplete bool
	scanResults  []scanResult
	totalSize    uint64
	totalFiles   int
	hasCache     bool // Whether we have scan results from CLI
}

// SessionCache mirrors the CLI cache structure
type SessionCache struct {
	ScanResults *config.Category `json:"scan_results"`
	TotalSize   uint64           `json:"total_size"`
	TotalFiles  int              `json:"total_files"`
	ScannedAt   time.Time        `json:"scanned_at"`
}

// Scan result for table display
type scanResult struct {
	Category string
	Files    int
	Size     string
	Duration string
	Status   string
}

// Initial model for MoonBit TUI
func initialModel() Model {
	cfg := config.DefaultConfig()

	m := Model{
		width:  80,
		height: 24,
		mode:   ModeMain,
		menuOptions: []string{
			"Scan System",
			"Clean Files",
			"Dry Run",
			"Show Results",
		},
		menuIndex:       0,
		currentTheme:    "moonbit",
		cfg:             cfg,
		scanner:         scanner.NewScanner(cfg),
		ctx:             context.Background(),
		showSettings:    false,
		settingsSubmenu: "",
		settingsItems: []string{
			"Scan Settings",
			"Themes",
			"About",
			"Quit",
		},
		settingsIndex: 0,
		scanState: scanState{
			phases: []string{
				"Initializing scan...",
				"Checking user cache...",
				"Scanning system directories...",
				"Collecting file statistics...",
				"Finalizing results...",
			},
			currentPhaseIndex: 0,
		},
	}

	// Check if we have existing scan results
	if cache, err := m.loadSessionCache(); err == nil {
		m.totalSize = cache.TotalSize
		m.totalFiles = cache.TotalFiles
		m.hasCache = true

		// Convert cache results to UI format
		for _, file := range cache.ScanResults.Files {
			m.scanResults = append(m.scanResults, scanResult{
				Category: "Total Cleanable",
				Files:    1,
				Size:     m.humanizeBytes(file.Size),
				Status:   "Ready to clean",
			})
		}
	}

	return m
}

// Implement tea.Model interface
func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			// Handle quit based on current mode
			if m.mode == ModeMain {
				return m, tea.Quit
			} else {
				// Return to main menu from other modes
				m.mode = ModeMain
				m.menuIndex = 0
			}
		case "f1":
			// Toggle settings popup
			m.showSettings = !m.showSettings
			return m, nil
		case "esc":
			// Handle ESC based on current state
			if m.showSettings {
				m.showSettings = false
				m.settingsSubmenu = ""
				return m, nil
			} else if m.mode != ModeMain && m.settingsSubmenu == "" {
				m.mode = ModeMain
				m.menuIndex = 0
				return m, nil
			} else if m.settingsSubmenu != "" {
				// Return from settings submenu
				m.settingsSubmenu = ""
				return m, nil
			}
		case "up":
			if m.showSettings {
				if m.settingsIndex > 0 {
					m.settingsIndex--
				}
			} else if m.menuIndex > 0 {
				m.menuIndex--
			}
		case "down":
			if m.showSettings {
				if m.settingsIndex < len(m.settingsItems)-1 {
					m.settingsIndex++
				}
			} else if m.menuIndex < len(m.menuOptions)-1 {
				m.menuIndex++
			}
		case "enter", " ":
			// Handle menu selection
			if m.showSettings && m.settingsSubmenu == "" {
				// Handle main settings menu
				switch m.settingsIndex {
				case 0: // Scan Settings
					return m.navigateToScanSettings()
				case 1: // Themes
					return m.navigateToThemes()
				case 2: // About
					m.mode = ModeAbout
					m.showSettings = false
				case 3: // Quit
					return m, tea.Quit
				}
			} else if m.settingsSubmenu == "" && m.mode == ModeScan {
				// In scan mode, Enter starts scanning
				if !m.scanState.active && len(m.scanState.results) == 0 {
					return m.startActualScan()
				}
			} else if m.settingsSubmenu == "" && m.mode == ModeDryRun {
				// In dry run mode, Enter starts scanning if no results
				if !m.scanState.active && len(m.scanState.results) == 0 {
					return m.startActualScan()
				}
			} else if m.settingsSubmenu == "" {
				// Handle main menu selection
				switch m.menuIndex {
				case 0: // Scan System
					return m.navigateToScanMode()
				case 1: // Clean Files
					return m.handleCleanFiles()
				case 2: // Dry Run
					return m.handleDryRun()
				case 3: // Show Results
					return m.handleShowResults()
				}
			}
		case "r":
			// Reset to main menu
			m.mode = ModeMain
			m.menuIndex = 0
			m.showSettings = false
			m.settingsSubmenu = ""
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case performScanMsg:
		// Handle scan completion
		go m.performScan(msg.ctx, msg.scanner, msg.categories)
		return m, func() tea.Msg {
			return scanCompleteMsg{}
		}
	case scanCompleteMsg:
		// Scan completed
		m.scanState.completed = true
		m.scanState.active = false
		m.scanState.currentPhase = "Scan completed!"
		return m, nil
	}
	return m, nil
}

// Navigation functions (sysc-greet pattern)
func (m Model) navigateToScanMode() (Model, tea.Cmd) {
	m.mode = ModeScan
	m.scanState.active = false
	m.scanState.startedAt = time.Now()
	m.scanState.currentPhase = ""
	m.scanState.currentPhaseIndex = 0
	m.scanState.results = nil
	m.scanState.completed = false

	return m, nil
}

// Start actual scanning with proper user-triggered feedback
func (m Model) startActualScan() (Model, tea.Cmd) {
	m.scanState.active = true
	m.scanState.startedAt = time.Now()
	m.scanState.currentPhase = "Initializing..."
	m.scanState.currentPhaseIndex = 0

	// Clear previous results
	m.scanState.results = nil
	m.scanState.completed = false

	// Start scanning in background
	return m, startScanCmd(m.ctx, m.scanner, m.cfg.Categories)
}

func (m Model) navigateToScanSettings() (Model, tea.Cmd) {
	m.settingsSubmenu = "scan_settings"
	m.settingsItems = []string{
		"← Back",
		"Scan Depth: 5",
		"Ignore Patterns: node_modules, .git",
		"Enable All Categories: true",
		"Dry Run Default: true",
	}
	m.settingsIndex = 0
	return m, nil
}

func (m Model) navigateToThemes() (Model, tea.Cmd) {
	m.settingsSubmenu = "themes"
	m.settingsItems = []string{
		"← Back",
		"Theme: Default",
		"Theme: Dracula",
		"Theme: Nord",
		"Theme: Gruvbox",
		"Theme: Solarized",
	}
	m.settingsIndex = 0
	return m, nil
}

// Menu action handlers that integrate with our comprehensive scanning system
func (m Model) handleCleanFiles() (Model, tea.Cmd) {
	if !m.hasCache {
		m.mode = ModeScan
		m.scanState.currentPhase = "No scan results found. Please run a scan first."
		return m, nil
	}

	// Load cache and show cleaning confirmation
	m.mode = ModeClean
	m.scanState.currentPhase = fmt.Sprintf("Ready to clean %d files (%s)",
		m.totalFiles, m.humanizeBytes(m.totalSize))
	return m, nil
}

func (m Model) handleDryRun() (Model, tea.Cmd) {
	if !m.hasCache {
		m.mode = ModeScan
		m.scanState.currentPhase = "No scan results found. Please run a scan first."
		return m, nil
	}

	// Load cache and show preview
	m.mode = ModeDryRun
	m.scanState.currentPhase = fmt.Sprintf("Dry run: Would delete %d files (%s)",
		m.totalFiles, m.humanizeBytes(m.totalSize))
	return m, nil
}

func (m Model) handleShowResults() (Model, tea.Cmd) {
	if !m.hasCache {
		m.mode = ModeScan
		m.scanState.currentPhase = "No scan results found. Please run a scan first."
		return m, nil
	}

	// Show scan results
	m.mode = ModeScan
	m.scanComplete = true
	m.scanState.currentPhase = fmt.Sprintf("Scan completed! Found %d files (%s)",
		m.totalFiles, m.humanizeBytes(m.totalSize))
	return m, nil
}

// Execute cleaning operation
func (m *Model) executeClean(dryRun bool) error {
	// This would call the CLI cleaning functions
	// For now, show a message that CLI cleaning is available
	m.scanState.currentPhase = fmt.Sprintf("%s cleaning via CLI: moonbit clean --%s",
		map[bool]string{true: "Dry run preview", false: "Executing"}[dryRun],
		map[bool]string{true: "dry-run", false: "force"}[dryRun])
	return nil
}

// Tea command to start scanning
func startScanCmd(ctx context.Context, scanner *scanner.Scanner, categories []config.Category) tea.Cmd {
	return func() tea.Msg {
		return performScanMsg{
			ctx:        ctx,
			scanner:    scanner,
			categories: categories,
		}
	}
}

// Perform actual system scan with real progress tracking
func (m *Model) performScan(ctx context.Context, scannerInstance *scanner.Scanner, categories []config.Category) {
	fmt.Printf("DEBUG: Starting scan with %d categories\n", len(categories))

	// Add timeout to prevent hanging
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Update scan progress phases
	updatePhase := func(phase string, index int) {
		fmt.Printf("DEBUG: %s\n", phase)
		m.scanState.currentPhase = phase
		m.scanState.currentPhaseIndex = index
	}

	updatePhase("Starting scan...", 0)
	time.Sleep(500 * time.Millisecond) // Small delay to show initial phase

	// Track all scan results
	var allResults []scanResult

	// Start scanning each category concurrently
	var wg sync.WaitGroup

	updatePhase("Analyzing directories...", 1)
	time.Sleep(500 * time.Millisecond)

	for i, category := range categories {
		if !category.Selected {
			continue
		}

		wg.Add(1)
		go func(idx int, cat *config.Category) {
			defer wg.Done()

			updatePhase(fmt.Sprintf("Scanning %s...", cat.Name), idx+2)
			fmt.Printf("DEBUG: Scanning %s...\n", cat.Paths[0])

			// Create a progress channel for this specific category
			catProgressCh := make(chan scanner.ScanMsg, 10)

			// Start scanning this category
			scannerInstance.ScanCategory(ctx, cat, catProgressCh)

			// Wait for completion and collect progress updates
			timeout := time.After(10 * time.Second)
			for {
				select {
				case <-timeout:
					fmt.Printf("Category %s scan timed out\n", cat.Name)
					return
				case msg := <-catProgressCh:
					if msg.Error != nil {
						// Log error but continue
						fmt.Printf("Scan error for %s: %v\n", cat.Name, msg.Error)
						return
					}

					if msg.Progress != nil {
						// Update UI progress in real-time
						m.scanState.progress = *msg.Progress
						updatePhase(fmt.Sprintf("Processing %s...", filepath.Base(msg.Progress.CurrentDir)), idx+2)
					}

					if msg.Complete != nil {
						// Got completion - convert to UI format
						files := msg.Complete.Stats.FileCount
						sizeBytes := msg.Complete.Stats.Size

						sizeStr := fmt.Sprintf("%d KB", sizeBytes/1024)
						duration := fmt.Sprintf("%.1fs", msg.Complete.Duration.Seconds())

						result := scanResult{
							Category: msg.Complete.Category,
							Files:    files,
							Size:     sizeStr,
							Duration: duration,
							Status:   "Complete",
						}

						// Store result
						allResults = append(allResults, result)
						updatePhase(fmt.Sprintf("Completed %s (%d files)", cat.Name, files), idx+2)
						return
					}
				}
			}
		}(i, &category)
	}

	// Wait for all scans to complete (with timeout)
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()

	updatePhase("Finalizing results...", len(categories)+2)

	select {
	case <-done:
		// Scans completed
		updatePhase("Scan completed!", len(categories)+3)
	case <-time.After(15 * time.Second):
		updatePhase("Scan timeout", len(categories)+3)
		fmt.Println("DEBUG: Scan timeout")
	}

	time.Sleep(300 * time.Millisecond) // Brief pause to show completion

	// Update UI with real results
	m.scanState.results = allResults
	m.scanState.completed = true
	m.scanState.active = false

	fmt.Printf("DEBUG: Scan finished with %d results\n", len(allResults))
}

// Message types
type performScanMsg struct {
	ctx        context.Context
	scanner    *scanner.Scanner
	categories []config.Category
}

type scanCompleteMsg struct{}

// Render the TUI view (sysc-greet installer style full-screen)
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var content strings.Builder

	// ASCII Header only (sysc-greet style)
	headerLines := []string{
		"██▄▀█ ▄▀▀▀▄ ▄▀▀▀▄ ▄▀  █ █▀▀▀▄ ▀▀█▀▀ ▀▀█▀▀    ▄▀    ▄▀ ",
		"█   █ █   █ █   █ █ ▀▄█ █▀▀▀▄   █     █    ▄▀    ▄▀   ",
		"▀   ▀  ▀▀▀   ▀▀▀  ▀   ▀ ▀▀▀▀  ▀▀▀▀▀   ▀   ▀     ▀    ",
	}

	for _, line := range headerLines {
		content.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render(line))
		content.WriteString("\n")
	}
	content.WriteString("\n")

	// Main content based on mode
	var mainContent string
	switch m.mode {
	case ModeMain:
		mainContent = m.renderMainMenu()
	case ModeScan:
		mainContent = m.renderScanView()
	case ModeClean:
		mainContent = m.renderCleanView()
	case ModeDryRun:
		mainContent = m.renderDryRunView()
	case ModeAbout:
		mainContent = m.renderAboutView()
	}

	// Wrap in border (no background color)
	mainStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Primary).
		Width(m.width - 4)
	content.WriteString(mainStyle.Render(mainContent))

	// Help text
	helpText := m.getHelpText()
	if helpText != "" {
		helpStyle := lipgloss.NewStyle().
			Foreground(FgMuted).
			Italic(true).
			Align(lipgloss.Center)
		content.WriteString("\n" + helpStyle.Render(helpText))
	}

	// Settings popup if visible
	if m.showSettings {
		content.WriteString("\n\n")
		content.WriteString(renderSettingsPopup(m))
	}

	// Full terminal control without background
	return content.String()
}

// Render settings popup (no background color)
func renderSettingsPopup(m Model) string {
	// Get title based on submenu state
	var settingsTitle string

	if m.settingsSubmenu == "" {
		settingsTitle = "///// Settings /////"
	} else if m.settingsSubmenu == "scan_settings" {
		settingsTitle = "/// Scan Settings ///"
	} else if m.settingsSubmenu == "themes" {
		settingsTitle = "///// Themes /////"
	} else {
		settingsTitle = "///// Settings /////"
	}

	// Create content
	var content []string
	content = append(content, "")
	content = append(content, settingsTitle)
	content = append(content, "")

	// Menu options
	for i, option := range m.settingsItems {
		var style lipgloss.Style
		if i == m.settingsIndex {
			style = lipgloss.NewStyle().
				Bold(true).
				Foreground(Accent).
				Padding(0, 2)
		} else {
			style = lipgloss.NewStyle().
				Foreground(FgSecondary).
				Padding(0, 2)
		}
		content = append(content, style.Render(option))
	}

	// Help
	content = append(content, "")
	helpStyle := lipgloss.NewStyle().Foreground(FgMuted)
	content = append(content, helpStyle.Render("↑↓ Navigate • Enter Select • Esc Close"))

	innerContent := lipgloss.JoinVertical(lipgloss.Left, content...)

	// Create bordered popup (no background)
	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Accent).
		Padding(2, 4)

	return popupStyle.Render(innerContent)
}

// Render scan header (minimal emoji usage as requested)
func renderScanHeader() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(Secondary).
		SetString("📊 MoonBit Scan Summary:")

	return header.Render()
}

// Render scanning status
func renderScanningStatus() string {
	status := lipgloss.NewStyle().
		Bold(true).
		Foreground(Accent).
		SetString("🔄 Scanning system... Please wait.")

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(BorderDefault).
		Padding(1, 0)

	return borderStyle.Render(status.Render())
}

// Render scan results as a table-like display
func renderScanTable(results []scanResult) string {
	if len(results) == 0 {
		return lipgloss.NewStyle().
			Foreground(FgMuted).
			SetString("No scan results available. Press R to reset.").Render()
	}

	var table strings.Builder

	// Table header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(BorderFocus)

	categoryHeader := "Category"
	filesHeader := "Files"
	sizeHeader := "Size"
	durationHeader := "Duration"
	statusHeader := "Status"

	table.WriteString(headerStyle.Render(fmt.Sprintf("%-20s %8s %12s %10s %10s\n",
		categoryHeader, filesHeader, sizeHeader, durationHeader, statusHeader)))

	// Table separator
	separatorStyle := lipgloss.NewStyle().
		Foreground(BorderDefault)
	table.WriteString(separatorStyle.Render(strings.Repeat("─", 64) + "\n"))

	// Table rows
	rowStyle := lipgloss.NewStyle().
		Foreground(FgPrimary)

	for _, result := range results {
		table.WriteString(rowStyle.Render(fmt.Sprintf("%-20s %8d %12s %10s %10s\n",
			result.Category, result.Files, result.Size, result.Duration, result.Status)))
	}

	// Wrap in border
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(BorderDefault).
		Padding(1, 0)

	return borderStyle.Render(table.String())
}

// Render scan summary statistics
func renderScanSummary(results []scanResult) string {
	if len(results) == 0 {
		return ""
	}

	var totalFiles int
	var totalSizeBytes uint64

	for _, result := range results {
		files, _ := strconv.Atoi(strconv.Itoa(result.Files))
		totalFiles += files
		// Extract numeric size for total (simplified)
		totalSizeBytes += uint64(result.Files * 1024) // Rough estimate
	}

	// Calculate totals
	totalCategories := len(results)
	totalSize := fmt.Sprintf("%d KB", totalSizeBytes/1024)

	summaryStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(Accent)

	summaryText := fmt.Sprintf("Total: %d files across %d categories • %s",
		totalFiles, totalCategories, totalSize)

	return "\n\n" + summaryStyle.Render(summaryText)
}

// Render main menu with clearer options
func (m Model) renderMainMenu() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Foreground(FgMuted).Render("System Cleaning Utility"))
	b.WriteString("\n\n")

	// Show scan status if we have results
	if m.hasCache {
		statusText := fmt.Sprintf("📊 Last scan: %d files, %s",
			m.totalFiles, m.humanizeBytes(m.totalSize))
		b.WriteString(lipgloss.NewStyle().Foreground(Accent).Render(statusText))
		b.WriteString("\n\n")
	}

	// Menu items with comprehensive descriptions
	menuItems := []string{
		"Scan System (find cleanable files)",
		"Clean Files (delete selected)",
		"Dry Run (preview what would be cleaned)",
		"Show Results (view detailed scan results)",
	}

	menuDescriptions := []string{
		"Scan pacman cache, AUR caches, thumbnails, and more",
		"Actually delete files found in last scan",
		"Preview what would be deleted without changing anything",
		"View detailed breakdown of found files and sizes",
	}

	for i, item := range menuItems {
		var prefix string
		if i == m.menuIndex {
			prefix = lipgloss.NewStyle().Foreground(Primary).Render("▸ ")
		} else {
			prefix = "  "
		}

		// Show both the menu item and description
		b.WriteString(prefix + item + "\n")
		if i == m.menuIndex {
			// Highlight the description for the selected item
			descStyle := lipgloss.NewStyle().Foreground(FgMuted).Italic(true)
			b.WriteString(descStyle.Render("    " + menuDescriptions[i]))
		} else {
			descStyle := lipgloss.NewStyle().Foreground(FgMuted)
			b.WriteString(descStyle.Render("    " + menuDescriptions[i]))
		}

		if i < len(menuItems)-1 {
			b.WriteString("\n\n")
		}
	}

	// Add helpful note about CLI commands
	if m.hasCache {
		b.WriteString("\n\n")
		cliStyle := lipgloss.NewStyle().Foreground(FgMuted).Italic(true)
		cliText := "💡 CLI commands: moonbit scan | moonbit clean --dry-run | moonbit clean --force"
		b.WriteString(cliStyle.Render(cliText))
	}

	return b.String()
}

// Render scan view with detailed progress tracking
func (m Model) renderScanView() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(Secondary).Render("System Scan"))
	b.WriteString("\n\n")

	if m.scanState.active {
		// Show detailed scanning progress
		b.WriteString(lipgloss.NewStyle().Foreground(Accent).Render("🔄 ACTIVE: Scanning for cleanable files"))
		b.WriteString("\n\n")

		// Show current step
		b.WriteString(fmt.Sprintf("Current: %s\n", m.scanState.currentPhase))
		b.WriteString("\n")

		// Show all planned scan steps
		b.WriteString(lipgloss.NewStyle().Foreground(FgMuted).Render("Scan Steps:"))
		b.WriteString("\n")
		for i, step := range m.scanState.phases {
			if i == 0 {
				// Current step
				b.WriteString(fmt.Sprintf("  ▸ %s\n", step))
			} else if i < len(m.scanState.phases)-2 {
				// Completed steps
				b.WriteString(fmt.Sprintf("  ✓ %s\n", step))
			} else {
				// Remaining steps
				b.WriteString(fmt.Sprintf("    %s\n", step))
			}
		}

		// Show scan progress details
		if m.scanState.progress.FilesScanned > 0 || m.scanState.progress.DirsScanned > 0 {
			b.WriteString("\n")
			b.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render("Real-time Progress:"))
			b.WriteString("\n")
			b.WriteString(fmt.Sprintf("  Files scanned: %d\n", m.scanState.progress.FilesScanned))
			b.WriteString(fmt.Sprintf("  Directories scanned: %d\n", m.scanState.progress.DirsScanned))
			if m.scanState.progress.Bytes > 0 {
				sizeMB := float64(m.scanState.progress.Bytes) / 1024 / 1024
				b.WriteString(fmt.Sprintf("  Data found: %.1f MB\n", sizeMB))
			}
			if m.scanState.progress.CurrentDir != "" {
				b.WriteString(fmt.Sprintf("  Currently scanning: %s\n", m.scanState.progress.CurrentDir))
			}
		}

		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(Warning).Render("⚠ Press Ctrl+C to cancel scan"))

	} else if m.scanState.completed && len(m.scanState.results) > 0 {
		// Show completed scan results
		b.WriteString(lipgloss.NewStyle().Foreground(Accent).Render("✅ Scan completed successfully!"))
		b.WriteString("\n\n")

		// Show scan summary
		var totalFiles int
		var totalBytes uint64
		for _, result := range m.scanState.results {
			files, _ := strconv.Atoi(strconv.Itoa(result.Files))
			totalFiles += files
			totalBytes += uint64(files * 1024) // Rough estimate
		}

		b.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render("Scan Summary:"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  Total files found: %d\n", totalFiles))
		b.WriteString(fmt.Sprintf("  Total space: %.1f MB\n", float64(totalBytes)/1024/1024))
		b.WriteString(fmt.Sprintf("  Categories scanned: %d\n", len(m.scanState.results)))
		b.WriteString("\n")

		b.WriteString(renderScanTable(m.scanState.results))

	} else if len(m.scanState.results) > 0 {
		// Has partial results
		b.WriteString(lipgloss.NewStyle().Foreground(Secondary).Render("Scan Results"))
		b.WriteString("\n\n")
		b.WriteString(renderScanTable(m.scanState.results))

	} else {
		// Ready to scan
		b.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render("Ready to scan your system for cleanable files."))
		b.WriteString("\n\n")
		b.WriteString("This will scan common locations for:")
		b.WriteString("\n")
		b.WriteString("• Temporary files")
		b.WriteString("\n")
		b.WriteString("• Cache files")
		b.WriteString("\n")
		b.WriteString("• Log files")
		b.WriteString("\n")
		b.WriteString("• Download cache")
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(Accent).Render("Press Enter to start scanning"))
	}

	return b.String()
}

// Render clean view
func (m Model) renderCleanView() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(Secondary).Render("Clean Files Mode"))
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render("Select categories to clean files"))
	b.WriteString("\n\n")

	// List categories with checkbox style
	for i, category := range m.cfg.Categories {
		status := "[ ]"
		if category.Selected {
			status = "[✓]"
		}

		b.WriteString(fmt.Sprintf("%s %s (%s)\n", status, category.Name, category.Risk.String()))
		if i < len(m.cfg.Categories)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// Render dry run view
func (m Model) renderDryRunView() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(Secondary).Render("Dry Run Mode"))
	b.WriteString("\n\n")

	if len(m.scanState.results) > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render("Preview what would be cleaned:"))
		b.WriteString("\n\n")

		var totalSize uint64
		var totalFiles int

		// Calculate totals from scan results
		for _, result := range m.scanState.results {
			files, _ := strconv.Atoi(strconv.Itoa(result.Files))
			totalFiles += files
			totalSize += uint64(files * 1024) // Rough estimate
		}

		b.WriteString(fmt.Sprintf("• %d files across %d categories\n", totalFiles, len(m.scanState.results)))
		b.WriteString(fmt.Sprintf("• Approximately %.1f MB\n\n", float64(totalSize)/1024/1024))
		b.WriteString(lipgloss.NewStyle().Foreground(Warning).Render("⚠ This is a preview - no files will be deleted"))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(FgMuted).Render("Scan the system first to see what would be cleaned"))
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(Accent).Render("Press Enter to perform scan first"))
	}

	return b.String()
}

// Render about view
func (m Model) renderAboutView() string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(Primary).Render("MoonBit"))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(FgSecondary).Render("System Cleaner"))
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render("Version 1.0"))
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render("A TUI-based system cleaning tool"))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(FgPrimary).Render("with safety mechanisms and undo support."))

	return b.String()
}

// Get help text based on current state
func (m Model) getHelpText() string {
	if m.showSettings {
		if m.settingsSubmenu == "" {
			return "↑/↓: Navigate • Enter: Select • Esc: Close"
		} else {
			return "↑/↓: Navigate • Enter: Select • Esc: Back"
		}
	}

	switch m.mode {
	case ModeMain:
		return "↑/↓: Navigate • Enter: Select • F1: Settings • Q: Quit"
	case ModeScan:
		if !m.scanState.active && len(m.scanState.results) == 0 {
			return "Enter: Start Scan • R: Reset • Esc: Back"
		} else {
			return "R: Reset • Esc: Back"
		}
	case ModeClean:
		return "↑/↓: Select Category • Space: Toggle • R: Reset • Esc: Back"
	case ModeDryRun:
		if len(m.scanState.results) == 0 {
			return "Enter: Perform Scan First • Esc: Back"
		} else {
			return "Esc: Back"
		}
	case ModeAbout:
		return "Esc: Back • Q: Quit"
	default:
		return "Esc: Back • Q: Quit"
	}
}

// Format menu item with selection indicator
func formatMenuItem(item string, selected bool) string {
	if selected {
		return "▶ " + item
	}
	return "  " + item
}

// Start launches the MoonBit TUI (sysc-greet installer style)
func Start() {
	// Use tea.WithAltScreen() to take full control of terminal like sysc-greet
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		// Handle error properly
		if debugLog != nil {
			debugLog.Printf("Error running MoonBit UI: %v", err)
		}
	}
}

// loadSessionCache loads the scan results from the CLI cache
func (m *Model) loadSessionCache() (*SessionCache, error) {
	cachePath := m.getSessionCachePath()
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	var cache SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// getSessionCachePath returns the path to the session cache
func (m *Model) getSessionCachePath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".cache", "moonbit", "scan_results.json")
}

// humanizeBytes converts bytes to human-readable format
func (m *Model) humanizeBytes(bytes uint64) string {
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
