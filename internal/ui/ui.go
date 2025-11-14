package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/cleaner"
	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// loadASCIIArt loads the ASCII art from ascii.txt file
func loadASCIIArt() string {
	// Try to load from ascii.txt in the current directory or project root
	possiblePaths := []string{
		"ascii.txt",
		"../ascii.txt",
		"../../ascii.txt",
		"/usr/share/moonbit/ascii.txt",
	}

	for _, path := range possiblePaths {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
	}

	// Fallback ASCII if file not found
	return `
█▀▄▀█ ▄▀▀▀▄ ▄▀▀▀▄ █▄  █ █▀▀▀▄ ▀▀█▀▀ ▀▀█▀▀    ▄▀    ▄▀
█   █ █   █ █   █ █ ▀▄█ █▀▀▀▄   █     █    ▄▀    ▄▀
▀   ▀  ▀▀▀   ▀▀▀  ▀   ▀ ▀▀▀▀  ▀▀▀▀▀   ▀   ▀     ▀
`
}

// ASCII header loaded from ascii.txt
var asciiHeader = loadASCIIArt()

// View modes for the TUI
type ViewMode string

const (
	ModeWelcome      ViewMode = "welcome"
	ModeScanProgress ViewMode = "scan_progress"
	ModeResults      ViewMode = "results"
	ModeSelect       ViewMode = "select"
	ModeConfirm      ViewMode = "confirm"
	ModeClean        ViewMode = "clean"
	ModeComplete     ViewMode = "complete"
	ModeSchedule     ViewMode = "schedule"
)

// CategoryInfo represents a cleanable category for UI
type CategoryInfo struct {
	Name    string
	Enabled bool
	Files   int
	Size    string
}

// Model represents the MoonBit TUI state
type Model struct {
	width  int
	height int
	mode   ViewMode

	// Menu selection
	menuIndex   int
	menuOptions []string
	scanMode    string // "quick" or "deep"

	// Scan state
	scanActive      bool
	scanStarted     time.Time
	scanOutput      strings.Builder
	scanResults     *config.SessionCache
	scanProgress    float64
	currentPhase    string
	scanError       string
	filesScanned    int
	bytesScanned    uint64
	currentFile     string
	totalFilesGuess int

	// Clean state
	cleanActive       bool
	cleanStarted      time.Time
	cleanError        string
	cleanFilesDeleted int
	cleanBytesFreed   uint64

	// Categories for selection
	categories    []CategoryInfo
	selectedCount int

	// Viewports for scrolling
	categoryViewport viewport.Model
	resultsViewport  viewport.Model
	viewportReady    bool

	// Settings
	cfg *config.Config
}

// NewModel creates a new MoonBit model
func NewModel() Model {
	cfg := config.DefaultConfig()

	return Model{
		width:     80,
		height:    24,
		mode:      ModeWelcome,
		menuIndex: 0,
		menuOptions: []string{
			"Quick Scan (Safe caches only)",
			"Deep Scan (All categories)",
			"Review Results",
			"Schedule Scan & Clean",
			"Exit",
		},
		scanMode: "quick", // Default to quick mode
		cfg:      cfg,
	}
}

