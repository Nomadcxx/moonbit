# Stacer Integration Analysis

Analysis of Stacer features for potential integration into MoonBit.

## Stacer Overview

**Status**: Abandoned project (last release May 2019, v1.1.0)
**Tech Stack**: C++/Qt5 GUI application
**License**: GPL-3.0

## Stacer Features

### 1. System Cleaner Modules

Stacer's System Cleaner has 5 cleanup categories:

| Category | Path | MoonBit Status |
|----------|------|----------------|
| Package Caches | `/var/cache/apt/archives`, etc | ✅ **DONE** - We support APT, DNF, Zypper, Pacman, Yay, Paru, Pamac |
| Crash Reports | `/var/crash`, `~/.local/share/apport` | ✅ **DONE** - Category added in recent update |
| Application Logs | `~/.cache/**/logs`, `~/.local/share/xorg` | ✅ **DONE** - Application Logs category |
| Application Caches | `~/.cache/*` | ✅ **DONE** - Multiple cache categories (font, mesa, webkit, browsers, etc) |
| Trash | `~/.local/share/Trash` | ✅ **DONE** - Trash category (not selected by default) |

### 2. Additional Stacer Features

- **Real-time System Monitoring**: CPU, RAM, disk usage, network activity dashboard
- **Startup Apps Manager**: Control which applications launch at boot
- **Process Manager**: Monitor and terminate running processes
- **Services Manager**: Enable/disable systemd services
- **Uninstaller**: GUI for removing installed packages
- **APT Repository Manager**: Manage software sources

## Analysis & Recommendations

### What MoonBit Already Has

MoonBit **already covers all 5 of Stacer's cleanup modules** and goes beyond:

**MoonBit Advantages:**
- **30+ cleanup categories** vs Stacer's 5
- **Cross-distro support**: APT, DNF, Zypper, Pacman (Stacer is Ubuntu-focused)
- **Development tool caches**: pip, npm, cargo, gradle, maven, Go
- **Media server support**: Plex, Jellyfin transcoding cleanup
- **Docker integration**: Native docker CLI commands
- **Age-based filtering**: Smart thumbnail cleanup (>30 days old)
- **Backup/restore**: File-based backups with SHA256 naming
- **Systemd automation**: Built-in timer support for scheduled cleaning
- **TUI + CLI**: Both interactive and scriptable modes
- **Active development**: Regular updates vs abandoned Stacer

### What Doesn't Make Sense for MoonBit

The following Stacer features are **out of scope** for MoonBit (focused cleaner tool):

❌ **Real-time Monitoring**: Use htop, btop, glances instead
❌ **Startup Apps Manager**: Desktop environment specific
❌ **Process Manager**: Use htop, kill, systemctl
❌ **Services Manager**: Use systemctl directly
❌ **Uninstaller**: Use native package managers (apt, dnf, pacman)
❌ **Repository Manager**: Use apt-add-repository, dnf config-manager

These features would bloat MoonBit and conflict with Unix philosophy (do one thing well).

### Potential Future Enhancements

Instead of copying Stacer, focus on MoonBit's strengths:

1. **Better TUI Visualization**
   - Category tree view (inspired by Stacer's clean GUI)
   - Before/after disk usage graphs
   - File preview for selected categories

2. **Duplicate File Detection**
   - Find and remove duplicate files (inspired by rmlint)
   - Hash-based comparison
   - Interactive selection

3. **Old Package Cleanup**
   - Remove old kernel versions (Ubuntu/Debian)
   - Clean orphaned packages (pacman -Qtdq)
   - Unused dependencies detection

4. **Browser-Specific Cleanup**
   - Firefox: cache, cookies, history (selective)
   - Chrome/Chromium: same
   - Brave, Vivaldi, Edge support

5. **IDE/Editor Cleanup**
   - VSCode extensions cache
   - IntelliJ IDEA caches
   - Vim/Neovim swap files

## Conclusion

**MoonBit already surpasses Stacer for system cleaning.**

Stacer's value was its GUI and all-in-one dashboard for beginners. MoonBit provides a superior cleaning experience with:
- More cleanup targets
- Better cross-distro support
- Modern features (age filtering, Docker integration)
- Active development
- Unix philosophy compliance

**Recommendation**: Don't integrate Stacer. Instead:
1. Continue expanding cleanup targets (see "Future Enhancements" above)
2. Improve TUI visualization
3. Add duplicate file detection
4. Focus on what MoonBit does best: comprehensive, safe, automated system cleaning

The only thing worth learning from Stacer is UI/UX patterns for making system maintenance accessible to beginners.
