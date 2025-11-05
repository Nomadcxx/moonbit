# MoonBit Systemd Service

Automated system cleaning with systemd timers.

## Installation

```bash
# Copy service files
sudo cp systemd/moonbit-*.{service,timer} /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload

# Enable and start scan timer (daily at 2 AM)
sudo systemctl enable --now moonbit-scan.timer

# Enable and start clean timer (weekly on Sunday at 3 AM)
sudo systemctl enable --now moonbit-clean.timer
```

## Verify Status

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

## Schedule Overview

- **Scan**: Daily at 2:00 AM (±30min randomized delay)
- **Clean**: Weekly on Sunday at 3:00 AM (±1hr randomized delay)

Randomized delays prevent resource spikes if running on multiple systems.

## Customization

Edit timer files to change schedule:

```bash
sudo systemctl edit moonbit-scan.timer
```

Common schedules:
- `OnCalendar=daily` - Every day at midnight
- `OnCalendar=weekly` - Every Monday at midnight
- `OnCalendar=*-*-* 04:00:00` - Daily at 4 AM
- `OnCalendar=Mon,Wed,Fri 02:00:00` - M/W/F at 2 AM

## Disable

```bash
# Stop and disable timers
sudo systemctl disable --now moonbit-scan.timer
sudo systemctl disable --now moonbit-clean.timer
```

## Security

Services run with:
- Private /tmp
- No new privileges
- Protected system directories
- Read-only home directory
- Limited write access to cache/log directories
