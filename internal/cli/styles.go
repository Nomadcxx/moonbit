package cli

import (
	"fmt"
	"strings"
)

// ANSI color codes
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	
	// Colors
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Gray    = "\033[90m"
)

// Style helpers for consistent CLI output
type Style struct{}

var S = Style{}

func (s Style) Header(text string) string {
	return Bold + Cyan + text + Reset
}

func (s Style) Success(text string) string {
	return Green + text + Reset
}

func (s Style) Error(text string) string {
	return Red + text + Reset
}

func (s Style) Warning(text string) string {
	return Yellow + text + Reset
}

func (s Style) Info(text string) string {
	return Blue + text + Reset
}

func (s Style) Muted(text string) string {
	return Gray + text + Reset
}

func (s Style) Bold(text string) string {
	return Bold + text + Reset
}

func (s Style) Separator() string {
	return Gray + strings.Repeat("─", 50) + Reset
}

// Box creates a simple bordered box
func (s Style) Box(title string, content []string) string {
	var result strings.Builder
	
	// Top border
	result.WriteString(Gray + "┌" + strings.Repeat("─", 48) + "┐" + Reset + "\n")
	
	// Title
	if title != "" {
		titleLine := fmt.Sprintf("│ %s%-46s%s │", Bold+Cyan, title, Reset)
		result.WriteString(titleLine + "\n")
		result.WriteString(Gray + "├" + strings.Repeat("─", 48) + "┤" + Reset + "\n")
	}
	
	// Content lines
	for _, line := range content {
		contentLine := fmt.Sprintf("%s│%s %-46s %s│%s", Gray, Reset, line, Gray, Reset)
		result.WriteString(contentLine + "\n")
	}
	
	// Bottom border
	result.WriteString(Gray + "└" + strings.Repeat("─", 48) + "┘" + Reset + "\n")
	
	return result.String()
}

// Progress bar
func (s Style) Progress(current, total int, label string) string {
	width := 30
	filled := int(float64(current) / float64(total) * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	
	return fmt.Sprintf("%s[%s%s%s] %s%d/%d%s %s",
		Gray, Reset, Cyan+bar, Gray, Reset, current, total, Gray, label+Reset)
}
