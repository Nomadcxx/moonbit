package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette for MoonBit TUI (Eldritch theme)
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
	// Eldritch theme colors (Lovecraftian horror inspired)
	// Matches the theme used in the CLI installer for visual consistency
	BgBase = lipgloss.Color("#212337")     // Sunken Depths Grey
	BgElevated = lipgloss.Color("#323449") // Shallow Depths Grey
	BgSubtle = BgBase
	BgActive = BgElevated

	Primary = lipgloss.Color("#37f499")   // Great Old One Green
	Secondary = lipgloss.Color("#04d1f9") // Watery Tomb Blue
	Accent = lipgloss.Color("#a48cf2")    // Lovecraft Purple
	Warning = lipgloss.Color("#f7c67f")   // Dreaming Orange
	Danger = lipgloss.Color("#f16c75")    // R'lyeh Red

	FgPrimary = lipgloss.Color("#ebfafa")   // Lighthouse White
	FgSecondary = lipgloss.Color("#7081d0") // The Old One Purple
	FgMuted = lipgloss.Color("#7081d0")     // The Old One Purple (comments)
	FgSubtle = lipgloss.Color("#5a6aa0")    // Darker muted

	BorderDefault = lipgloss.Color("#323449") // Slightly lighter than base
	BorderFocus = Primary
}