// Start launches the MoonBit TUI
func Start() {
	p := tea.NewProgram(NewModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running MoonBit: %v\n", err)
		os.Exit(1)
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages - implements tea.Model interface
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	// Process message based on type
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Initialize viewports with proper dimensions
		if !m.viewportReady {
			// Reserve space for header (8 lines), footer (3 lines), and padding
			contentHeight := m.height - 15
			if contentHeight < 10 {
				contentHeight = 10
			}

			m.categoryViewport = viewport.New(m.width-4, contentHeight)
			m.resultsViewport = viewport.New(m.width-4, contentHeight)
			m.viewportReady = true
		} else {
			// Update viewport dimensions on window resize
			contentHeight := m.height - 15
			if contentHeight < 10 {
				contentHeight = 10
			}
			
			m.categoryViewport.Width = m.width - 4
			m.categoryViewport.Height = contentHeight
			m.resultsViewport.Width = m.width - 4
			m.resultsViewport.Height = contentHeight
		}
	case tickMsg:
		// Update progress display if scanning
		if m.scanActive && m.totalFilesGuess > 0 {
			prog := float64(m.filesScanned) / float64(m.totalFilesGuess)
			m.scanProgress = prog
			if m.currentFile != "" {
				m.currentPhase = fmt.Sprintf("Scanning: %s", filepath.Base(m.currentFile))
			}
		}
		// Continue ticking while scan is active
		if m.scanActive || m.cleanActive {
			return m, tick()
		}
		return m, nil
	case scanProgressMsg:
		m.scanProgress = msg.Progress
		m.currentPhase = msg.Phase
		m.filesScanned = msg.FilesScanned
		m.bytesScanned = msg.BytesScanned
		m.currentFile = msg.CurrentPath
		return m, nil
	case scanCompleteMsg:
		return m.handleScanComplete(msg)
	case cleanCompleteMsg:
		return m.handleCleanComplete(msg)
	case timerCommandMsg:
		m.currentPhase = msg.message
		// Stay in schedule mode and refresh
		return m, nil
	}

	// Update viewport if in relevant modes
	if m.mode == ModeSelect {
		m.categoryViewport, cmd = m.categoryViewport.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.mode == ModeResults {
		m.resultsViewport, cmd = m.resultsViewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// handleCompleteKey handles keypresses in complete mode
func (m Model) handleCompleteKey() (tea.Model, tea.Cmd) {
	m.mode = ModeWelcome
	m.menuIndex = 0
	// Clear clean results
	m.cleanFilesDeleted = 0
	m.cleanBytesFreed = 0
	return m, nil
}

// handleKey processes keyboard input
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Allow viewport scrolling in certain modes
	if m.mode == ModeSelect || m.mode == ModeResults {
		switch msg.String() {
		case "pgup", "pgdown", "home", "end":
			// These keys will be handled by viewport update in Update()
			return m, nil

		}
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up":
		if m.menuIndex > 0 {
			m.menuIndex--
		}
	case "down":
		// Calculate max index based on current mode
		maxIndex := len(m.menuOptions) - 1
		if m.mode == ModeSelect {
			// categories + Select All + Clean + Back
			maxIndex = len(m.categories) + 2
		} else if m.mode == ModeConfirm {
			// 2 options: Confirm & Clean, Cancel
			maxIndex = 1
		}

		if m.menuIndex < maxIndex {
			m.menuIndex++
		}
	case "enter", " ":
		// Handle complete mode specially
		if m.mode == ModeComplete {
			return m.handleCompleteKey()
		}
		return m.handleMenuSelect()
	case "esc":
		if m.mode == ModeComplete {
			return m.handleCompleteKey()
		}
		if m.mode != ModeWelcome {
			m.mode = ModeWelcome
			m.menuIndex = 0
		}
	}

	return m, nil
}

// handleMenuSelect processes menu selection
func (m Model) handleMenuSelect() (tea.Model, tea.Cmd) {
	switch m.mode {
	case ModeWelcome:
		switch m.menuIndex {
		case 0: // Quick Scan
			m.scanMode = "quick"
			return m.startScan()
		case 1: // Deep Scan
			m.scanMode = "deep"
			return m.startScan()
		case 2: // Review Results
			return m.showResults()
		case 3: // Schedule Scan & Clean
			return m.showSchedule()
		case 4: // Exit
			return m, tea.Quit
		}
	case ModeResults:
		// Pressing Enter on results view goes to category selection
		if m.scanResults != nil && len(m.categories) > 0 {
			m.mode = ModeSelect
			m.menuIndex = 0
		}
		return m, nil
	case ModeConfirm:
		if m.menuIndex == 0 { // Confirm & Clean (first option)
			return m.executeClean()
		} else { // Cancel (second option, menuIndex == 1)
			m.mode = ModeSelect
			m.menuIndex = 0
			return m, nil
		}
	case ModeSelect:
		totalOptions := len(m.categories) + 3 // categories + Select All + Clean + Back

		if m.menuIndex == totalOptions-1 { // Back
			m.mode = ModeResults
			m.menuIndex = 0
		} else if m.menuIndex == totalOptions-2 { // Clean Selected
			return m.showConfirm()
		} else if m.menuIndex == len(m.categories) { // Select All
			// Toggle select all
			allSelected := true
			for _, cat := range m.categories {
				if !cat.Enabled {
					allSelected = false
					break
				}
			}
			// If all selected, deselect all. Otherwise, select all
			for i := range m.categories {
				m.categories[i].Enabled = !allSelected
			}
			m.updateSelectedCount()
		} else if m.menuIndex >= 0 && m.menuIndex < len(m.categories) {
			// Toggle individual category
			m.categories[m.menuIndex].Enabled = !m.categories[m.menuIndex].Enabled
			m.updateSelectedCount()
		}
	case ModeSchedule:
		switch m.menuIndex {
		case 0: // Enable Scan Timer
			m.currentPhase = "" // Clear previous messages
			return m.executeTimerCommand("enable", "moonbit-scan.timer")
		case 1: // Disable Scan Timer
			m.currentPhase = ""
			return m.executeTimerCommand("disable", "moonbit-scan.timer")
		case 2: // Enable Clean Timer
			m.currentPhase = ""
			return m.executeTimerCommand("enable", "moonbit-clean.timer")
		case 3: // Disable Clean Timer
			m.currentPhase = ""
			return m.executeTimerCommand("disable", "moonbit-clean.timer")
		case 4: // Back
			m.mode = ModeWelcome
			m.menuIndex = 0
			m.currentPhase = ""
		}
	}

	return m, nil
}

// tick creates a ticker command for progress updates
func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// startScan initiates scanning
func (m Model) startScan() (tea.Model, tea.Cmd) {
	m.mode = ModeScanProgress
	m.scanActive = true
	m.scanStarted = time.Now()
	m.scanProgress = 0
	if m.scanMode == "deep" {
		m.currentPhase = "Starting deep scan (all categories)..."
	} else {
		m.currentPhase = "Starting quick scan (safe caches)..."
	}
	m.scanOutput.Reset()

	// Reset progress state
	m.filesScanned = 0
	m.bytesScanned = 0
	m.currentFile = ""
	m.totalFilesGuess = 0

	return m, tea.Batch(runScanCmd(m.cfg, m.scanMode), tick())
}

// runScanCmd executes the scan using the scanner package directly
func runScanCmd(cfg *config.Config, scanMode string) tea.Cmd {
	return func() tea.Msg {
		// Count total categories to scan for progress calculation
		totalCategories := 0
		for _, category := range cfg.Categories {
			if scanMode == "quick" && !category.Selected {
				continue
			}
			exists := false
			for _, path := range category.Paths {
				if _, err := os.Stat(path); err == nil {
					exists = true
					break
				}
			}
			if exists {
				totalCategories++
			}
		}

		ctx := context.Background()
		s := scanner.NewScanner(cfg)

		var scannedCategories []config.Category
		var totalSize uint64
		var totalFiles int
		var totalFilesScanned int
		categoriesScanned := 0

		// Scan categories based on mode
		for _, category := range cfg.Categories {
			// In quick mode, only scan Selected:true categories
			if scanMode == "quick" && !category.Selected {
				continue
			}

			// Check if category paths exist
			exists := false
			for _, path := range category.Paths {
				if _, err := os.Stat(path); err == nil {
					exists = true
					break
				}
			}

			if !exists {
				continue
			}

			progressCh := make(chan scanner.ScanMsg, 10)
			go s.ScanCategory(ctx, &category, progressCh)

			// Collect results for this category
			for msg := range progressCh {
				// Forward progress updates (but we can't send them from here)
				// We'll track total files scanned for final progress
				if msg.Progress != nil {
					totalFilesScanned += msg.Progress.FilesScanned
				}

				if msg.Complete != nil {
					scannedCategories = append(scannedCategories, *msg.Complete.Stats)
					totalSize += msg.Complete.Stats.Size
					totalFiles += msg.Complete.Stats.FileCount
					categoriesScanned++
					break
				}
				if msg.Error != nil {
					return scanCompleteMsg{
						Success: false,
						Error:   msg.Error.Error(),
					}
				}
			}
		}

		// Save to cache
		cache := &config.SessionCache{
			ScanResults: &config.Category{
				Name:  "Total Cleanable",
				Files: []config.FileInfo{},
			},
			TotalSize:  totalSize,
			TotalFiles: totalFiles,
			ScannedAt:  time.Now(),
		}

		// Aggregate all files
		for _, cat := range scannedCategories {
			cache.ScanResults.Files = append(cache.ScanResults.Files, cat.Files...)
		}

		if err := saveSessionCache(cache); err != nil {
			return scanCompleteMsg{
				Success: false,
				Error:   fmt.Sprintf("failed to save cache: %v", err),
			}
		}

		return scanCompleteMsg{
			Success:    true,
			Categories: scannedCategories,
			TotalSize:  totalSize,
			TotalFiles: totalFiles,
		}
	}
}

// handleScanComplete processes scan completion
func (m Model) handleScanComplete(msg scanCompleteMsg) (tea.Model, tea.Cmd) {
	m.scanActive = false

	if !msg.Success {
		m.currentPhase = "Scan failed: " + msg.Error
		m.scanError = msg.Error
		m.mode = ModeResults // Show error in results view
		return m, nil
	}

	// Load scan results from cache
	if cache, err := m.loadSessionCache(); err == nil {
		m.scanResults = cache
		m.scanError = "" // Clear any previous errors
		m.parseScanResults(cache, msg.Categories)

		// Go directly to category selection instead of results view
		m.mode = ModeSelect
		m.menuIndex = 0
	} else {
		m.currentPhase = "Failed to load scan results"
		m.scanError = err.Error()
		m.mode = ModeResults
	}

	return m, nil
}

// showResults displays scan results
func (m Model) showResults() (tea.Model, tea.Cmd) {
	// Try to load existing scan results
	if cache, err := m.loadSessionCache(); err == nil {
		m.scanResults = cache
		m.parseScanResults(cache, nil)
		m.mode = ModeResults
	} else {
		m.currentPhase = "No scan results found. Run a scan first."
	}
	return m, nil
}

// parseScanResults converts cache to UI categories
func (m *Model) parseScanResults(cache *config.SessionCache, categories []config.Category) {
	m.categories = []CategoryInfo{}

	// If we have fresh categories from scan, use those
	if len(categories) > 0 {
		for _, cat := range categories {
			if cat.FileCount > 0 {
				m.categories = append(m.categories, CategoryInfo{
					Name:    cat.Name,
					Files:   cat.FileCount,
					Size:    humanizeBytes(cat.Size),
					Enabled: true,
				})
			}
		}
	} else if cache != nil && cache.TotalFiles > 0 {
		// Otherwise, try to reconstruct from cache
		// Group files by category name from config
		categoryMap := make(map[string]*CategoryInfo)

		// Include ALL categories (matching scan behavior)
		for _, cat := range m.cfg.Categories {
			categoryMap[cat.Name] = &CategoryInfo{
				Name:    cat.Name,
				Files:   0,
				Size:    "0 B",
				Enabled: true,
			}
		}

		// Aggregate cache data
		if cache.ScanResults != nil {
			for _, file := range cache.ScanResults.Files {
				// Try to match file to category by path
				matched := false
				for _, cat := range m.cfg.Categories {
					for _, path := range cat.Paths {
						if strings.HasPrefix(file.Path, path) {
							if info, exists := categoryMap[cat.Name]; exists {
								info.Files++
								// Parse existing size and add
								matched = true
								break
							}
						}
					}
					if matched {
						break
					}
				}
			}
		}

		// Convert map to slice, only include non-zero categories
		for _, info := range categoryMap {
			if info.Files > 0 {
				m.categories = append(m.categories, *info)
			}
		}

		// If we couldn't reconstruct, show aggregate
		if len(m.categories) == 0 && cache.TotalFiles > 0 {
			m.categories = append(m.categories, CategoryInfo{
				Name:    "Cleanable Files",
				Files:   cache.TotalFiles,
				Size:    humanizeBytes(cache.TotalSize),
				Enabled: true,
			})
		}
	}

	m.updateSelectedCount()
}

// updateSelectedCount updates the count of selected categories
func (m *Model) updateSelectedCount() {
	count := 0
	for _, cat := range m.categories {
		if cat.Enabled {
			count++
		}
	}
	m.selectedCount = count
}

// startClean initiates cleaning process
func (m Model) startClean() (tea.Model, tea.Cmd) {
	if m.scanResults == nil {
		m.currentPhase = "No scan results found. Run a scan first."
		return m, nil
	}

	m.mode = ModeSelect
	return m, nil
}

// showConfirm displays confirmation dialog
func (m Model) showConfirm() (tea.Model, tea.Cmd) {
	m.mode = ModeConfirm
	m.menuIndex = 0 // Default to Confirm & Clean (first option)
	return m, nil
}

// executeClean performs the actual cleaning
func (m Model) executeClean() (tea.Model, tea.Cmd) {
	m.mode = ModeClean
	m.menuIndex = 0
	m.cleanActive = true
	m.cleanStarted = time.Now()
	m.currentPhase = "Cleaning in progress..."

	// Build a filtered category with only files from enabled categories
	filteredCache := m.buildFilteredCache()

	return m, tea.Batch(runCleanCmd(m.cfg, filteredCache), tick())
}

// buildFilteredCache creates a cache with only files from enabled categories
func (m Model) buildFilteredCache() *config.SessionCache {
	if m.scanResults == nil || m.scanResults.ScanResults == nil {
		return nil
	}

	// Get names of enabled categories
	enabledNames := make(map[string]bool)
	for _, cat := range m.categories {
		if cat.Enabled {
			enabledNames[cat.Name] = true
		}
	}

	// Filter files based on enabled categories
	var filteredFiles []config.FileInfo
	for _, file := range m.scanResults.ScanResults.Files {
		// Match file to category by checking if path starts with category path
		for _, configCat := range m.cfg.Categories {
			if !enabledNames[configCat.Name] {
				continue
			}
			for _, catPath := range configCat.Paths {
				if strings.HasPrefix(file.Path, catPath) {
					filteredFiles = append(filteredFiles, file)
					break
				}
			}
		}
	}

	// Calculate total size
	var totalSize uint64
	for _, file := range filteredFiles {
		totalSize += file.Size
	}

	return &config.SessionCache{
		ScanResults: &config.Category{
			Name:      "Selected Categories",
			Files:     filteredFiles,
			FileCount: len(filteredFiles),
			Size:      totalSize,
		},
		TotalSize:  totalSize,
		TotalFiles: len(filteredFiles),
		ScannedAt:  m.scanResults.ScannedAt,
	}
}

// runCleanCmd executes cleaning using the cleaner package
func runCleanCmd(cfg *config.Config, cache *config.SessionCache) tea.Cmd {
	return func() tea.Msg {
		if cache == nil || cache.ScanResults == nil {
			return cleanCompleteMsg{
				Success: false,
				Error:   "No scan results available",
			}
		}

		ctx := context.Background()
		c := cleaner.NewCleaner(cfg)

		progressCh := make(chan cleaner.CleanMsg, 10)
		go c.CleanCategory(ctx, cache.ScanResults, false, progressCh)

		var deletedFiles int
		var deletedBytes uint64
		var errors []string

		// Process cleaning messages
		for msg := range progressCh {
			if msg.Complete != nil {
				deletedFiles = msg.Complete.FilesDeleted
				deletedBytes = msg.Complete.BytesFreed
				errors = msg.Complete.Errors
				break
			}

			if msg.Error != nil {
				return cleanCompleteMsg{
					Success: false,
					Error:   msg.Error.Error(),
				}
			}
		}

		errorMsg := ""
		if len(errors) > 0 {
			errorMsg = fmt.Sprintf("%d files failed to delete", len(errors))
		}

		return cleanCompleteMsg{
			Success:      true,
			FilesDeleted: deletedFiles,
			BytesFreed:   deletedBytes,
			Error:        errorMsg,
		}
	}
}

// handleCleanComplete processes cleaning completion
func (m Model) handleCleanComplete(msg cleanCompleteMsg) (tea.Model, tea.Cmd) {
	m.cleanActive = false

	if msg.Success {
		m.mode = ModeComplete
		m.cleanError = ""
		m.cleanFilesDeleted = msg.FilesDeleted
		m.cleanBytesFreed = msg.BytesFreed
		if msg.Error != "" {
			m.currentPhase = fmt.Sprintf("Cleaned %d files (%s) with some errors: %s",
				msg.FilesDeleted, humanizeBytes(msg.BytesFreed), msg.Error)
		} else {
			m.currentPhase = fmt.Sprintf("Successfully cleaned %d files, freed %s",
				msg.FilesDeleted, humanizeBytes(msg.BytesFreed))
		}
	} else {
		m.currentPhase = "Cleaning failed: " + msg.Error
		m.cleanError = msg.Error
		m.mode = ModeResults // Return to results view on error
	}
	return m, nil
}

// loadSessionCache loads scan results from CLI cache
func (m Model) loadSessionCache() (*config.SessionCache, error) {
	homeDir, _ := os.UserHomeDir()
	cachePath := filepath.Join(homeDir, ".cache", "moonbit", "scan_results.json")

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	var cache config.SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// saveSessionCache saves scan results to cache
func saveSessionCache(cache *config.SessionCache) error {
	homeDir, _ := os.UserHomeDir()
	cacheDir := filepath.Join(homeDir, ".cache", "moonbit")
	cachePath := filepath.Join(cacheDir, "scan_results.json")

	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cachePath, data, 0600)
}

// borderedPanel wraps content in a bordered panel (sysc-greet style)
func borderedPanel(content string, borderColor lipgloss.Color, width int) string {
	border := lipgloss.RoundedBorder()
	style := lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Padding(1, 2).
		Width(width - 4).
		Align(lipgloss.Left)

	return style.Render(content)
}

// View renders the UI (sysc-greet inspired layout)
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}
	
	// Ensure minimum dimensions for proper rendering
	// ASCII art is 54 chars wide, need at least 60 for borders
	minWidth := 60
	minHeight := 20
	if m.width < minWidth || m.height < minHeight {
		msg := lipgloss.NewStyle().
			Foreground(Warning).
			Bold(true).
			Width(m.width).
			Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render(fmt.Sprintf("Terminal too small!\nMinimum: %dx%d\nCurrent: %dx%d", 
				minWidth, minHeight, m.width, m.height))
		return msg
	}

	var content strings.Builder

	// ASCII Header - sysc-greet pattern: style entire block first, then center
	// Per-line Render() causes JoinVertical/centering to miscalculate widths
	headerStyle := lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true)

	// Clean up ASCII - trim whitespace and remove empty lines
	cleanASCII := strings.TrimSpace(asciiHeader)

	// Apply styling to entire ASCII block at once
	styledASCII := headerStyle.Render(cleanASCII)

	// Center the styled block
	centeredASCII := lipgloss.NewStyle().
		Width(m.width).
		Align(lipgloss.Center).
		Render(styledASCII)
	content.WriteString(centeredASCII)
	content.WriteString("\n")

	// Subtitle - style first, then center
	subtitleText := "System Cleaner for Linux"
	styledSubtitle := lipgloss.NewStyle().
		Foreground(FgMuted).
		Italic(true).
		Render(subtitleText)
	centeredSubtitle := lipgloss.NewStyle().
		Width(m.width).
		Align(lipgloss.Center).
		Render(styledSubtitle)
	content.WriteString(centeredSubtitle)
	content.WriteString("\n\n")

	// Main content area with borders
	var mainContent string
	var borderColor lipgloss.Color

	switch m.mode {
	case ModeWelcome:
		mainContent = m.renderWelcome()
		borderColor = Primary
	case ModeScanProgress:
		mainContent = m.renderScanProgress()
		borderColor = Primary
	case ModeResults:
		mainContent = m.renderResults()
		borderColor = Accent
	case ModeSelect:
		mainContent = m.renderSelect()
		borderColor = Secondary
	case ModeConfirm:
		mainContent = m.renderConfirm()
		borderColor = Danger  // Red border for dangerous action confirmation
	case ModeClean:
		mainContent = m.renderClean()
		borderColor = Danger
	case ModeComplete:
		mainContent = m.renderComplete()
		borderColor = Accent
	case ModeSchedule:
		mainContent = m.renderSchedule()
		borderColor = Secondary
	}

	// Bordered panel (not centered yet)
	panel := borderedPanel(mainContent, borderColor, m.width)
	content.WriteString(panel)

	// Footer
	content.WriteString("\n\n")
	footer := lipgloss.NewStyle().
		Foreground(FgMuted).
		Italic(true).
		Render(m.getFooterText())
	content.WriteString(footer)

	// Center everything at the end with a single wrapper
	bgStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Top)
	
	return bgStyle.Render(content.String())
}

