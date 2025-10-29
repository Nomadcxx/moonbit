package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ASCII header from ascii.txt
const asciiHeader = `
█▀▄▀█ ▄▀▀▀▄ ▄▀▀▀▄ █▄  █ █▀▀▀▄ ▀▀█▀▀ ▀▀█▀▀    ▄▀    ▄▀ 
█   █ █   █ █   █ █ ▀▄█ █▀▀▀▄   █     █    ▄▀    ▄▀   
▀   ▀  ▀▀▀   ▀▀▀  ▀   ▀ ▀▀▀▀  ▀▀▀▀▀   ▀   ▀     ▀    

`

// View modes for the TUI
type ViewMode string

const (
	ModeMain     ViewMode = "main"
	ModeScan     ViewMode = "scan"
	ModeClean    ViewMode = "clean"
	ModePreview  ViewMode = "preview"
	ModeSettings ViewMode = "settings"
	ModeThemes   ViewMode = "themes"
	ModeAbout    ViewMode = "about"
)

// Model represents the MoonBit TUI state
type Model struct {
	// Terminal dimensions
	width  int
	height int

	// Menu system
	menuOptions []string
	menuIndex   int
	mode        ViewMode

	// Current theme
	currentTheme string

	// Scan summary data
	scanResults  []scanResult
	scanComplete bool
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
	return Model{
		width:  80,
		height: 24,
		mode:   ModeMain,
		menuOptions: []string{
			"Scan System",
			"Clean Files",
			"Preview Changes",
			"Settings",
			"Themes",
			"About MoonBit",
			"Quit",
		},
		menuIndex:    0,
		currentTheme: "moonbit",
	}
}

// Implement tea.Model interface
func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up":
			if m.menuIndex > 0 {
				m.menuIndex--
			}
		case "down":
			if m.menuIndex < len(m.menuOptions)-1 {
				m.menuIndex++
			}
		case "enter", " ":
			// Handle menu selection
			switch m.menuIndex {
			case 0: // Scan System
				return m.navigateToScanMode()
			case 1: // Clean Files
				m.mode = ModeClean
			case 2: // Preview Changes
				m.mode = ModePreview
			case 3: // Settings
				m.mode = ModeSettings
			case 4: // Themes
				m.mode = ModeThemes
			case 5: // About
				m.mode = ModeAbout
			case 6: // Quit
				return m, tea.Quit
			}
		case "r":
			// Reset to main menu
			m.mode = ModeMain
			m.menuIndex = 0
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// Navigate to scan mode and load simulated data
func (m Model) navigateToScanMode() (Model, tea.Cmd) {
	m.mode = ModeScan
	m.scanComplete = true

	// Simulate scan results for demonstration
	m.scanResults = []scanResult{
		{"System Cache", 1247, "156 MB", "2.3s", "Complete"},
		{"Pacman Cache", 342, "89 MB", "1.1s", "Complete"},
		{"Browser Cache", 2156, "234 MB", "4.2s", "Complete"},
		{"Thumbnails", 892, "45 MB", "0.8s", "Complete"},
		{"Log Files", 156, "12 MB", "0.3s", "Complete"},
	}

	return m, nil
}

// Render the TUI view
func (m Model) View() string {
	// Create styles using our color system
	headerStyle := lipgloss.NewStyle().
		Foreground(FgPrimary).
		SetString(asciiHeader)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(Primary).
		Align(lipgloss.Center)

	menuStyle := lipgloss.NewStyle().
		Foreground(FgPrimary)

	selectedStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(Accent)

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(BorderDefault)

	helpStyle := lipgloss.NewStyle().
		Foreground(FgMuted).
		Align(lipgloss.Center)

	var view strings.Builder

	switch m.mode {
	case ModeMain:
		// Main menu with ASCII header
		view.WriteString(headerStyle.Render() + "\n")
		view.WriteString(titleStyle.Render("System Cleaner") + "\n\n")

		// Menu items
		for i, option := range m.menuOptions {
			style := menuStyle
			if i == m.menuIndex {
				style = selectedStyle
			}
			view.WriteString(style.Render(formatMenuItem(option, i == m.menuIndex)) + "\n")
		}

		// Footer
		view.WriteString("\n")
		view.WriteString(helpStyle.Render("↑↓ Navigate • Enter Select • Q/Ctrl+C Quit • R Reset"))

	case ModeScan:
		// Scan mode with table-like display
		view.WriteString(renderScanHeader())
		view.WriteString(renderScanTable(m.scanResults))
		view.WriteString(renderScanSummary(m.scanResults))

	case ModeClean:
		view.WriteString(renderScanHeader())
		view.WriteString("\n")
		view.WriteString(menuStyle.Render("Clean Mode - Select categories to clean files") + "\n\n")
		view.WriteString(helpStyle.Render("R Reset • Q/Ctrl+C Quit"))

	case ModePreview:
		view.WriteString(renderScanHeader())
		view.WriteString("\n")
		view.WriteString(menuStyle.Render("Preview Mode - Review changes before cleaning") + "\n\n")
		view.WriteString(helpStyle.Render("R Reset • Q/Ctrl+C Quit"))

	case ModeSettings, ModeThemes, ModeAbout:
		// Settings, themes, and about views
		view.WriteString(headerStyle.Render() + "\n")
		view.WriteString(titleStyle.Render("Settings") + "\n\n")
		view.WriteString(menuStyle.Render("Settings coming soon...") + "\n\n")
		view.WriteString(helpStyle.Render("R Reset • Q/Ctrl+C Quit"))
	}

	return borderStyle.Render(view.String())
}

// Render scan header (minimal emoji usage as requested)
func renderScanHeader() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(Secondary).
		SetString("📊 MoonBit Scan Summary:")

	return header.Render()
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

// Format menu item with selection indicator
func formatMenuItem(item string, selected bool) string {
	if selected {
		return "▶ " + item
	}
	return "  " + item
}

// Start launches the MoonBit TUI
func Start() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		// Handle error properly
		if debugLog != nil {
			debugLog.Printf("Error running MoonBit UI: %v", err)
		}
	}
}
