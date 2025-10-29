package cli

import (
	"fmt"
	"os"

	"github.com/Nomadcxx/moonbit/internal/ui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "moonbit",
	Short: "MoonBit – system cleaner TUI",
	Long: `MoonBit is a Go-based TUI application for system cleaning and privacy scrubbing.
It provides interactive scanning, previewing, and selective deletion of temporary files,
caches, logs, and application data on Linux (Arch-primary).

Features:
• Interactive TUI with beautiful theming (sysc-greet inspired)
• Safe dry-runs and undo mechanisms
• Parallel scanning with progress tracking
• Multiple cleaning categories (Pacman cache, temporary files, browser cache, etc.)
• JSON output for automation and launcher integration`,
	Run: func(cmd *cobra.Command, args []string) {
		// Start Bubble Tea UI with MoonBit model
		ui.Start()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