// renderWelcome renders the welcome screen (sysc-greet style)
func (m Model) renderWelcome() string {
	var content strings.Builder

	// System status with status marker
	if m.scanResults != nil {
		statusMarker := lipgloss.NewStyle().
			Foreground(Accent).
			Render("[OK]")

		lastScan := fmt.Sprintf("%s Last scan: %d files (%s)",
			statusMarker,
			m.scanResults.TotalFiles,
			humanizeBytes(m.scanResults.TotalSize))

		content.WriteString(lastScan)
		content.WriteString("\n\n")
	} else {
		infoMarker := lipgloss.NewStyle().
			Foreground(FgMuted).
			Render("[INFO]")

		content.WriteString(fmt.Sprintf("%s No previous scan found", infoMarker))
		content.WriteString("\n\n")
	}

	// Menu - simple "> Option" style like sysc-greet
	content.WriteString(lipgloss.NewStyle().
		Foreground(FgSecondary).
		Bold(true).
		Render("Select an option:"))
	content.WriteString("\n\n")

	for i, option := range m.menuOptions {
		var line string
		if i == m.menuIndex {
			// Selected item - bold and with arrow
			line = lipgloss.NewStyle().
				Foreground(Primary).
				Bold(true).
				Render(fmt.Sprintf("> %s", option))
		} else {
			// Unselected item
			line = lipgloss.NewStyle().
				Foreground(FgPrimary).
				Render(fmt.Sprintf("  %s", option))
		}
		content.WriteString(line)
		content.WriteString("\n")
	}

	return content.String()
}

