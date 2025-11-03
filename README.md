<div align="center">
  <img src="logo.png" alt="MoonBit" />
</div>
A modern system cleaner built in Go with a TUI and CLI. Clean temporary files, caches, logs and docker images and much more.

![Eldritch Theme](https://img.shields.io/badge/theme-eldritch-37f499?style=flat-square)
![License](https://img.shields.io/badge/license-MIT-blue?style=flat-square)

## Features
- **distro Support**: Arch, Debian/Ubuntu, Fedora/RHEL, openSUSE
- **package manager**: Pacman, APT, DNF, Zypper, AUR helpers
- *development tools**: Python (pip), Node (npm), Rust (cargo), Go, Java (gradle/maven)
- *system caches**: Font, Mesa shader, WebKit, thumbnails
- *docker cleanup**: Images, containers, volumes
- **media servers**: Plex and Jellyfin transcoding cleanup
- **automated scheduling**: Systemd timers for daily/weekly cleaning

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
## Usage (TUI)
```bash
moonbit
```
## Usage (CLI)

Just run `moonbit` - it'll prompt for your password if needed:

```bash
moonbit              # Interactive TUI
moonbit scan         # Scan system
moonbit clean        # Clean (dry-run by default)
moonbit clean --force # Actually delete files
```

### Additional Commands

```bash
# Package manager cleanup
moonbit pkg orphans              # Remove orphaned packages
moonbit pkg kernels              # Remove old kernels (Debian/Ubuntu)

# Docker cleanup
moonbit docker images            # Remove unused images
moonbit docker all               # Remove all unused resources

# Find duplicates
moonbit duplicates find          # Find duplicate files
moonbit duplicates find --min-size 10240  # Only files >= 10KB

# Backups
moonbit backup list              # List backups
moonbit backup restore <name>    # Restore backup
```

## Automated Cleaning

The installer sets up systemd timers:

- **Daily**: Scan daily at 2 AM, clean weekly on Sunday at 3 AM
- **Weekly**: Scan and clean weekly on Sunday at 3 AM
- **Manual**: No automation

Check timer status:

```bash
systemctl list-timers moonbit-*
```

## Development

```bash
make build    # Build binary
make test     # Run tests
make installer # Build installer
```

## License

MIT
