package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/cleaner"
	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ASCII header from sysc-greet inspired design
const asciiHeader = `
██▄▀█ ▄▀▀▀▄ ▄▀▀▀▄ ▄▀  █ █▀▀▀▄ ▀▀█▀▀ ▀▀█▀▀    ▄▀    ▄▀ 
█   █ █   █ █   █ █ ▀▄█ █▀▀▀▄   █     █    ▄▀    ▄▀   
▀   ▀  ▀▀▀   ▀▀▀  ▀   ▀ ▀▀▀▀  ▀▀▀▀▀   ▀   ▀     ▀    

System Cleaner for Linux
`

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
)

// SessionCache mirrors CLI cache structure
type SessionCache struct {
	ScanResults *config.Category `json:"scan_results"`
	TotalSize   uint64           `json:"total_size"`
	TotalFiles  int              `json:"total_files"`
	ScannedAt   time.Time        `json:"scanned_at"`
}

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

	// Scan state
	scanActive   bool
	scanStarted  time.Time
	scanOutput   strings.Builder
	scanResults  *SessionCache
	scanProgress float64
	currentPhase string
	scanError    string

	// Clean state
	cleanActive  bool
	cleanStarted time.Time
	cleanError   string

	// Categories for selection
	categories    []CategoryInfo
	selectedCount int

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
			"Quick Scan",
			"Review Results",
			"Clean System",
			"Exit",
		},
		cfg: cfg,
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
	// Process message based on type
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		// Update progress from shared state if scanning
		if m.scanActive && currentProgress.totalFiles > 0 {
			progress := float64(currentProgress.filesScanned) / float64(currentProgress.totalFiles)
			m.scanProgress = progress
			if currentProgress.currentFile != "" {
				m.currentPhase = fmt.Sprintf("Scanning: %s", filepath.Base(currentProgress.currentFile))
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
		return m, nil
	case scanCompleteMsg:
		return m.handleScanComplete(msg)
	case cleanCompleteMsg:
		return m.handleCleanComplete(msg)
	}

	return m, nil
}

// handleKey processes keyboard input
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			maxIndex = 1 // Cancel and Confirm
		}
		
		if m.menuIndex < maxIndex {
			m.menuIndex++
		}
	case "enter", " ":
		return m.handleMenuSelect()
	case "esc":
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
			return m.startScan()
		case 1: // Review Results
			return m.showResults()
		case 2: // Clean System
			return m.startClean()
		case 3: // Exit
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
		if m.menuIndex == 0 { // Cancel
			m.mode = ModeSelect
			m.menuIndex = 0
		} else { // Confirm
			return m.executeClean()
		}
	case ModeSelect:
		totalOptions := len(m.categories) + 3 // categories + Select All + Clean + Back
		
		if m.menuIndex == totalOptions - 1 { // Back
			m.mode = ModeResults
			m.menuIndex = 0
		} else if m.menuIndex == totalOptions - 2 { // Clean Selected
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
	m.currentPhase = "Starting scan..."
	m.scanOutput.Reset()
	
	// Reset progress state
	currentProgress = progressState{}

	return m, tea.Batch(runScanCmd(m.cfg), tick())
}

