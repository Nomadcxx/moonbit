package cli

import (
	"fmt"
	"strings"
)

// ANSI color codes - Eldritch theme
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	
	// Eldritch colors (using closest ANSI approximations)
	Red     = "\033[38;2;241;108;117m"  // R'lyeh' Red #f16c75
	Green   = "\033[38;2;55;244;153m"   // Great Old One Green #37f499
	Yellow  = "\033[38;2;241;252;121m"  // Gold of Yuggoth #f1fc79
	Cyan    = "\033[38;2;4;209;249m"    // Watery Tomb Blue #04d1f9
	Purple  = "\033[38;2;164;140;242m"  // Lovecraft Purple #a48cf2
	Comment = "\033[38;2;112;129;208m"  // The Old One Purple #7081d0
	Gray    = "\033[38;2;112;129;208m"  // Using comment color for muted
)

// Style helpers for consistent CLI output
type Style struct{}

var S = Style{}

func (s Style) Header(text string) string {
	return Bold + Green + text + Reset  // Eldritch primary green
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
	return Cyan + text + Reset  // Watery Tomb Blue
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

// ASCIIHeader returns the MoonBit ASCII art header
func (s Style) ASCIIHeader() string {
ascii := `
█▀▄▀█ ▄▀▀▀▄ ▄▀▀▀▄ █▄  █ █▀▀▀▄ ▀▀█▀▀ ▀▀█▀▀    ▄▀    ▄▀ 
█   █ █   █ █   █ █ ▀▄█ █▀▀▀▄   █     █    ▄▀    ▄▀   
▀   ▀  ▀▀▀   ▀▀▀  ▀   ▀ ▀▀▀▀  ▀▀▀▀▀   ▀   ▀     ▀
`
return Green + ascii + Reset
}
