# MoonBit

**Status: Work in Progress**

A system cleaner TUI for Linux. Scan and clean temporary files, caches, and logs.

## Quick Start

```bash
make build
sudo ./moonbit
```

## What It Does

- Scans package manager caches (Pacman, Yay, Paru, Pamac)
- Development tool caches (pip, npm, cargo, gradle, maven, go)
- Docker cleanup (images, containers, volumes)
- System caches and logs
- Interactive TUI for selecting what to clean
- Backup before deletion

## Commands

```bash
# Interactive TUI
moonbit

# CLI mode
moonbit scan
moonbit clean --force

# Docker cleanup (uses docker CLI)
moonbit docker images    # Remove unused images
moonbit docker all       # Remove all unused resources

# Manage backups
moonbit backup list
moonbit backup restore <name>
```

## Current Status

Working:
- [x] Basic scanning and cleaning
- [x] Interactive category selection
- [x] Backup/restore functionality
- [x] Safety checks (protected paths, size limits)
- [x] Development tool caches (pip, npm, cargo, gradle, maven, go)
- [x] Docker cleanup commands

Todo:
- [ ] Quick vs Deep clean modes
- [ ] Multi-distro support (apt, dnf, zypper)
- [ ] Better TUI with file preview
- [ ] Additional cleanup targets (VSCode extensions, browser data)

## Development

```bash
make build    # Build binary
make test     # Run tests
make install  # Install to /usr/local/bin
```

See [CLAUDE.md](CLAUDE.md) for architecture details.

## Requirements

- Go 1.21+
- Linux (Arch-based tested, others should work)
- sudo for system paths

## License

MIT - see [LICENSE](LICENSE)
