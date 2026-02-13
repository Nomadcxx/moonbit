package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/Nomadcxx/moonbit/internal/utils"
	"github.com/spf13/cobra"
)

func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}

	if strings.HasSuffix(s, "d") {
		prefix := strings.TrimSuffix(s, "d")
		days, err := strconv.ParseFloat(prefix, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}

	return time.ParseDuration(s)
}

var (
	daemonScanInterval  string
	daemonCleanInterval string
	daemonLogFile       string
	daemonPidFile       string
)

// DaemonState tracks the running daemon state
type DaemonState struct {
	mu            sync.Mutex
	StartTime     time.Time
	LastScanTime  time.Time
	LastCleanTime time.Time
	ScanCount     int
	CleanCount    int
	FilesCleaned  int64
	SpaceFreed    int64
	logger        *audit.Logger
}

type daemonStats struct {
	StartTime     time.Time
	LastScanTime  time.Time
	LastCleanTime time.Time
	ScanCount     int
	CleanCount    int
	FilesCleaned  int64
	SpaceFreed    int64
}

func (ds *DaemonState) setLastScanTime(t time.Time) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.LastScanTime = t
}

func (ds *DaemonState) incrementScanCount() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.ScanCount++
}

func (ds *DaemonState) setLastCleanTime(t time.Time) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.LastCleanTime = t
}

func (ds *DaemonState) incrementCleanCount() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.CleanCount++
}

func (ds *DaemonState) addFilesCleaned(n int64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.FilesCleaned += n
}

func (ds *DaemonState) addSpaceFreed(n int64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.SpaceFreed += n
}

func (ds *DaemonState) auditLogger() *audit.Logger {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.logger
}

func (ds *DaemonState) stats() daemonStats {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return daemonStats{
		StartTime:     ds.StartTime,
		LastScanTime:  ds.LastScanTime,
		LastCleanTime: ds.LastCleanTime,
		ScanCount:     ds.ScanCount,
		CleanCount:    ds.CleanCount,
		FilesCleaned:  ds.FilesCleaned,
		SpaceFreed:    ds.SpaceFreed,
	}
}

var daemonState *DaemonState

var opSem = make(chan struct{}, 1)

