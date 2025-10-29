package ui

import (
	"log"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Logging for debugging
var debugLog *log.Logger

func init() {
	// Initialize debug logger
	logFile, err := os.OpenFile("/tmp/moonbit-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		debugLog = log.New(os.Stderr, "[MOONBIT] ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		debugLog = log.New(logFile, "[MOONBIT] ", log.Ldate|log.Ltime|log.Lshortfile)
	}
}

// TTY-safe colors with profile detection (based on sysc-greet system)
var (
	// Background colors
	BgBase     lipgloss.Color
	BgElevated lipgloss.Color
	BgSubtle   lipgloss.Color
	BgActive   lipgloss.Color

	// Primary brand colors
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Warning   lipgloss.Color
	Danger    lipgloss.Color

	// Text colors
	FgPrimary   lipgloss.Color
	FgSecondary lipgloss.Color
	FgMuted     lipgloss.Color
	FgSubtle    lipgloss.Color

	// Border colors
	BorderDefault lipgloss.Color
	BorderFocus   lipgloss.Color
)

func init() {
	// Initialize colors with TTY fallbacks
	// Base background - fallback to black on TTY
	BgBase = lipgloss.Color("235") // ANSI256 dark gray
	BgElevated = BgBase
	BgSubtle = BgBase
	BgActive = BgBase

	// Primary blue (MoonBit brand)
	Primary = lipgloss.Color("39") // ANSI256 bright blue

	// Secondary cyan
	Secondary = lipgloss.Color("45") // ANSI256 cyan

	// Accent green
	Accent = lipgloss.Color("42") // ANSI256 green

	// Warning amber
	Warning = lipgloss.Color("214") // ANSI256 orange

	// Danger red
	Danger = lipgloss.Color("196") // ANSI256 red

	// Primary text - white
	FgPrimary = lipgloss.Color("255") // ANSI256 white

	// Secondary text - light gray
	FgSecondary = lipgloss.Color("252") // ANSI256 light gray

	// Muted text - gray
	FgMuted = lipgloss.Color("244") // ANSI256 gray

	// Subtle text - dark gray
	FgSubtle = lipgloss.Color("240") // ANSI256 dark gray

	// Border default - dark gray
	BorderDefault = lipgloss.Color("238") // ANSI256 dark gray

	BorderFocus = Primary
}

// Theme application based on sysc-greet's pattern
func applyTheme(themeName string) {
	switch themeName {
	case "dracula":
		BgBase = lipgloss.Color("#282a36")
		Primary = lipgloss.Color("#bd93f9")
		Secondary = lipgloss.Color("#8be9fd")
		Accent = lipgloss.Color("#50fa7b")
		FgPrimary = lipgloss.Color("#f8f8f2")
		FgSecondary = lipgloss.Color("#f1f2f6")
		FgMuted = lipgloss.Color("#6272a4")
		BorderDefault = lipgloss.Color("#6272a4")
		BorderFocus = Primary

	case "nord":
		BgBase = lipgloss.Color("#2e3440")
		Primary = lipgloss.Color("#81a1c1")
		Secondary = lipgloss.Color("#88c0d0")
		Accent = lipgloss.Color("#8fbcbb")
		FgPrimary = lipgloss.Color("#eceff4")
		FgSecondary = lipgloss.Color("#e5e9f0")
		FgMuted = lipgloss.Color("#d8dee9")
		BorderDefault = lipgloss.Color("#4c566a")
		BorderFocus = Primary

	case "gruvbox":
		BgBase = lipgloss.Color("#282828")
		Primary = lipgloss.Color("#fe8019")
		Secondary = lipgloss.Color("#8ec07c")
		Accent = lipgloss.Color("#fabd2f")
		FgPrimary = lipgloss.Color("#ebdbb2")
		FgSecondary = lipgloss.Color("#d5c4a1")
		FgMuted = lipgloss.Color("#bdae93")
		BorderDefault = lipgloss.Color("#3c3836")
		BorderFocus = Primary

	default: // Default/MoonBit theme
		BgBase = lipgloss.Color("#1a1a1a")
		Primary = lipgloss.Color("#0ea5e9")
		Secondary = lipgloss.Color("#06b6d4")
		Accent = lipgloss.Color("#10b981")
		FgPrimary = lipgloss.Color("#f8fafc")
		FgSecondary = lipgloss.Color("#cbd5e1")
		FgMuted = lipgloss.Color("#94a3b8")
		BorderDefault = lipgloss.Color("#374151")
		BorderFocus = Primary
	}
}