// renderScanProgress renders the scan progress screen
func (m Model) renderScanProgress() string {
	var content strings.Builder

	// Progress header
	content.WriteString(progressHeaderStyle.Render("SCANNING SYSTEM"))
	content.WriteString("\n\n")

	// Current phase with stats
	phaseText := m.currentPhase
	if m.scanActive {
		elapsed := time.Since(m.scanStarted)
		if m.filesScanned > 0 {
			phaseText = fmt.Sprintf("%s - %d files (%s) - %.1fs",
				m.currentPhase,
				m.filesScanned,
				humanizeBytes(m.bytesScanned),
				elapsed.Seconds())
		} else {
			phaseText = fmt.Sprintf("%s (%.1fs elapsed)", m.currentPhase, elapsed.Seconds())
		}
	}
	content.WriteString(phaseStyle.Render(phaseText))
	content.WriteString("\n\n")

	// Animated indeterminate progress bar with gradient while scanning
	barWidth := 50

	if m.scanActive {
		// Indeterminate progress - show moving wave with gradient
		elapsed := time.Since(m.scanStarted).Seconds()
		// Calculate position of the "wave" - moves left to right
		wavePos := int(elapsed*10) % barWidth

		var bar strings.Builder
		for i := 0; i < barWidth; i++ {
			// Create a 5-character wide "wave" of filled blocks
			dist := i - wavePos
			if dist < 0 {
				dist = -dist
			}
			if dist < 3 {
				// Calculate gradient position (0.0 to 1.0) across the bar
				gradientPos := float64(i) / float64(barWidth)
				// Interpolate between Primary (Great Old One Green) and Secondary (Watery Tomb Blue)
				color := interpolateColor(Primary, Secondary, gradientPos)
				coloredChar := lipgloss.NewStyle().
					Foreground(color).
					Render("█")
				bar.WriteString(coloredChar)
			} else {
				// Subtle empty bar color
				bar.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color("#323449")).
					Render("░"))
			}
		}

		content.WriteString(lipgloss.NewStyle().
			Align(lipgloss.Center).
			Render(bar.String()))
		content.WriteString("\n")
		content.WriteString(lipgloss.NewStyle().
			Foreground(Secondary).
			Bold(true).
			Align(lipgloss.Center).
			Render("Scanning..."))
	} else {
		// Static completed bar with gradient
		var bar strings.Builder
		for i := 0; i < barWidth; i++ {
			gradientPos := float64(i) / float64(barWidth)
			color := interpolateColor(Primary, Secondary, gradientPos)
			coloredChar := lipgloss.NewStyle().
				Foreground(color).
				Render("█")
			bar.WriteString(coloredChar)
		}

		content.WriteString(lipgloss.NewStyle().
			Align(lipgloss.Center).
			Render(bar.String()))
		content.WriteString("\n")
		content.WriteString(lipgloss.NewStyle().
			Foreground(Secondary).
			Bold(true).
			Align(lipgloss.Center).
			Render("Complete"))
	}
	content.WriteString("\n\n")

	// Show current file being scanned
	if m.scanActive && m.currentFile != "" {
		currentFile := m.currentFile
		if len(currentFile) > 60 {
			currentFile = "..." + currentFile[len(currentFile)-57:]
		}
		content.WriteString(statusStyle.Render(fmt.Sprintf("Current: %s", currentFile)))
		content.WriteString("\n")
	}

	return content.String()
}

