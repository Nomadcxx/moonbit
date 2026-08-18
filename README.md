<div align="center">
  <img src="logo.png" alt="moonbit" />
  
  **A system cleaner for Linux, with a TUI and a CLI**
  
  ![Eldritch Theme](https://img.shields.io/badge/theme-eldritch-37f499?style=flat-square)
  ![License](https://img.shields.io/badge/license-GPL--3.0-blue?style=flat-square)
</div>

---
![TUI Demo](demos/tui-full.gif)

## Features
- **Distro Support**: Arch, Debian/Ubuntu, Fedora/RHEL, openSUSE
- **Packaging**: AUR, `.deb` and `.rpm` per release, a Nix flake, and static binaries
- **Package Managers**: Pacman, APT, DNF, Zypper, AUR helpers (yay, paru)
- **Safe Cache Cleanup**: Package caches, temp files, thumbnails, font caches, logs, and conservative system caches
- **App Cache Cleanup**: Deep-scan discovery for IDE, Electron, AI-tool, Bottles, and Lutris cache/log/temp paths
- **Docker Cleanup**: Images, containers, volumes, build cache
- **Media Servers**: Plex and Jellyfin transcoding cleanup
- **Duplicate Finder**: Locate duplicate files with configurable minimum sizes
- **Automated Maintenance**: Systemd timer mode and daemon mode
- **Safety Controls**: Dry-runs by default, category filtering, and explicit force mode for deletion
- **Desktop Launcher**: Appears in the app menu, with a graphical password prompt

## Installation

### Arch Linux (AUR)

```bash
yay -S moonbit
# or
paru -S moonbit
```

### Debian / Ubuntu / Fedora

Prebuilt packages are attached to every [release](https://github.com/Nomadcxx/moonbit/releases/latest):

```bash
# Debian 13 (also Ubuntu 25.x)
sudo apt install ./moonbit_<version>_debian13_amd64.deb

# Ubuntu 24.04
sudo apt install ./moonbit_<version>_ubuntu24.04_amd64.deb

# Fedora 42+
sudo dnf install ./moonbit-<version>-1.fedora42.x86_64.rpm
```

These install `/usr/bin/moonbit`.

### Static binary (any distro)

Every release carries static `linux/amd64` and `linux/arm64` binaries and a
`SHA256SUMS` file:

```bash
curl -fsSLO https://github.com/Nomadcxx/moonbit/releases/latest/download/moonbit-linux-amd64
curl -fsSLO https://github.com/Nomadcxx/moonbit/releases/latest/download/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
sudo install -Dm755 moonbit-linux-amd64 /usr/local/bin/moonbit
```

### Quick Install Script

```bash
curl -sSL https://raw.githubusercontent.com/Nomadcxx/moonbit/main/install.sh | sudo bash
```

**Requires:** `go` 1.24+, `git`, `make`. Installs `/usr/local/bin/moonbit`.

### Nix / NixOS

```bash
# Run without installing
nix run github:Nomadcxx/moonbit

# Build from a clone
git clone https://github.com/Nomadcxx/moonbit.git
cd moonbit
nix build
./result/bin/moonbit --help
```

The derivation installs the systemd units under `$out/lib/systemd/system` with
`ExecStart` pointing at the store path.

### Manual Build

**Requirements:** Go 1.24+, `make`

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

Search for **moonbit** in your application menu to launch it without a terminal.
moonbit opens a terminal itself and asks for your password there. It honours
`$TERMINAL` when that names a terminal you actually have, then falls back
through ghostty, kitty, foot, alacritty, konsole, ptyxis, GNOME Console,
gnome-terminal, xfce4-terminal and others.

If it picks the wrong one, name yours explicitly:

```bash
MOONBIT_TERMINAL="myterm --exec-flag" moonbit --launcher
```

Set it in `~/.config/environment.d/moonbit.conf` to make it stick.

The TUI offers two scan modes:
- **Quick Scan** - Fast scan of conservative, commonly safe cleanup categories
- **Deep Scan** - Comprehensive scan including logs, system caches, and deep-only app cache categories

The Schedule screen can enable or disable moonbit's systemd timer and daemon modes, and it warns if both automation modes are active.

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

# Systemd journal
moonbit journal vacuum --size=500M          # Preview
moonbit journal vacuum --time=14d --force   # Apply

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

`moonbit clean` only previews until you pass `--force`. Some categories need sudo, depending on what you select and your sudo policy.

moonbit skips high-risk cleanup classes by default. Browser caches, model caches, and Steam shader and download caches stay out of the app cache set. Application paths cover cache, log, temp, crash-report, and old tool-output locations; session, project, storage, repository, plugin, and prefix data stay untouched.

moonbit never follows symlinks, and it re-checks every path against your config between scan and clean. A stale or hand-edited scan cache cannot widen what gets deleted. Reported "space freed" counts bytes measured on disk at deletion, not sizes recorded during the scan.

Log cleanup targets rotated files only. moonbit will not unlink a log a daemon still holds open: it truncates Docker container logs, and reclaims journal space through `moonbit journal vacuum`, which drives `journalctl --vacuum-*`.

## Automated Cleaning

> **Scope:** automation cleans system-wide paths only. It never touches a
> user's home directory. The units run as root with `HOME=/root` and
> `ProtectHome=read-only`, so home-relative categories (User Cache, Thumbnails,
> Trash, npm, pip, cargo) resolve under `/root`. On a desktop, your own caches
> are the ones filling the disk, and automation will not reclaim them. Run
> `moonbit scan && moonbit clean --force` from your session for those, or write
> a `systemctl --user` unit. See [systemd/README.md](systemd/README.md).

moonbit has two automation modes. Use one at a time:

- **Timer Mode**: lightweight scheduled systemd services
- **Daemon Mode**: a long-running service with configurable scan and clean intervals

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

---

<a href="https://github.com/Nomadcxx"><img src="https://raw.githubusercontent.com/Nomadcxx/Nomadcxx/main/assets/rama-mark.svg" height="22" alt="RAMA"></a> — terminal-native tooling for the linux desktop.
[More projects →](https://github.com/Nomadcxx) · [Sponsor](https://github.com/sponsors/Nomadcxx) ❤️
