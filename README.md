<div align="center">
  <img src="logo.png" alt="MoonBit" />
</div>

**Status: Work in Progress**

A system cleaner TUI for Linux. Scan and clean temporary files, caches, and logs.

## Installation

### Quick Install (Recommended)

Install with a single command:

```bash
curl -sSL https://raw.githubusercontent.com/Nomadcxx/moonbit/main/install.sh | sudo bash
```

This will:
- Download and build MoonBit
- Launch the interactive installer
- Let you choose your auto-cleaning schedule (daily, weekly, or manual)
- Install the binary to `/usr/local/bin` so you can run `moonbit` from anywhere

### Manual Installation

If you prefer to install manually:

```bash
# Clone the repository
git clone https://github.com/Nomadcxx/moonbit.git
cd moonbit

# Build and run the installer
make installer
sudo ./moonbit-installer
```

Or build and install directly:

```bash
make build
sudo make install
```

**Note**: MoonBit requires root access to scan and clean system directories like `/var/cache`, `/var/log`, and package manager caches.

## Quick Start

After installation, run MoonBit from anywhere:

```bash
sudo moonbit     # Launch interactive TUI
```

The TUI lets you:
- Scan your system for cleanable files
- Preview what will be deleted
- Selectively choose categories to clean
- See space savings before confirming

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

The installer automatically sets up systemd timers based on your preference:

- **Daily**: Scan daily at 2 AM, clean weekly on Sunday at 3 AM
- **Weekly**: Scan and clean weekly on Sunday at 3 AM
- **Manual**: No automation, run `moonbit` manually as needed

To manually install systemd timers without using the installer:

```bash
# Install and enable systemd timers
make install-systemd
sudo systemctl enable --now moonbit-scan.timer
sudo systemctl enable --now moonbit-clean.timer

# Check status
systemctl list-timers moonbit-*
```

See `systemd/README.md` for customization.

## Current Status

Working:
- [x] Basic scanning and cleaning
- [x] Interactive category selection
- [x] Backup/restore functionality
- [x] Safety checks (protected paths, size limits)
- [x] Development tool caches (pip, npm, cargo, gradle, maven, go)
- [x] Docker cleanup commands


## Development

```bash
make build    # Build binary
make test     # Run tests
make install  # Install to /usr/local/bin
```

## Requirements

- Go 1.21+
- Linux (Arch-based tested, others should work)
- sudo for system paths

## License

MIT - see [LICENSE](LICENSE)