var daemonOut io.Writer = os.Stdout
var daemonErr io.Writer = os.Stderr

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run moonbit as a background daemon",
	Long: `Run moonbit as a continuous background daemon that periodically scans and cleans the system.

The daemon stays running and performs automatic maintenance at configured intervals.

Examples:
  moonbit daemon                    # Start daemon with default intervals (scan: 1h, clean: 24h)
  moonbit daemon --scan 30m         # Scan every 30 minutes
  moonbit daemon --clean 12h        # Clean every 12 hours
  moonbit daemon --scan 1h --clean 7d  # Custom intervals`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isRunningAsRoot() {
			reexecWithSudo()
			return nil
		}

		daemonOut = os.Stdout
		daemonErr = os.Stderr
		var logFile *os.File
		if daemonLogFile != "" {
			if err := os.MkdirAll(filepath.Dir(daemonLogFile), 0755); err != nil {
				return fmt.Errorf("failed to create log directory %s: %w", filepath.Dir(daemonLogFile), err)
			}
			f, err := os.OpenFile(daemonLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				return fmt.Errorf("failed to open log file %s: %w", daemonLogFile, err)
			}
			logFile = f
			defer logFile.Close()
			daemonOut = io.MultiWriter(os.Stdout, logFile)
			daemonErr = io.MultiWriter(os.Stderr, logFile)
		}

		// Parse intervals
		scanInterval, err := parseDuration(daemonScanInterval)
		if err != nil {
			return fmt.Errorf("invalid scan interval: %w", err)
		}

		cleanInterval, err := parseDuration(daemonCleanInterval)
		if err != nil {
			return fmt.Errorf("invalid clean interval: %w", err)
		}

		if err := checkTimerConflicts(); err != nil {
			return err
		}

		// Setup logging
		logger, err := audit.NewLogger()
		if err != nil {
			fmt.Fprintf(daemonErr, "Warning: Failed to create audit logger: %v\n", err)
			logger = nil
		}

		daemonState = &DaemonState{
			StartTime: time.Now(),
			logger:    logger,
		}

		// Write PID file
		if daemonPidFile != "" {
			if err := cleanStalePidFile(daemonPidFile); err != nil {
				return err
			}
			if err := writePidFile(daemonPidFile); err != nil {
				return fmt.Errorf("failed to write PID file: %w", err)
			}
			defer os.Remove(daemonPidFile)
		}

		fmt.Fprintln(daemonOut, S.ASCIIHeader())
		fmt.Fprintln(daemonOut)
		fmt.Fprintln(daemonOut, S.Bold("MoonBit Daemon Started"))
		fmt.Fprintf(daemonOut, "  Scan interval:  %s\n", S.Success(scanInterval.String()))
		fmt.Fprintf(daemonOut, "  Clean interval: %s\n", S.Success(cleanInterval.String()))
		fmt.Fprintf(daemonOut, "  Log file:       %s\n", S.Muted(daemonLogFile))
		fmt.Fprintf(daemonOut, "  PID file:       %s\n", S.Muted(daemonPidFile))
		fmt.Fprintln(daemonOut)
		fmt.Fprintln(daemonOut, S.Muted("Press Ctrl+C to stop"))
		fmt.Fprintln(daemonOut)

		// Log daemon start
		if logger := daemonState.auditLogger(); logger != nil {
			logger.Log(audit.LogEntry{
				Operation: "daemon_start",
				Args:      []string{scanInterval.String(), cleanInterval.String()},
				Result:    "success",
			})
		}

		// Setup signal handling
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Create tickers
		scanTicker := time.NewTicker(scanInterval)
		cleanTicker := time.NewTicker(cleanInterval)
		defer scanTicker.Stop()
		defer cleanTicker.Stop()

		// Do initial scan immediately
		go performScan()

		// Main daemon loop
		for {
			select {
			case <-scanTicker.C:
				go performScan()

			case <-cleanTicker.C:
				go performClean()

			case sig := <-sigChan:
				fmt.Fprintf(daemonOut, "\n%s Received signal: %v\n", S.Warning("⚠"), sig)
				fmt.Fprintln(daemonOut, S.Bold("Shutting down daemon..."))

				stats := daemonState.stats()
				uptime := time.Since(stats.StartTime).Round(time.Second)
				fmt.Fprintf(daemonOut,
					"%s Daemon statistics — uptime: %s, scans: %d, cleans: %d, files cleaned: %d, space freed: %s\n",
					S.Bold("📊"),
					uptime,
					stats.ScanCount,
					stats.CleanCount,
					stats.FilesCleaned,
					utils.HumanizeBytes(uint64(stats.SpaceFreed)),
				)

				if logger := daemonState.auditLogger(); logger != nil {
					logger.Log(audit.LogEntry{
						Operation: "daemon_stop",
						Args:      []string{sig.String()},
						Result:    fmt.Sprintf("scans=%d cleans=%d", stats.ScanCount, stats.CleanCount),
					})
					logger.Close()
				}

				fmt.Fprintln(daemonOut, S.Success("✓ Daemon stopped"))
				return nil
			}
		}
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Long:  "Display current status of the running moonbit daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidFile := "/var/run/moonbit.pid"

		// Check if PID file exists
		data, err := os.ReadFile(pidFile)
		if err != nil {
			fmt.Println(S.Error("✗ Daemon is not running"))
			fmt.Println(S.Muted("  No PID file found at " + pidFile))
			os.Exit(1)
		}

		var pid int
		fmt.Sscanf(string(data), "%d", &pid)

		// Check if process exists
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
			fmt.Println(S.Error("✗ Daemon is not running (stale PID file)"))
			os.Remove(pidFile)
			os.Exit(1)
		}

		fmt.Println(S.Success("✓ Daemon is running"))
		fmt.Printf("  PID: %d\n", pid)
		fmt.Printf("  Uptime: %s\n", S.Muted("see logs for details"))
		fmt.Println()
		fmt.Println(S.Muted("View logs: journalctl -u moonbit-daemon -f"))
		return nil
	},
}