// renderResults renders the results summary screen
func (m Model) renderResults() string {
	var header strings.Builder
	var viewportContent strings.Builder

	// Results header
	header.WriteString(resultsHeaderStyle.Render("SCAN RESULTS"))
	header.WriteString("\n\n")

	// Show error if present
	if m.scanError != "" {
		return header.String() +
			errorStyle.Render("⚠️  Error: "+m.scanError) + "\n\n" +
			nextActionStyle.Render("Press Esc to return to main menu")
	}

	// Show cleaning error if present
	if m.cleanError != "" {
		return header.String() +
			errorStyle.Render("⚠️  Cleaning failed: "+m.cleanError) + "\n\n" +
			nextActionStyle.Render("Press Esc to return to main menu")
	}

	if m.scanResults != nil && len(m.categories) > 0 {
		// Summary stats
		summary := fmt.Sprintf("Found %d cleanable files (%s)",
			m.scanResults.TotalFiles, humanizeBytes(m.scanResults.TotalSize))
		viewportContent.WriteString(summaryStyle.Render(summary))
		viewportContent.WriteString("\n\n")

		// Category breakdown
		viewportContent.WriteString(categoryHeaderStyle.Render("CATEGORIES"))
		viewportContent.WriteString("\n")

		for _, cat := range m.categories {
			line := fmt.Sprintf("📁 %s: %s (%d files)", cat.Name, cat.Size, cat.Files)
			if cat.Enabled {
				viewportContent.WriteString(categoryEnabledStyle.Render("  ✓ " + line))
			} else {
				viewportContent.WriteString(categoryDisabledStyle.Render("  ○ " + line))
			}
			viewportContent.WriteString("\n")
		}

		// Update viewport with content
		m.resultsViewport.SetContent(viewportContent.String())

		footer := nextActionStyle.Render("Press Enter to select categories for cleaning")
		return header.String() + m.resultsViewport.View() + "\n\n" + footer
	}

	return header.String() +
		errorStyle.Render("No scan results available") + "\n\n" +
		nextActionStyle.Render("Press Esc to return to main menu and run a scan")
}

