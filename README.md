<div align="center">
  <img src="logo.png" alt="MoonBit" />
  
  **A modern system cleaner built in Go with a TUI and CLI**
  
  ![Eldritch Theme](https://img.shields.io/badge/theme-eldritch-37f499?style=flat-square)
  ![License](https://img.shields.io/badge/license-MIT-blue?style=flat-square)
</div>

---
![TUI Demo](demos/tui-full.gif)

## Features
- **Distro Support**: Arch, Debian/Ubuntu, Fedora/RHEL, openSUSE
- **Package Managers**: Pacman, APT, DNF, Zypper, AUR helpers (yay, paru)
- **System Caches**: Font, Mesa shader, WebKit, thumbnails, browser caches
- **Docker Cleanup**: Images, containers, volumes, build cache
- **Media Servers**: Plex and Jellyfin transcoding cleanup
- **Automated Scheduling**: Systemd timers for regular maintenance

## Installation

### Arch Linux (AUR)

```bash
yay -S moonbit
# or
paru -S moonbit
```

### Quick Install Script

```bash
curl -sSL https://raw.githubusercontent.com/Nomadcxx/moonbit/main/install.sh | sudo bash
```

### Manual Build

**Requirements:** Go 1.21+

```bash
git clone https://github.com/Nomadcxx/moonbit.git
cd moonbit
make installer
sudo ./moonbit-installer
```
## Usage

### Interactive TUI

```bash
moonbit              # Launch interactive TUI
```

The TUI offers two scan modes:
- **Quick Scan** - Fast scan of safe caches only (~25 categories)
- **Deep Scan** - Comprehensive scan of all categories including system logs

### CLI Commands

```bash
# Scanning
moonbit scan                    # Standard scan
moonbit scan --mode quick       # Quick scan (safe caches only)
moonbit scan --mode deep        # Deep scan (all categories)

# Cleaning
moonbit clean                   # Preview what would be deleted (dry-run)
moonbit clean --force           # Actually delete files
moonbit clean --mode quick      # Clean only quick scan categories
moonbit clean --mode deep       # Clean all scanned categories

# Package manager cleanup
moonbit pkg orphans             # Remove orphaned packages
moonbit pkg kernels             # Remove old kernels (Debian/Ubuntu)

# Docker cleanup
moonbit docker images           # Remove unused images
moonbit docker all              # Remove all unused resources

# Find duplicates
moonbit duplicates find                    # Find duplicate files
moonbit duplicates find --min-size 10240   # Only files >= 10KB

# Backups
moonbit backup list             # List available backups
moonbit backup restore <name>   # Restore a backup
```

## Automated Cleaning

MoonBit includes systemd timers for automated maintenance. The installer configures these, but you can also set them up manually after installing via AUR.

### Setup Timers (Post-AUR Installation)

```bash
# Enable and start scan timer (runs daily at 2 AM)
sudo systemctl enable --now moonbit-scan.timer

# Enable and start clean timer (runs weekly on Sunday at 3 AM)
sudo systemctl enable --now moonbit-clean.timer
```

### Timer Options

- **moonbit-scan.timer**: Scans daily at 2 AM
- **moonbit-clean.timer**: Cleans weekly on Sunday at 3 AM

### Check Timer Status

```bash
# View active timers
systemctl list-timers moonbit-*

# Check service logs
journalctl -u moonbit-scan.service
journalctl -u moonbit-clean.service
```

### Customize Timers

Edit timer files to change schedule:

```bash
sudo systemctl edit moonbit-scan.timer
sudo systemctl edit moonbit-clean.timer
```

## Development

```bash
make build    # Build binary
make test     # Run tests
make installer # Build installer
```

## License

MIT