func performScan() {
	select {
	case opSem <- struct{}{}:
		defer func() { <-opSem }()
	default:
		fmt.Fprintf(daemonOut, "%s Skipping scan — another operation in progress\n", S.Warning("⚠"))
		return
	}

	now := time.Now()
	daemonState.setLastScanTime(now)
	daemonState.incrementScanCount()

	fmt.Fprintf(daemonOut, "\n%s [%s] Starting scheduled scan...\n",
		S.Bold("🔍"),
		now.Format("2006-01-02 15:04:05"))

	start := time.Now()

	// Run scan
	if err := ScanAndSave(); err != nil {
		fmt.Fprintf(daemonOut, "%s Scan failed: %v\n", S.Error("✗"), err)

		if logger := daemonState.auditLogger(); logger != nil {
			logger.Log(audit.LogEntry{
				Timestamp: now,
				Operation: "scheduled_scan",
				Result:    "failed",
				Error:     err,
			})
		}
		return
	}

	duration := time.Since(start)
	fmt.Fprintf(daemonOut, "%s Scan completed in %s\n", S.Success("✓"), duration)

	if logger := daemonState.auditLogger(); logger != nil {
		logger.Log(audit.LogEntry{
			Timestamp: now,
			Operation: "scheduled_scan",
			Result:    "success",
		})
	}
}

func performClean() {
	select {
	case opSem <- struct{}{}:
		defer func() { <-opSem }()
	default:
		fmt.Fprintf(daemonOut, "%s Skipping clean — another operation in progress\n", S.Warning("⚠"))
		return
	}

	now := time.Now()
	daemonState.setLastCleanTime(now)
	daemonState.incrementCleanCount()

	fmt.Fprintf(daemonOut, "\n%s [%s] Starting scheduled clean...\n",
		S.Bold("🧹"),
		now.Format("2006-01-02 15:04:05"))

	start := time.Now()

	// Run clean
	if err := CleanSession(true); err != nil {
		fmt.Fprintf(daemonOut, "%s Clean failed: %v\n", S.Error("✗"), err)

		if logger := daemonState.auditLogger(); logger != nil {
			logger.Log(audit.LogEntry{
				Timestamp: now,
				Operation: "scheduled_clean",
				Result:    "failed",
				Error:     err,
			})
		}
		return
	}

	duration := time.Since(start)
	fmt.Fprintf(daemonOut, "%s Clean completed in %s\n", S.Success("✓"), duration)

	if logger := daemonState.auditLogger(); logger != nil {
		logger.Log(audit.LogEntry{
			Timestamp: now,
			Operation: "scheduled_clean",
			Result:    "success",
		})
	}
}

func writePidFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	pid := os.Getpid()
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

func cleanStalePidFile(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file %s: %w", path, err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		_ = os.Remove(path)
		return nil
	}

	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		return fmt.Errorf("daemon already running with PID %d", pid)
	} else if os.IsNotExist(err) {
		_ = os.Remove(path)
		return nil
	} else {
		return fmt.Errorf("failed to stat /proc/%d: %w", pid, err)
	}
}

func checkTimerConflicts() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}

	activeTimers := []string{}
	for _, timer := range []string{"moonbit-scan.timer", "moonbit-clean.timer"} {
		cmd := exec.Command("systemctl", "is-active", "--quiet", timer)
		if err := cmd.Run(); err == nil {
			activeTimers = append(activeTimers, timer)
		}
	}

	if len(activeTimers) == 0 {
		return nil
	}

	return fmt.Errorf(
		"moonbit timers are active (%s) — disable them first with 'systemctl disable --now moonbit-scan.timer moonbit-clean.timer'",
		strings.Join(activeTimers, ", "),
	)
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStatusCmd)

	daemonCmd.Flags().StringVar(&daemonScanInterval, "scan", "1h", "Scan interval (e.g., 30m, 1h, 2h)")
	daemonCmd.Flags().StringVar(&daemonCleanInterval, "clean", "24h", "Clean interval (e.g., 12h, 24h, 7d)")
	daemonCmd.Flags().StringVar(&daemonLogFile, "log", "/var/log/moonbit/daemon.log", "Log file path")
	daemonCmd.Flags().StringVar(&daemonPidFile, "pid", "/var/run/moonbit.pid", "PID file path")
}
