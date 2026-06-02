<div align="center">
  <img src="logo.png" alt="MoonBit" />
  
  **A modern system cleaner built in Go with a TUI and CLI**
  
  ![Eldritch Theme](https://img.shields.io/badge/theme-eldritch-37f499?style=flat-square)
  ![License](https://img.shields.io/badge/license-GPL--3.0-blue?style=flat-square)
</div>

---
![TUI Demo](demos/tui-full.gif)

## Features
- **Distro Support**: Arch, Debian/Ubuntu, Fedora/RHEL, openSUSE
- **Package Managers**: Pacman, APT, DNF, Zypper, AUR helpers (yay, paru)
- **Safe Cache Cleanup**: Package caches, temp files, thumbnails, font caches, logs, and conservative system caches
- **App Cache Cleanup**: Deep-scan discovery for IDE, Electron, AI-tool, Bottles, and Lutris cache/log/temp paths
- **Docker Cleanup**: Images, containers, volumes, build cache
- **Media Servers**: Plex and Jellyfin transcoding cleanup
- **Duplicate Finder**: Locate duplicate files with configurable minimum sizes
- **Automated Maintenance**: Systemd timer mode and daemon mode
- **Safety Controls**: Dry-runs by default, category filtering, and explicit force mode for deletion

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

**Requirements:** Go 1.24+

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
- **Quick Scan** - Fast scan of conservative, commonly safe cleanup categories
- **Deep Scan** - Comprehensive scan including logs, system caches, and deep-only app cache categories

The Schedule screen can enable or disable MoonBit's systemd timer and daemon modes, and it warns if both automation modes are active.

### CLI Commands

```bash
# Scanning
moonbit scan                    # Standard scan
moonbit scan --mode quick       # Quick scan (safe caches only)
moonbit scan --mode deep        # Deep scan (all categories)
moonbit scan --no-prompt        # Scan without interactive clean prompt
moonbit scan --list-categories  # Show available categories
moonbit scan --include-category "opencode Caches"
moonbit scan --exclude-category "System Logs"

# Cleaning
moonbit clean                   # Preview what would be deleted (dry-run)
moonbit clean --force           # Actually delete files
moonbit clean --mode quick      # Clean only quick scan categories
moonbit clean --mode deep       # Clean all scanned categories
moonbit clean --include-category "Lutris Prefix Temp" --force
moonbit clean --exclude-category "System Logs" --force

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

# Daemon
moonbit daemon                  # Run continuous maintenance loop
moonbit daemon --scan 1h --clean 24h
moonbit daemon status           # Show daemon status
```

### Safety Notes

MoonBit intentionally avoids high-risk cleanup classes by default. Browser caches, model caches, Steam shader caches, and Steam download caches are not included in the built-in app cache cleanup set. New application cleanup paths are scoped to cache, log, temp, crash-report, and old tool-output locations; session, project, storage, repository, plugin, and prefix data paths are excluded.

`moonbit clean` is a dry-run unless `--force` is provided. System-wide scans and cleans may require sudo depending on the selected categories and your local sudo policy.

## Automated Cleaning

MoonBit includes two mutually exclusive automation modes:

- **Timer Mode**: lightweight scheduled systemd services
- **Daemon Mode**: a long-running service with configurable scan and clean intervals

Use only one mode at a time.

### Timer Mode

- **moonbit-scan.timer**: runs `moonbit scan --mode quick --no-prompt` daily at 2 AM with a 30 minute randomized delay
- **moonbit-clean.timer**: runs a quick pre-scan and `moonbit clean --force --mode quick` weekly on Sunday at 3 AM with a 1 hour randomized delay

#### Setup Timers

**Option 1: Using the TUI (Recommended)**

```bash
moonbit  # Launch TUI and select "Schedule Scan & Clean"
```

The TUI allows you to enable or disable timers and view their current status.

**Option 2: Using the Installer**

```bash
sudo ./moonbit-installer  # Select timer schedule during installation
```

**Option 3: Manual Setup**

```bash
# Enable and start scan timer
sudo systemctl enable --now moonbit-scan.timer

# Enable and start clean timer
sudo systemctl enable --now moonbit-clean.timer
```

#### Check Timer Status

```bash
# View active timers
systemctl list-timers moonbit-*

# Check service logs
journalctl -u moonbit-scan.service
journalctl -u moonbit-clean.service
```

#### Customize Timer Schedule

```bash
sudo systemctl edit moonbit-scan.timer
sudo systemctl edit moonbit-clean.timer
```

### Daemon Mode

```bash
sudo systemctl disable --now moonbit-scan.timer moonbit-clean.timer
sudo systemctl enable --now moonbit-daemon.service
```

The daemon defaults to scanning every 1 hour and cleaning every 24 hours. You can customize intervals by editing `moonbit-daemon.service` or by running the daemon directly:

```bash
moonbit daemon --scan 30m --clean 12h
moonbit daemon status
journalctl -u moonbit-daemon.service -f
```

## Development

```bash
make build    # Build binary
make test     # Run tests
make installer # Build installer
```

## License

GPL-3.0