// renderSelect renders the category selection screen (sysc-greet style)
func (m Model) renderSelect() string {
	var header strings.Builder
	var viewportContent strings.Builder

	// Selection header with clean styling
	header.WriteString(lipgloss.NewStyle().
		Foreground(Secondary).
		Bold(true).
		Render("SELECT CATEGORIES TO CLEAN"))
	header.WriteString("\n\n")

	// Simple scan summary at the top
	var scanSummary string
	if m.scanResults != nil && m.scanResults.TotalFiles > 0 {
		scanSummary = lipgloss.NewStyle().
			Foreground(Accent).
			Render(fmt.Sprintf("Scan found: %d categories with %d files (%s total)", 
				len(m.categories), m.scanResults.TotalFiles, humanizeBytes(m.scanResults.TotalSize)))
		scanSummary += "\n\n"
	}

	// Build viewport content with categories using clean checkboxes
	for i, cat := range m.categories {
		var line string
		checkbox := "[ ]"
		if cat.Enabled {
			checkbox = "[X]"
		}

		// Format the line
		catInfo := fmt.Sprintf("%s %s - %s (%d files)", checkbox, cat.Name, cat.Size, cat.Files)

		if i == m.menuIndex {
			// Selected item - bold with arrow
			line = lipgloss.NewStyle().
				Foreground(Primary).
				Bold(true).
				Render(fmt.Sprintf("> %s", catInfo))
		} else if cat.Enabled {
			// Enabled but not selected
			line = lipgloss.NewStyle().
				Foreground(Accent).
				Render(fmt.Sprintf("  %s", catInfo))
		} else {
			// Not selected, not enabled
			line = lipgloss.NewStyle().
				Foreground(FgPrimary).
				Render(fmt.Sprintf("  %s", catInfo))
		}

		viewportContent.WriteString(line)
		viewportContent.WriteString("\n")
	}

	// Action buttons with separator
	viewportContent.WriteString("\n")
	viewportContent.WriteString(lipgloss.NewStyle().
		Foreground(FgMuted).
		Render("──────────────────────────────────"))
	viewportContent.WriteString("\n\n")

	// Select All option
	selectAllIdx := len(m.categories)
	selectAllText := "[Select All / Deselect All]"
	if m.menuIndex == selectAllIdx {
		viewportContent.WriteString(lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true).
			Render(fmt.Sprintf("> %s", selectAllText)))
	} else {
		viewportContent.WriteString(lipgloss.NewStyle().
			Foreground(FgPrimary).
			Render(fmt.Sprintf("  %s", selectAllText)))
	}
	viewportContent.WriteString("\n")

	// Clean Selected button
	cleanIdx := len(m.categories) + 1
	cleanText := "▶ Clean Selected"
	if m.menuIndex == cleanIdx {
		viewportContent.WriteString(lipgloss.NewStyle().
			Foreground(Accent).
			Bold(true).
			Render(fmt.Sprintf("> %s", cleanText)))
	} else {
		viewportContent.WriteString(lipgloss.NewStyle().
			Foreground(FgPrimary).
			Render(fmt.Sprintf("  %s", cleanText)))
	}
	viewportContent.WriteString("\n")

	// Back button
	backIdx := len(m.categories) + 2
	backText := "← Back"
	if m.menuIndex == backIdx {
		viewportContent.WriteString(lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true).
			Render(fmt.Sprintf("> %s", backText)))
	} else {
		viewportContent.WriteString(lipgloss.NewStyle().
			Foreground(FgPrimary).
			Render(fmt.Sprintf("  %s", backText)))
	}

	// Update viewport content and ensure selected item is visible
	m.categoryViewport.SetContent(viewportContent.String())

	// Auto-scroll to keep selected item visible
	// Each line is roughly 1 line height, ensure menuIndex line is visible
	if m.menuIndex*2 < m.categoryViewport.YOffset {
		m.categoryViewport.YOffset = m.menuIndex * 2
	} else if m.menuIndex*2 >= m.categoryViewport.YOffset+m.categoryViewport.Height {
		m.categoryViewport.YOffset = (m.menuIndex * 2) - m.categoryViewport.Height + 2
	}

	// Selection info
	selectedSize := m.calculateSelectedSize()
	footer := selectionInfoStyle.Render(fmt.Sprintf("Selected: %d/%d categories (%s)", m.selectedCount, len(m.categories), selectedSize))

	// Combine: header + scan summary + category viewport + footer
	return header.String() + scanSummary + m.categoryViewport.View() + "\n\n" + footer
}

// calculateSelectedSize calculates total size of selected categories
func (m Model) calculateSelectedSize() string {
	totalMB := 0
	for _, cat := range m.categories {
		if cat.Enabled {
			// Parse size string and convert to MB
			if sizeStr := strings.TrimSuffix(cat.Size, " MB"); sizeStr != cat.Size {
				if mb, err := strconv.Atoi(sizeStr); err == nil {
					totalMB += mb
				}
			}
		}
	}
	return fmt.Sprintf("%d MB", totalMB)
}

// renderConfirm renders the confirmation screen (Eldritch themed)
func (m Model) renderConfirm() string {
	var content strings.Builder

	// Warning marker and header - using Eldritch Primary color
	warnMarker := lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true).
		Render("[WARN]")

	header := lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true).
		Render("FINAL CONFIRMATION REQUIRED")

	content.WriteString(fmt.Sprintf("%s %s", warnMarker, header))
	content.WriteString("\n\n")

	// Warning text with clean formatting
	content.WriteString(lipgloss.NewStyle().
		Foreground(FgPrimary).
		Render("You are about to permanently delete:"))
	content.WriteString("\n\n")

	for _, cat := range m.categories {
		if cat.Enabled {
			item := lipgloss.NewStyle().
				Foreground(FgSecondary).
				Render(fmt.Sprintf("  • %s (%s)", cat.Name, cat.Size))
			content.WriteString(item)
			content.WriteString("\n")
		}
	}

	content.WriteString("\n")
	content.WriteString(lipgloss.NewStyle().
		Foreground(Danger).
		Bold(true).
		Render("⚠ This action CANNOT be undone!"))
	content.WriteString("\n\n")

	// Simple menu-style buttons
	content.WriteString(lipgloss.NewStyle().
		Foreground(FgSecondary).
		Render("Select an option:"))
	content.WriteString("\n\n")

	// Confirm & Clean button (first option, index 0)
	if m.menuIndex == 0 {
		content.WriteString(lipgloss.NewStyle().
			Foreground(Danger).
			Bold(true).
			Render("> Confirm & Clean"))
	} else {
		content.WriteString(lipgloss.NewStyle().
			Foreground(FgPrimary).
			Render("  Confirm & Clean"))
	}
	content.WriteString("\n")

	// Cancel button (second option, index 1)
	if m.menuIndex == 1 {
		content.WriteString(lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true).
			Render("> Cancel"))
	} else {
		content.WriteString(lipgloss.NewStyle().
			Foreground(FgPrimary).
			Render("  Cancel"))
	}

	return content.String()
}

