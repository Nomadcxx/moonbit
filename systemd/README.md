# MoonBit Systemd Service

Automated system cleaning with systemd. Two operational modes available:

1. **Timer Mode** (Periodic, lightweight) - Runs scan and clean on a schedule
2. **Daemon Mode** (Continuous, long-running) - Keeps daemon always running

⚠️ **Important:** Timer and daemon modes are mutually exclusive. Enable only ONE mode at a time.

## Scope: what automation cleans

**Timer and daemon modes clean what their own config lists, with the same
reach as `sudo moonbit`.**

The units run as root with `Environment=HOME=/root` and no `SUDO_USER`, and
read their config from `/var/lib/moonbit/config/moonbit/config.toml`. A config
generated there resolves every home-relative category (User Cache, Thumbnails,
Trash, npm, pip, cargo, gradle, go-build, fontconfig, mesa, and the
per-application caches) under `/root`. A config that names your paths, for
example one written by a `sudo moonbit` run, cleans those paths too: the units
no longer mount `/home` read-only, so a scheduled clean reaches what the TUI
reaches. Only `/usr`, `/boot` and `/etc` stay read-only (`ProtectSystem=full`).

Every path is still re-checked against the root-owned config between scan and
clean, symlinks are never followed, and logs a process holds open are
truncated rather than unlinked. A scheduled run cannot know which caches a live
session is using, so keep home-relative categories in the units' config to the
ones you are happy to clear on a schedule. The desktop panel and the TUI clean
your caches on demand from your own config:

```bash
moonbit scan && moonbit clean --force
```

If you want that on a schedule, use a *user* systemd unit running in your own
session (`systemctl --user`), not the system units here.

## Installation

### Option A: Timer Mode (Default)

For periodic cleaning at scheduled times (minimal resource usage):

```bash
# Copy service and timer files
sudo cp systemd/moonbit-{scan,clean}.{service,timer} /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload

# Enable and start scan timer (daily at 2 AM)
sudo systemctl enable --now moonbit-scan.timer

# Enable and start clean timer (weekly on Sunday at 3 AM)
sudo systemctl enable --now moonbit-clean.timer
```

### Option B: Daemon Mode (Long-running)

For continuous background monitoring (always active):

```bash
# Use the Makefile target
make install-daemon

# OR manually:
sudo cp moonbit /usr/local/bin/
sudo mkdir -p /var/log/moonbit
sudo cp systemd/moonbit-daemon.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now moonbit-daemon.service
```

## Verify Status

### Timer Mode

```bash
# Check timer status
systemctl list-timers moonbit-*

# Check service logs
journalctl -u moonbit-scan.service
journalctl -u moonbit-clean.service

# Manual trigger (for testing)
sudo systemctl start moonbit-scan.service
sudo systemctl start moonbit-clean.service
```

### Daemon Mode

```bash
# Check daemon status
systemctl status moonbit-daemon.service

# View daemon logs
journalctl -u moonbit-daemon.service -f

# Manually stop/start
sudo systemctl stop moonbit-daemon.service
sudo systemctl start moonbit-daemon.service
```

## Schedule Overview

### Timer Mode
- **Scan**: Daily at 2:00 AM (±30min randomized delay)
- **Clean**: Weekly on Sunday at 3:00 AM (±1hr randomized delay)

Randomized delays prevent resource spikes if running on multiple systems.

### Daemon Mode
- **Continuous**: Daemon runs 24/7
- **Scan interval**: Configurable (default: 1 hour)
- **Clean interval**: Configurable (default: 24 hours)

## Switching Between Modes

### From Timer to Daemon

```bash
# Disable timer mode
sudo systemctl disable --now moonbit-scan.timer
sudo systemctl disable --now moonbit-clean.timer

# Enable daemon mode
make install-daemon
```

### From Daemon to Timer

```bash
# Disable daemon mode
make uninstall-daemon

# Enable timer mode (see "Option A: Timer Mode" above)
sudo systemctl enable --now moonbit-scan.timer
sudo systemctl enable --now moonbit-clean.timer
```

## Customization

### Timer Mode

Edit timer files to change schedule:

```bash
sudo systemctl edit moonbit-scan.timer
```

Common schedules:
- `OnCalendar=daily` - Every day at midnight
- `OnCalendar=weekly` - Every Monday at midnight
- `OnCalendar=*-*-* 04:00:00` - Daily at 4 AM
- `OnCalendar=Mon,Wed,Fri 02:00:00` - M/W/F at 2 AM

### Daemon Mode

Daemon runs with default intervals. To customize, edit the systemd service:

```bash
sudo systemctl edit moonbit-daemon
```

And modify the `ExecStart` line:
```
ExecStart=/usr/local/bin/moonbit daemon --scan 1h --clean 24h --log /var/log/moonbit/daemon.log
```

## Disable

### Timer Mode

```bash
# Stop and disable timers
sudo systemctl disable --now moonbit-scan.timer
sudo systemctl disable --now moonbit-clean.timer
```

### Daemon Mode

```bash
# Use the Makefile target
make uninstall-daemon

# OR manually:
sudo systemctl disable --now moonbit-daemon.service
sudo rm /etc/systemd/system/moonbit-daemon.service
sudo systemctl daemon-reload
```

## Security

Services run with identical hardening:
- Private /tmp
- No new privileges
- Protected system directories
- `/usr`, `/boot` and `/etc` read-only; everything moonbit cleans is under
  `/var` or a home directory

Daemon additionally logs to `/var/log/moonbit/daemon.log` with appropriate permissions.
