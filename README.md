# MoonBit

**Status: Work in Progress**

A system cleaner TUI for Linux. Scan and clean temporary files, caches, and logs.

## Quick Start

```bash
make build
sudo ./moonbit
```

## What It Does

**Cross-Distro Package Managers:**
- Arch: Pacman, Yay, Paru, Pamac
- Debian/Ubuntu: APT cache
- Fedora/RHEL: DNF cache
- openSUSE: Zypper cache

**Development Tools:**
- Python (pip), Node.js (npm), Rust (cargo)
- Java (gradle, maven), Go (build cache)

**System & User Caches:**
- Font cache, Mesa shader cache, WebKit cache
- Thumbnails (age-based filtering: >30 days old)
- Browser caches, application caches

**Media Servers:**
- Plex: transcoding temp files
- Jellyfin: transcodes and cache

**Other:**
- Docker cleanup (images, containers, volumes)
- Crash reports, system logs, trash
- Duplicate file detection with SHA256 hashing
- Interactive TUI for selective cleaning
- Automatic backup before deletion

## Commands

```bash
# Interactive TUI
moonbit

# CLI mode
moonbit scan
moonbit clean --force

# Find and remove duplicate files
moonbit duplicates find [paths...]          # Find duplicates (default: home directory)
moonbit duplicates find --min-size 10240    # Find duplicates >= 10KB
moonbit duplicates clean                    # Interactive removal (coming soon)

# Package manager cleanup (requires sudo)
moonbit pkg orphans             # List orphaned packages (dry-run)
moonbit pkg orphans --force     # Remove orphaned packages (Pacman/APT/DNF/Zypper)
moonbit pkg kernels             # List old kernels (Debian/Ubuntu only)
moonbit pkg kernels --force     # Remove old kernels (keep current + previous)

# Docker cleanup (uses docker CLI)
moonbit docker images    # Remove unused images
moonbit docker all       # Remove all unused resources

# Manage backups
moonbit backup list
moonbit backup restore <name>
```

## Automated Cleaning (Systemd)

Set up automated scans and cleaning with systemd timers:

```bash
# Install MoonBit
make install

# Install and enable systemd timers
make install-systemd
sudo systemctl enable --now moonbit-scan.timer
sudo systemctl enable --now moonbit-clean.timer

# Check status
systemctl list-timers moonbit-*
```

Default schedule:
- **Scan**: Daily at 2 AM
- **Clean**: Weekly on Sunday at 3 AM

See `systemd/README.md` for customization.

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
