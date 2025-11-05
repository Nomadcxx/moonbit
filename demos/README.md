# MoonBit Demo Recordings

This directory contains VHS tape files for creating demo GIFs.

## Generating Demos

**Prerequisites:**
- VHS installed: `go install github.com/charmbracelet/vhs@latest`

**Build the demo version (no sudo required):**
```bash
cd moonbit-demo
go build -o moonbit-demo cmd/main.go
```

**Generate the demos:**
```bash
cd demos
vhs cli-scan-demo.tape      # Creates cli-scan.gif
vhs tui-full-demo.tape      # Creates tui-full.gif
```

The demo version has sudo checks removed so VHS can run it without elevation.

## Tape Files

- **cli-scan-demo.tape** - CLI quick scan workflow with interactive cleaning
- **tui-full-demo.tape** - TUI full workflow: navigation, category selection, cleaning

## Settings

All demos are configured for:
- Resolution: 1920x1080 (Full HD)
- Font Size: 18
- Theme: Catppuccin Mocha
- Uses `moonbit-demo` binary (sudo checks removed)

## VHS Navigation Reference

Useful keys for TUI navigation in tapes:
- `Down` / `Up` / `Left` / `Right` - Arrow keys
- `Space` - Toggle selection
- `Enter` - Confirm/Select
- `Tab` - Navigate between UI elements
- `Type "q"` - Quit
- `Down@3` - Press Down 3 times rapidly

## Notes

- The `moonbit-demo/` directory is not tracked in git (temporary build)
- Generated GIFs are not tracked either (add them manually when ready)
