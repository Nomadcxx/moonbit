# MoonBit Demo Recordings

This directory contains VHS tape files for creating demo GIFs.

## Generating Demos

**Prerequisites:**
- VHS installed: `go install github.com/charmbracelet/vhs@latest`
- Sudo access with password: `dnommm78` (temporary demo password)

**Generate the demos:**
```bash
cd demos
vhs cli-scan.tape      # Creates cli-scan.gif
vhs tui-full.tape      # Creates tui-full.gif
```

## Tape Files

- **cli-scan.tape** - CLI quick scan workflow with interactive cleaning
- **tui-full.tape** - TUI full workflow: navigation, category selection, cleaning

## Settings

All demos are configured for:
- Resolution: 1920x1080 (Full HD)
- Font Size: 18
- Theme: Catppuccin Mocha
- Includes sudo password entry

## VHS Navigation Reference

Useful keys for TUI navigation in tapes:
- `Down` / `Up` / `Left` / `Right` - Arrow keys
- `Space` - Toggle selection
- `Enter` - Confirm/Select
- `Tab` - Navigate between UI elements
- `Type "q"` - Quit
- `Down@3` - Press Down 3 times rapidly