// renderClean renders the cleaning progress screen
func (m Model) renderClean() string {
	var content strings.Builder

	// Cleaning header
	content.WriteString(cleaningHeaderStyle.Render("CLEANING IN PROGRESS"))
	content.WriteString("\n\n")

	// Current phase
	phaseText := m.currentPhase
	if m.cleanActive {
		elapsed := time.Since(m.cleanStarted)
		phaseText = fmt.Sprintf("%s (%.1fs elapsed)", m.currentPhase, elapsed.Seconds())
	}
	content.WriteString(phaseStyle.Render(phaseText))
	content.WriteString("\n\n")

	// Progress indicator (animated for cleaning with gradient)
	if m.cleanActive {
		// Animated progress bar moving back and forth with gradient
		elapsed := time.Since(m.cleanStarted).Seconds()
		barWidth := 50

		// Oscillate position between 0 and barWidth
		animProgress := 0.5 + 0.4*math.Sin(elapsed*2.0)
		filledWidth := int(animProgress * float64(barWidth))
		if filledWidth < 0 {
			filledWidth = 0
		}
		if filledWidth > barWidth {
			filledWidth = barWidth
		}

		// Create animated bar with gradient from Danger (red) to Warning (orange)
		var bar strings.Builder
		for i := 0; i < barWidth; i++ {
			if i < filledWidth {
				// Calculate gradient position across filled portion
				gradientPos := float64(i) / float64(barWidth)
				color := interpolateColor(Danger, Warning, gradientPos)
				coloredChar := lipgloss.NewStyle().
					Foreground(color).
					Render("█")
				bar.WriteString(coloredChar)
			} else {
				bar.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color("#323449")).
					Render("░"))
			}
		}

		content.WriteString(lipgloss.NewStyle().
			Align(lipgloss.Center).
			Render(bar.String()))
		content.WriteString("\n")
		content.WriteString(lipgloss.NewStyle().
			Foreground(Danger).
			Bold(true).
			Align(lipgloss.Center).
			Render("Cleaning..."))
	}

	return content.String()
}

// renderComplete renders the completion screen (sysc-greet style)
func (m Model) renderComplete() string {
	var content strings.Builder

	// Success marker and header
	successMarker := lipgloss.NewStyle().
		Foreground(Accent).
		Bold(true).
		Render("[OK]")

	header := lipgloss.NewStyle().
		Foreground(Accent).
		Bold(true).
		Render("CLEANING COMPLETE!")

	content.WriteString(fmt.Sprintf("%s %s", successMarker, header))
	content.WriteString("\n\n")

	// Stats with clean formatting
	content.WriteString(lipgloss.NewStyle().
		Foreground(FgPrimary).
		Render(fmt.Sprintf("Files Deleted:  %d", m.cleanFilesDeleted)))
	content.WriteString("\n")
	content.WriteString(lipgloss.NewStyle().
		Foreground(FgPrimary).
		Render(fmt.Sprintf("Space Freed:    %s", humanizeBytes(m.cleanBytesFreed))))
	content.WriteString("\n\n")

	// Warning if there were errors
	if m.cleanError != "" {
		warnMarker := lipgloss.NewStyle().
			Foreground(Warning).
			Render("[WARN]")

		content.WriteString(fmt.Sprintf("%s Some files could not be deleted", warnMarker))
		content.WriteString("\n\n")
	}

	// Next action
	content.WriteString(lipgloss.NewStyle().
		Foreground(FgMuted).
		Italic(true).
		Render("Press any key to return to main menu"))

	return content.String()
}

// getFooterText returns appropriate footer text (sysc-greet style)
func (m Model) getFooterText() string {
	switch m.mode {
	case ModeWelcome:
		return "↑/↓ Navigate  |  Enter Select  |  Q Quit"
	case ModeScanProgress, ModeClean:
		return "Scanning system... please wait"
	case ModeConfirm:
		return "↑/↓ Navigate  |  Enter Select  |  Esc Cancel"
	case ModeResults:
		return "Enter Continue  |  Esc Back  |  Q Quit"
	case ModeSelect:
		return "↑/↓ Navigate  |  Space Toggle  |  Enter Select  |  Esc Back"
	case ModeComplete:
		return "Press any key to continue"
	case ModeSchedule:
		return "↑/↓ Navigate  |  Enter Select  |  Esc Back  |  Q Quit"
	default:
		return "↑/↓ Navigate  |  Enter Select  |  Esc Back  |  Q Quit"
	}
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

// interpolateColor interpolates between two lipgloss colors
func interpolateColor(color1, color2 lipgloss.Color, factor float64) lipgloss.Color {
	// Parse hex colors
	c1 := string(color1)
	c2 := string(color2)

	// Extract RGB values from hex
	r1, g1, b1 := parseHexColor(c1)
	r2, g2, b2 := parseHexColor(c2)

	// Interpolate each component
	r := uint8(float64(r1)*(1-factor) + float64(r2)*factor)
	g := uint8(float64(g1)*(1-factor) + float64(g2)*factor)
	b := uint8(float64(b1)*(1-factor) + float64(b2)*factor)

	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

// parseHexColor parses a hex color string to RGB values
func parseHexColor(hex string) (uint8, uint8, uint8) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		// Default to white if invalid
		return 255, 255, 255
	}

	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)

	return uint8(r), uint8(g), uint8(b)
}

// Message types for async operations
type scanProgressMsg struct {
	Progress     float64
	Phase        string
	FilesScanned int
	BytesScanned uint64
	CurrentPath  string
}

type scanCompleteMsg struct {
	Success    bool
	Error      string
	Output     string
	Categories []config.Category
	TotalSize  uint64
	TotalFiles int
}

type cleanCompleteMsg struct {
	Success      bool
	Error        string
	Output       string
	FilesDeleted int
	BytesFreed   uint64
}

type tickMsg time.Time

