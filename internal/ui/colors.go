package ui

import (
	"log"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
)

// Logging for debugging
var debugLog *log.Logger

func init() {
	// Initialize debug logger with user-specific location
	var logFile *os.File
	var err error

	// Try XDG_CACHE_HOME first, then fallback to ~/.cache
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		homeDir, homeErr := os.UserHomeDir()
		if homeErr == nil {
			cacheHome = filepath.Join(homeDir, ".cache")
		}
	}

	if cacheHome != "" {
		logDir := filepath.Join(cacheHome, "moonbit")
		os.MkdirAll(logDir, 0700) // User-only permissions
		logPath := filepath.Join(logDir, "debug.log")
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600) // User-only read/write
	}

	if err != nil || logFile == nil {
		debugLog = log.New(os.Stderr, "[MOONBIT] ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		debugLog = log.New(logFile, "[MOONBIT] ", log.Ldate|log.Ltime|log.Lshortfile)
	}
}

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
	BgBase = lipgloss.Color("#212337")       // Sunken Depths Grey
	BgElevated = lipgloss.Color("#323449")   // Shallow Depths Grey
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
