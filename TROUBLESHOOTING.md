# MoonBit Troubleshooting Guide

**Version**: 1.2.2  
**Last Updated**: 2025-01-05

---

## Common Issues

### Scan Finds No Files

**Symptoms**: Scan completes but shows 0 files found.

**Possible Causes**:
1. **Paths don't exist**: Category paths may not exist on your distribution
2. **Permission denied**: System paths require sudo
3. **Filters too restrictive**: Category filters may not match any files
4. **Age filtering**: Files may be too new (MinAgeDays setting)

**Solutions**:
```bash
# Check if paths exist
ls -la /var/cache/pacman/pkg  # Arch
ls -la /var/cache/apt/archives  # Debian/Ubuntu

# Run with sudo for system paths
sudo moonbit scan

# Check category configuration
cat ~/.config/moonbit/config.toml

# Verify filters match your files
# Example: If filter is "\.log$" but files are .txt, nothing will match
```

---

### Permission Denied Errors

**Symptoms**: "Permission denied" errors during scan or clean.

**Causes**:
- System-wide paths require root access
- User doesn't have read/write permissions

**Solutions**:
```bash
# Use sudo for system paths
sudo moonbit scan
sudo moonbit clean --force

# Check file permissions
ls -la /var/cache/pacman/pkg

# Verify user has access
sudo chown -R $USER:$USER /path/to/directory  # If appropriate
```

**Note**: MoonBit automatically detects when sudo is needed and will prompt or re-exec with sudo.

---

### Clean Command Does Nothing

**Symptoms**: `moonbit clean` runs but no files are deleted.

**Causes**:
1. **Dry-run mode**: Default behavior (safety feature)
2. **No scan results**: Need to run scan first
3. **No files selected**: Categories may be filtered out

**Solutions**:
```bash
# Check if dry-run is enabled (default)
moonbit clean  # Shows what would be deleted

# Actually delete files
moonbit clean --force

# Verify scan results exist
ls -la ~/.cache/moonbit/scan_results.json

# Run scan first if needed
moonbit scan
```

---

### Files Not Being Cleaned

**Symptoms**: Files appear in scan but aren't deleted during clean.

**Possible Causes**:
1. **Protected paths**: Files in `/bin`, `/etc`, etc. are protected
2. **Size limit exceeded**: Category exceeds MaxDeletionSize (500GB default)
3. **High-risk category**: Requires explicit confirmation
4. **Safe mode**: High-risk categories skipped in safe mode
5. **Age filtering**: Files too new (MinAgeDays)

**Solutions**:
```bash
# Check if path is protected
# Protected: /bin, /usr/bin, /etc, /boot, /sys, /proc

# Check category risk level
cat ~/.config/moonbit/config.toml | grep -A 5 "category_name"

# Check size limits
# Default: 500GB per operation

# Disable safe mode (if appropriate)
# Edit config: safe_mode = false
```

---

### Duplicate Clean Command Issues

**Symptoms**: `moonbit duplicates clean` doesn't work as expected.

**Possible Causes**:
1. **No duplicates found**: Scan may not have found duplicates
2. **Path validation failures**: Invalid paths are skipped
3. **Permission errors**: Can't delete files

**Solutions**:
```bash
# First find duplicates
moonbit duplicates find

# Check for validation errors in output
# Invalid paths are automatically skipped

# Use dry-run first
moonbit duplicates clean --dry-run

# Check file permissions
ls -la /path/to/duplicate/file
```

---

### Docker Cleanup Fails

**Symptoms**: Docker cleanup commands fail or show errors.

**Possible Causes**:
1. **Docker not installed**: Docker CLI not available
2. **Docker not running**: Docker daemon not started
3. **Permission issues**: User not in docker group

**Solutions**:
```bash
# Check Docker installation
docker version

# Check Docker daemon
sudo systemctl status docker

# Start Docker if needed
sudo systemctl start docker

# Add user to docker group (if needed)
sudo usermod -aG docker $USER
# Log out and back in for group change to take effect

# Verify Docker access
docker ps
```

---

### Session Cache Issues

**Symptoms**: Scan results not persisting, cache errors.

**Possible Causes**:
1. **Cache directory permissions**: Can't write to `~/.cache/moonbit/`
2. **Corrupted cache file**: JSON parsing errors
3. **Cache file locked**: Another process using cache

**Solutions**:
```bash
# Check cache directory
ls -la ~/.cache/moonbit/

# Check permissions
chmod 755 ~/.cache/moonbit/

# Remove corrupted cache
rm ~/.cache/moonbit/scan_results.json

# Re-run scan to regenerate cache
moonbit scan
```

---

### Configuration Issues

**Symptoms**: Config file errors, categories not appearing.

**Possible Causes**:
1. **Invalid TOML syntax**: Syntax errors in config file
2. **Missing fields**: Required fields not present
3. **Invalid values**: Risk levels, paths, etc.

**Solutions**:
```bash
# Validate TOML syntax
# Use a TOML validator or check manually

# Regenerate default config
rm ~/.config/moonbit/config.toml
moonbit scan  # Will create new default config

# Check config structure
cat ~/.config/moonbit/config.toml

# Verify category definitions
# Each category needs: name, paths, filters, risk
```

---

### TUI Issues

**Symptoms**: TUI doesn't start, crashes, or behaves unexpectedly.

**Possible Causes**:
1. **Terminal too small**: Minimum size requirements
2. **Terminal compatibility**: Terminal doesn't support required features
3. **Color support**: Terminal doesn't support colors