// Style definitions based on sysc-greet patterns
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Secondary).
			Padding(1)

	statusStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Padding(0, 1)

	menuItemStyle = lipgloss.NewStyle().
			Foreground(FgPrimary)

	menuItemSelectedStyle = menuItemStyle.Copy().
				Bold(true).
				Foreground(Primary)

	progressHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(Primary).
				Align(lipgloss.Center)

	resultsHeaderStyle = progressHeaderStyle.Copy().
				Foreground(Accent)

	selectionHeaderStyle = progressHeaderStyle.Copy().
				Foreground(Secondary)

	warningHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(Warning).
				Align(lipgloss.Center)

	cleaningHeaderStyle = progressHeaderStyle.Copy().
				Foreground(Danger)

	successHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(Accent).
				Align(lipgloss.Center)

	phaseStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(FgPrimary).
			Align(lipgloss.Center).
			Padding(1)

	summaryStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			Align(lipgloss.Center)

	progressBarStyle = lipgloss.NewStyle().
				Foreground(Primary).
				Align(lipgloss.Center)

	progressStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Secondary).
			Align(lipgloss.Center)

	categoryHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(Secondary)

	categoryEnabledStyle = lipgloss.NewStyle().
				Foreground(Accent)

	categoryDisabledStyle = lipgloss.NewStyle().
				Foreground(FgMuted)

	categoryItemStyle = lipgloss.NewStyle().
				Foreground(FgPrimary)

	categoryItemSelectedStyle = categoryItemStyle.Copy().
					Bold(true).
					Foreground(Primary)

	categoryItemEnabledStyle = categoryItemStyle.Copy().
					Foreground(Accent).
					Bold(true)

	actionItemStyle         = menuItemStyle.Copy()
	actionItemSelectedStyle = menuItemSelectedStyle.Copy()

	selectionInfoStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(Accent).
				Align(lipgloss.Center)

	warningStyle = lipgloss.NewStyle().
			Foreground(Warning).
			Padding(1, 2)

	successStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Padding(1, 2)

	nextActionStyle = lipgloss.NewStyle().
			Foreground(FgMuted).
			Align(lipgloss.Center)

	errorStyle = lipgloss.NewStyle().
			Foreground(Danger).
			Padding(1, 2)

	buttonStyle = lipgloss.NewStyle().
			Foreground(FgPrimary).
			Background(BgSubtle).
			Padding(0, 2)

	buttonSelectedStyle = buttonStyle.Copy().
				Background(Primary).
				Foreground(lipgloss.Color("0"))

	buttonDangerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(Danger).
				Padding(0, 2).
				Bold(true)

	buttonDangerSelectedStyle = buttonDangerStyle.Copy().
					Background(lipgloss.Color("#ff0000")). // Brighter red when selected
					Bold(true)

	footerStyle = lipgloss.NewStyle().
			Foreground(FgMuted).
			Align(lipgloss.Center)
)

// showSchedule enters schedule mode
func (m Model) showSchedule() (tea.Model, tea.Cmd) {
m.mode = ModeSchedule
m.menuIndex = 0
m.currentPhase = "" // Clear any previous status messages
return m, nil
}

// renderSchedule renders the schedule management screen
func (m Model) renderSchedule() string {
var content strings.Builder

content.WriteString(lipgloss.NewStyle().
Foreground(FgSecondary).
Bold(true).
Render("Schedule Automated Cleaning"))
content.WriteString("\n\n")

// Check current timer status
scanEnabled, scanStatus := checkTimerStatus("moonbit-scan.timer")
cleanEnabled, cleanStatus := checkTimerStatus("moonbit-clean.timer")

// Display current status
content.WriteString(lipgloss.NewStyle().
Foreground(FgSecondary).
Render("Current Status:"))
content.WriteString("\n\n")

// Scan timer status
scanStatusColor := FgMuted
if scanEnabled {
scanStatusColor = Accent
}
content.WriteString(fmt.Sprintf("  %s  Scan Timer: %s\n",
getStatusIcon(scanEnabled),
lipgloss.NewStyle().Foreground(scanStatusColor).Render(scanStatus)))

// Clean timer status  
cleanStatusColor := FgMuted
if cleanEnabled {
cleanStatusColor = Accent
}
content.WriteString(fmt.Sprintf("  %s  Clean Timer: %s\n",
getStatusIcon(cleanEnabled),
lipgloss.NewStyle().Foreground(cleanStatusColor).Render(cleanStatus)))

content.WriteString("\n")

// Timer info
content.WriteString(lipgloss.NewStyle().
Foreground(FgMuted).
Render("• Scan Timer: Runs daily at 2 AM"))
content.WriteString("\n")
content.WriteString(lipgloss.NewStyle().
Foreground(FgMuted).
Render("• Clean Timer: Runs weekly on Sunday at 3 AM"))
content.WriteString("\n\n")

// Menu options
content.WriteString(lipgloss.NewStyle().
Foreground(FgSecondary).
Bold(true).
Render("Select an option:"))
content.WriteString("\n\n")

options := []string{
"Enable Scan Timer",
"Disable Scan Timer",
"Enable Clean Timer",
"Disable Clean Timer",
"← Back",
}

for i, option := range options {
var line string
if i == m.menuIndex {
line = lipgloss.NewStyle().
Foreground(Primary).
Bold(true).
Render(fmt.Sprintf("> %s", option))
} else {
line = lipgloss.NewStyle().
Foreground(FgPrimary).
Render(fmt.Sprintf("  %s", option))
}
content.WriteString(line)
content.WriteString("\n")
}

// Display status message if available
if m.currentPhase != "" {
content.WriteString("\n")
msgColor := Accent
if strings.Contains(m.currentPhase, "Failed") {
msgColor = Danger
}
content.WriteString(lipgloss.NewStyle().
Foreground(msgColor).
Render(m.currentPhase))
}

return content.String()
}

// checkTimerStatus checks if a systemd timer is enabled and active
func checkTimerStatus(timerName string) (bool, string) {
cmd := exec.Command("systemctl", "is-enabled", timerName)
output, err := cmd.CombinedOutput()
enabled := err == nil && strings.TrimSpace(string(output)) == "enabled"

cmd = exec.Command("systemctl", "is-active", timerName)
output, err = cmd.CombinedOutput()
active := err == nil && strings.TrimSpace(string(output)) == "active"

if enabled && active {
return true, "Enabled & Active"
} else if enabled {
return true, "Enabled (Inactive)"
}
return false, "Disabled"
}

// getStatusIcon returns an icon for timer status
func getStatusIcon(enabled bool) string {
if enabled {
return "✓"
}
return "✗"
}

// executeTimerCommand executes a systemctl command for a timer
// timerCommandMsg contains the result of a timer command
type timerCommandMsg struct {
success bool
message string
}

func (m Model) executeTimerCommand(action, timerName string) (tea.Model, tea.Cmd) {
return m, runTimerCommand(action, timerName)
}

// runTimerCommand executes systemctl command asynchronously
func runTimerCommand(action, timerName string) tea.Cmd {
return func() tea.Msg {
var cmd *exec.Cmd
switch action {
case "enable":
cmd = exec.Command("sudo", "systemctl", "enable", "--now", timerName)
case "disable":
cmd = exec.Command("sudo", "systemctl", "disable", "--now", timerName)
}

if cmd != nil {
if err := cmd.Run(); err != nil {
return timerCommandMsg{
success: false,
message: fmt.Sprintf("Failed to %s %s: %v", action, timerName, err),
}
}
return timerCommandMsg{
success: true,
message: fmt.Sprintf("Successfully %sd %s", action, timerName),
}
}

return timerCommandMsg{
success: false,
message: "Invalid command",
}
}
}
