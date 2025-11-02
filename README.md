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
- Finds temporary files and user caches
- Interactive TUI for selecting what to clean
- Backup before deletion

## Commands

```bash
# Interactive TUI
moonbit

# CLI mode
moonbit scan
moonbit clean --force

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

Todo:
- [ ] More cleanup targets (Docker, pip, cargo, npm, flatpak)
- [ ] Quick vs Deep clean modes
- [ ] Multi-distro support (apt, dnf, zypper)
- [ ] Better TUI with file preview

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
