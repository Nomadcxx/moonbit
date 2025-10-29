package themes

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
)

// Theme defines colors, borders, and styling for MoonBit
type Theme struct {
	Name            string
	PrimaryColor    lipgloss.Color
	SecondaryColor  lipgloss.Color
	AccentColor     lipgloss.Color
	BackgroundColor lipgloss.Color
	Border          lipgloss.Border
	Styling         struct {
		Bold      bool
		Italic    bool
		Underline bool
	}
	Colors struct {
		Background lipgloss.Color
		Foreground lipgloss.Color
		Secondary  lipgloss.Color
		Muted      lipgloss.Color
		Border     lipgloss.Color
		Focus      lipgloss.Color
		Success    lipgloss.Color
		Warning    lipgloss.Color
		Error      lipgloss.Color
		Selected   lipgloss.Color
		Highlight  lipgloss.Color
	}
}

// DefaultTheme provides a solid foundation for MoonBit
var DefaultTheme = Theme{
	Name:            "MoonBit",
	PrimaryColor:    lipgloss.Color("89"),  // Indigo
	SecondaryColor:  lipgloss.Color("123"), // Sky
	AccentColor:     lipgloss.Color("42"),  // Green
	BackgroundColor: lipgloss.Color("236"), // Dark Gray
	Border:          lipgloss.NormalBorder(),
	Colors: struct {
		Background lipgloss.Color
		Foreground lipgloss.Color
		Secondary  lipgloss.Color
		Muted      lipgloss.Color
		Border     lipgloss.Color
		Focus      lipgloss.Color
		Success    lipgloss.Color
		Warning    lipgloss.Color
		Error      lipgloss.Color
		Selected   lipgloss.Color
		Highlight  lipgloss.Color
	}{
		Background: lipgloss.Color("236"), // Dark gray
		Foreground: lipgloss.Color("248"), // Light gray
		Secondary:  lipgloss.Color("123"), // Sky blue
		Muted:      lipgloss.Color("240"), // Gray
		Border:     lipgloss.Color("238"), // Medium gray
		Focus:      lipgloss.Color("89"),  // Indigo
		Success:    lipgloss.Color("42"),  // Green
		Warning:    lipgloss.Color("214"), // Orange
		Error:      lipgloss.Color("196"), // Red
		Selected:   lipgloss.Color("89"),  // Indigo
		Highlight:  lipgloss.Color("141"), // Purple
	},
}

// LoadThemesFromDir loads themes from a directory
func LoadThemesFromDir(themesDir string) (map[string]Theme, error) {
	themes := make(map[string]Theme)

	// Always include built-in themes
	themes["default"] = DefaultTheme

	// Try to load additional themes from directory
	if _, err := os.Stat(themesDir); os.IsNotExist(err) {
		return themes, nil
	}

	files, err := os.ReadDir(themesDir)
	if err != nil {
		return themes, nil
	}

	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".toml" {
			themeName := filepath.Base(file.Name()[:len(file.Name())-len(filepath.Ext(file.Name()))])
			themePath := filepath.Join(themesDir, file.Name())

			var theme Theme
			if _, err := toml.DecodeFile(themePath, &theme); err == nil {
				theme.Name = themeName
				themes[themeName] = theme
			}
		}
	}

	return themes, nil
}

// GetTheme returns a theme by name, or default if not found
func GetTheme(name string) Theme {
	themes, _ := LoadThemesFromDir("themes")
	if theme, ok := themes[name]; ok {
		return theme
	}
	return DefaultTheme
}