**Solutions**:
```bash
# Check terminal size
# Minimum: 80x24 characters

# Try different terminal
# Recommended: xterm, gnome-terminal, alacritty, kitty

# Check color support
echo $TERM

# Use CLI mode instead
moonbit scan  # CLI mode
```

---

### Backup/Restore Issues

**Symptoms**: Backups not created or restore fails.

**Possible Causes**:
1. **Backup disabled**: Backup not enabled in config
2. **Disk space**: Insufficient space for backup
3. **Permission errors**: Can't write to backup directory
4. **Corrupted backup**: Backup files missing or corrupted

**Solutions**:
```bash
# Check backup directory
ls -la ~/.local/share/moonbit/backups/

# Check disk space
df -h ~/.local/share/moonbit/backups/

# Verify backup exists
moonbit backup list

# Check backup metadata
cat ~/.local/share/moonbit/backups/CategoryName_*.backup.json

# Verify backup files exist
ls -la ~/.local/share/moonbit/backups/CategoryName_*.backup.files/
```

---

### Performance Issues

**Symptoms**: Scans are slow, high memory usage.

**Possible Causes**:
1. **Large directories**: Scanning very large directory trees
2. **Deep nesting**: Deeply nested directories
3. **Many files**: Thousands of files in single directory

**Solutions**:
```bash
# Use quick scan mode (faster, fewer categories)
moonbit scan --mode quick

# Limit scan depth in config
# Edit: scan.max_depth = 2  # Reduce from default 3

# Add ignore patterns for large directories
# Edit: scan.ignore_patterns = ["node_modules", ".git", "large_dir"]

# Use CLI mode for better performance visibility
moonbit scan  # Shows progress
```

---

### Package Manager Issues

**Symptoms**: Package manager cleanup commands fail.

**Possible Causes**:
1. **Not running as root**: Package manager commands need sudo
2. **Package manager not installed**: APT on Arch, etc.
3. **Invalid package names**: Package validation failures

**Solutions**:
```bash
# Use sudo for package operations
sudo moonbit pkg orphans --force

# Verify package manager exists
which pacman  # Arch
which apt     # Debian/Ubuntu
which dnf     # Fedora

# Check package manager status
sudo pacman -Sy  # Arch
sudo apt update  # Debian/Ubuntu
```

---

## Error Messages

### "Path contains unsafe traversal"
**Meaning**: File path contains `..` which could be used for path traversal attacks.

**Solution**: Use absolute paths or validated relative paths.

### "Cannot operate on protected system path"
**Meaning**: Attempting to delete files in protected system directories.

**Solution**: Protected paths cannot be deleted. Use system package managers instead.

### "Category size exceeds maximum allowed"
**Meaning**: Category exceeds MaxDeletionSize limit (default 500GB).

**Solution**: Clean categories separately or increase limit in config.

### "High-risk category requires confirmation"
**Meaning**: Category marked as High risk requires explicit user confirmation.

**Solution**: Confirm deletion or disable safe mode (not recommended).

### "Failed to create session manager"
**Meaning**: Cannot access or create cache directory.

**Solution**: Check `~/.cache/moonbit/` permissions and disk space.

### "Docker is not installed or not running"
**Meaning**: Docker CLI not available or daemon not running.

**Solution**: Install Docker and start the daemon.

---

## Debugging

### Enable Verbose Logging

```bash
# Check audit logs
tail -f ~/.local/share/moonbit/logs/audit.log

# Check systemd service logs
journalctl -u moonbit-scan.service -f
journalctl -u moonbit-clean.service -f
```

### Verify Configuration

```bash
# View current config
cat ~/.config/moonbit/config.toml

# Test config loading
moonbit scan --mode quick  # Will validate config
```

### Check File Permissions

```bash
# Check cache directory
ls -la ~/.cache/moonbit/

# Check config directory
ls -la ~/.config/moonbit/

# Check backup directory
ls -la ~/.local/share/moonbit/backups/
```

### Test Individual Components

```bash
# Test scan only
moonbit scan

# Test clean (dry-run)
moonbit clean

# Test duplicate find
moonbit duplicates find

# Test Docker
moonbit docker images
```

---

## Getting Help

### Check Logs
1. Audit log: `~/.local/share/moonbit/logs/audit.log`
2. Systemd logs: `journalctl -u moonbit-*.service`
3. Application output: Check terminal output for errors

### Verify Installation
```bash
# Check binary
which moonbit
moonbit --version

# Check systemd timers
systemctl list-timers moonbit-*

# Check dependencies
go version  # Should be 1.21+
```

### Report Issues
Include:
- MoonBit version
- Distribution and version
- Error messages
- Relevant log excerpts
- Steps to reproduce

---

## Best Practices

### Safety
1. **Always use dry-run first**: `moonbit clean` (default)
2. **Review scan results**: Check what will be deleted
3. **Use quick scan**: Faster, safer categories only
4. **Create backups**: Enable backup before cleaning important data
5. **Test on non-critical systems first**

### Performance
1. **Use quick scan for regular maintenance**: `moonbit scan --mode quick`
2. **Limit scan depth**: Reduce `max_depth` in config for faster scans
3. **Add ignore patterns**: Skip large directories like `node_modules`
4. **Run scans during off-hours**: Use systemd timers

### Maintenance
1. **Regular scans**: Set up systemd timers for automated scans
2. **Review backups**: Periodically clean up old backups
3. **Update config**: Add new categories as needed
4. **Monitor disk space**: Check backup directory size

---

*For architecture details, see ARCHITECTURE.md*  
*For development, see CLAUDE.md*