// runScanCmd executes the scan using the scanner package directly
func runScanCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		s := scanner.NewScanner(cfg)

		var scannedCategories []config.Category
		var totalSize uint64
		var totalFiles int

		// Calculate total expected files for progress tracking
		expectedFiles := 0
		for _, category := range cfg.Categories {
			if category.Selected {
				expectedFiles += 1000 // Estimate, will be updated during scan
			}
		}
		currentProgress.totalFiles = expectedFiles

		// Scan each category
		for _, category := range cfg.Categories {
			if !category.Selected {
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
				if msg.Progress != nil {
					// Update shared progress state for ticker to read
					currentProgress.filesScanned = msg.Progress.FilesScanned
					currentProgress.bytesScanned = msg.Progress.Bytes
					currentProgress.currentFile = msg.Progress.Path
				}
				
				if msg.Complete != nil {
					scannedCategories = append(scannedCategories, *msg.Complete.Stats)
					totalSize += msg.Complete.Stats.Size
					totalFiles += msg.Complete.Stats.FileCount
					// Update total files with actual count
					currentProgress.totalFiles = totalFiles
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
		cache := &SessionCache{
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
func (m *Model) parseScanResults(cache *SessionCache, categories []config.Category) {
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
		
		for _, cat := range m.cfg.Categories {
			if cat.Selected {
				categoryMap[cat.Name] = &CategoryInfo{
					Name:    cat.Name,
					Files:   0,
					Size:    "0 B",
					Enabled: true,
				}
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
	m.menuIndex = 0
	return m, nil
}

// executeClean performs the actual cleaning
func (m Model) executeClean() (tea.Model, tea.Cmd) {
	m.mode = ModeClean
	m.menuIndex = 0
	m.cleanActive = true
	m.cleanStarted = time.Now()
	m.currentPhase = "Cleaning in progress..."

	return m, runCleanCmd(m.cfg, m.scanResults)
}

// runCleanCmd executes cleaning using the cleaner package
func runCleanCmd(cfg *config.Config, cache *SessionCache) tea.Cmd {
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
func (m Model) loadSessionCache() (*SessionCache, error) {
	homeDir, _ := os.UserHomeDir()
	cachePath := filepath.Join(homeDir, ".cache", "moonbit", "scan_results.json")

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

// saveSessionCache saves scan results to cache
func saveSessionCache(cache *SessionCache) error {
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

// View renders the UI
func (m Model) View() string {
	var content strings.Builder

	// Main header
	content.WriteString(headerStyle.Render(asciiHeader))
	content.WriteString("\n\n")

	switch m.mode {
	case ModeWelcome:
		content.WriteString(m.renderWelcome())
	case ModeScanProgress:
		content.WriteString(m.renderScanProgress())
	case ModeResults:
		content.WriteString(m.renderResults())
	case ModeSelect:
		content.WriteString(m.renderSelect())
	case ModeConfirm:
		content.WriteString(m.renderConfirm())
	case ModeClean:
		content.WriteString(m.renderClean())
	case ModeComplete:
		content.WriteString(m.renderComplete())
	}

	// Footer
	content.WriteString("\n\n")
	content.WriteString(footerStyle.Render(m.getFooterText()))

	return content.String()
}

// renderWelcome renders the welcome screen
func (m Model) renderWelcome() string {
	var content strings.Builder

	// System status
	if m.scanResults != nil {
		lastScan := fmt.Sprintf("Last scan: %d files (%s)",
			m.scanResults.TotalFiles, humanizeBytes(m.scanResults.TotalSize))
		content.WriteString(statusStyle.Render(lastScan))
		content.WriteString("\n\n")
	}

	// Menu
	for i, option := range m.menuOptions {
		prefix := "  "
		if i == m.menuIndex {
			prefix = "> "
		}
		style := menuItemStyle
		if i == m.menuIndex {
			style = menuItemSelectedStyle
		}
		content.WriteString(style.Render(prefix + option))
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
		if currentProgress.filesScanned > 0 {
			phaseText = fmt.Sprintf("%s - %d files (%s) - %.1fs",
				m.currentPhase,
				currentProgress.filesScanned,
				humanizeBytes(currentProgress.bytesScanned),
				elapsed.Seconds())
		} else {
			phaseText = fmt.Sprintf("%s (%.1fs elapsed)", m.currentPhase, elapsed.Seconds())
		}
	}
	content.WriteString(phaseStyle.Render(phaseText))
	content.WriteString("\n\n")

	// Progress bar with real-time updates
	progress := int(m.scanProgress * 50)
	if m.scanActive {
		if m.scanProgress > 0 && m.scanProgress < 1 {
			// Show real progress
			progressBar := strings.Repeat("█", progress) + strings.Repeat("░", 50-progress)
			content.WriteString(progressBarStyle.Render(fmt.Sprintf("[%s] %.1f%%", progressBar, m.scanProgress*100)))
		} else {
			// Show indeterminate progress animation
			animFrame := int(time.Since(m.scanStarted).Milliseconds()/100) % 50
			progressBar := strings.Repeat("░", animFrame) + "█" + strings.Repeat("░", 49-animFrame)
			content.WriteString(progressBarStyle.Render(fmt.Sprintf("[%s] Scanning...", progressBar)))
		}
	} else {
		progressBar := strings.Repeat("█", progress) + strings.Repeat("░", 50-progress)
		content.WriteString(progressBarStyle.Render(fmt.Sprintf("[%s] %.1f%%", progressBar, m.scanProgress*100)))
	}
	content.WriteString("\n\n")

	// Show current file being scanned
	if m.scanActive && currentProgress.currentFile != "" {
		currentFile := currentProgress.currentFile
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
	var content strings.Builder

	// Results header
	content.WriteString(resultsHeaderStyle.Render("SCAN RESULTS"))
	content.WriteString("\n\n")

	// Show error if present
	if m.scanError != "" {
		content.WriteString(errorStyle.Render("⚠️  Error: " + m.scanError))
		content.WriteString("\n\n")
		content.WriteString(nextActionStyle.Render("Press Esc to return to main menu"))
		return content.String()
	}

	if m.scanResults != nil && len(m.categories) > 0 {
		// Summary stats
		summary := fmt.Sprintf("Found %d cleanable files (%s)",
			m.scanResults.TotalFiles, humanizeBytes(m.scanResults.TotalSize))
		content.WriteString(summaryStyle.Render(summary))
		content.WriteString("\n\n")

		// Category breakdown
		content.WriteString(categoryHeaderStyle.Render("CATEGORIES"))
		content.WriteString("\n")

		for _, cat := range m.categories {
			line := fmt.Sprintf("📁 %s: %s (%d files)", cat.Name, cat.Size, cat.Files)
			if cat.Enabled {
				content.WriteString(categoryEnabledStyle.Render("  ✓ " + line))
			} else {
				content.WriteString(categoryDisabledStyle.Render("  ○ " + line))
			}
			content.WriteString("\n")
		}

		content.WriteString("\n")
		content.WriteString(nextActionStyle.Render("Press Enter to select categories for cleaning"))
	} else {
		content.WriteString(errorStyle.Render("No scan results available"))
		content.WriteString("\n\n")
		content.WriteString(nextActionStyle.Render("Press Esc to return to main menu and run a scan"))
	}

	return content.String()
}

// renderSelect renders the category selection screen
func (m Model) renderSelect() string {
	var content strings.Builder

	// Selection header
	content.WriteString(selectionHeaderStyle.Render("SELECT CATEGORIES TO CLEAN"))
	content.WriteString("\n\n")

	// Categories
	for i, cat := range m.categories {
		prefix := "  "
		style := categoryItemStyle
		if i == m.menuIndex {
			prefix = "> "
			style = categoryItemSelectedStyle
		}

		indicator := "[ ]"
		if cat.Enabled {
			indicator = "[X]"
		}

		// Apply enabled style on top if checked
		if cat.Enabled && i != m.menuIndex {
			style = categoryItemEnabledStyle
		}

		line := fmt.Sprintf("%s%s %s - %s (%d files)", prefix, indicator, cat.Name, cat.Size, cat.Files)
		content.WriteString(style.Render(line))
		content.WriteString("\n")
	}

	// Select All option
	content.WriteString("\n")
	selectAllIdx := len(m.categories)
	if m.menuIndex == selectAllIdx {
		content.WriteString(actionItemSelectedStyle.Render("> [Select All / Deselect All]"))
	} else {
		content.WriteString(actionItemStyle.Render("  [Select All / Deselect All]"))
	}
	content.WriteString("\n\n")

	// Clean Selected button
	cleanIdx := len(m.categories) + 1
	if m.menuIndex == cleanIdx {
		content.WriteString(actionItemSelectedStyle.Render("> ▶ Clean Selected"))
	} else {
		content.WriteString(actionItemStyle.Render("  ▶ Clean Selected"))
	}
	content.WriteString("\n")
	
	// Back button
	backIdx := len(m.categories) + 2
	if m.menuIndex == backIdx {
		content.WriteString(actionItemSelectedStyle.Render("> ← Back"))
	} else {
		content.WriteString(actionItemStyle.Render("  ← Back"))
	}

	// Selection info
	selectedSize := m.calculateSelectedSize()
	content.WriteString("\n\n")
	content.WriteString(selectionInfoStyle.Render(fmt.Sprintf("Selected: %d/%d categories (%s)", m.selectedCount, len(m.categories), selectedSize)))

	return content.String()
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

// renderConfirm renders the confirmation screen
func (m Model) renderConfirm() string {
	var content strings.Builder

	// Warning header
	content.WriteString(warningHeaderStyle.Render("FINAL CONFIRMATION REQUIRED"))
	content.WriteString("\n\n")

	// Warning text
	warning := "You are about to permanently delete:\n"
	for _, cat := range m.categories {
		if cat.Enabled {
			warning += fmt.Sprintf("• %s (%s)\n", cat.Name, cat.Size)
		}
	}
	warning += "\nThis action CANNOT be undone!"

	content.WriteString(warningStyle.Render(warning))
	content.WriteString("\n\n")

	// Confirmation buttons
	if m.menuIndex == 0 {
		content.WriteString(buttonSelectedStyle.Render("  Cancel  "))
	} else {
		content.WriteString(buttonStyle.Render("  Cancel  "))
	}

	content.WriteString("  ")

	if m.menuIndex == 1 {
		content.WriteString(buttonSelectedStyle.Render("  Confirm & Clean  "))
	} else {
		content.WriteString(buttonStyle.Render("  Confirm & Clean  "))
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
	content.WriteString(phaseStyle.Render(m.currentPhase))
	content.WriteString("\n\n")

	// Progress indicator
	content.WriteString(progressStyle.Render("Cleaning files..."))

	return content.String()
}

// renderComplete renders the completion screen
func (m Model) renderComplete() string {
	var content strings.Builder

	// Success header
	content.WriteString(successHeaderStyle.Render("CLEANING COMPLETE!"))
	content.WriteString("\n\n")

	// Success message
	success := "Your system has been optimized!\n\n"
	success += "Cache files and temporary data have been successfully removed."
	content.WriteString(successStyle.Render(success))
	content.WriteString("\n\n")

	// Next action
	content.WriteString(nextActionStyle.Render("Press Enter to return to main menu"))

	return content.String()
}

// getFooterText returns appropriate footer text
func (m Model) getFooterText() string {
	switch m.mode {
	case ModeWelcome:
		return "↑↓ Navigate • Enter Select • Esc Back • Q Quit"
	case ModeScanProgress, ModeClean:
		return "Processing... • Esc Back"
	case ModeConfirm:
		return "↑↓ Navigate • Enter Select • Esc Back"
	default:
		return "↑↓ Navigate • Enter Select • Esc Back • Q Quit"
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

// progressState holds shared progress information
type progressState struct {
	filesScanned int
	bytesScanned uint64
	currentFile  string
	totalFiles   int
}

var currentProgress progressState

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

	footerStyle = lipgloss.NewStyle().
			Foreground(FgMuted).
			Align(lipgloss.Center)
)
