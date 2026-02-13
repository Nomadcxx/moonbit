package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/Nomadcxx/moonbit/internal/config"
	"github.com/Nomadcxx/moonbit/internal/scanner"
	"github.com/Nomadcxx/moonbit/internal/utils"
	"github.com/spf13/cobra"
)

var (
	daemonScanInterval   string
	daemonCleanInterval  string
	daemonLogFile        string
	daemonPidFile        string
)

// DaemonState tracks the running daemon state
type DaemonState struct {
	StartTime     time.Time
	LastScanTime  time.Time
	LastCleanTime time.Time
	ScanCount     int
	CleanCount    int
	FilesCleaned  int64
	SpaceFreed   int64
	logger       *audit.Logger
}

var daemonState *DaemonState

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
	Run: func(cmd *cobra.Command, args []string) {
		if !isRunningAsRoot() {
			reexecWithSudo()
			return
		}

		// Parse intervals
		scanInterval, err := time.ParseDuration(daemonScanInterval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid scan interval: %v\n", err)
			os.Exit(1)
		}

		cleanInterval, err := time.ParseDuration(daemonCleanInterval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid clean interval: %v\n", err)
			os.Exit(1)
		}

		// Setup logging
		logger, err := audit.NewLogger()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create audit logger: %v\n", err)
			logger = nil
		}

		daemonState = &DaemonState{
			StartTime: time.Now(),
			logger:    logger,
		}

		// Write PID file
		if daemonPidFile != "" {
			if err := writePidFile(daemonPidFile); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to write PID file: %v\n", err)
				os.Exit(1)
			}
			defer os.Remove(daemonPidFile)
		}

		fmt.Println(S.ASCIIHeader())
		fmt.Println()
		fmt.Println(S.Bold("MoonBit Daemon Started"))
		fmt.Printf("  Scan interval:  %s\n", S.Success(scanInterval.String()))
		fmt.Printf("  Clean interval: %s\n", S.Success(cleanInterval.String()))
		fmt.Printf("  Log file:       %s\n", S.Muted(daemonLogFile))
		fmt.Printf("  PID file:       %s\n", S.Muted(daemonPidFile))
		fmt.Println()
		fmt.Println(S.Muted("Press Ctrl+C to stop"))
		fmt.Println()

		// Log daemon start
		if daemonState.logger != nil {
			daemonState.logger.Log(audit.LogEntry{
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
				fmt.Printf("\n%s Received signal: %v\n", S.Warning("⚠"), sig)
				fmt.Println(S.Bold("Shutting down daemon..."))
				
				if daemonState.logger != nil {
					daemonState.logger.Log(audit.LogEntry{
						Operation: "daemon_stop",
						Args:      []string{sig.String()},
						Result:    fmt.Sprintf("scans=%d cleans=%d", daemonState.ScanCount, daemonState.CleanCount),
					})
				}
				
				fmt.Println(S.Success("✓ Daemon stopped"))
				return
			}
		}
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	Long:  "Display current status of the running moonbit daemon",
	Run: func(cmd *cobra.Command, args []string) {
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
	},
}

func performScan() {
	now := time.Now()
	daemonState.LastScanTime = now
	daemonState.ScanCount++

	fmt.Printf("\n%s [%s] Starting scheduled scan...\n", 
		S.Bold("🔍"), 
		now.Format("2006-01-02 15:04:05"))

	start := time.Now()
	
	// Run scan
	if err := ScanAndSave(); err != nil {
		fmt.Printf("%s Scan failed: %v\n", S.Error("✗"), err)
		
		if daemonState.logger != nil {
			daemonState.logger.Log(audit.LogEntry{
				Timestamp: now,
				Operation: "scheduled_scan",
				Result:    "failed",
				Error:     err,
			})
		}
		return
	}

	duration := time.Since(start)
	fmt.Printf("%s Scan completed in %s\n", S.Success("✓"), duration)

	if daemonState.logger != nil {
		daemonState.logger.Log(audit.LogEntry{
			Timestamp: now,
			Operation: "scheduled_scan",
			Result:    "success",
		})
	}
}

func performClean() {
	now := time.Now()
	daemonState.LastCleanTime = now
	daemonState.CleanCount++

	fmt.Printf("\n%s [%s] Starting scheduled clean...\n",
		S.Bold("🧹"),
		now.Format("2006-01-02 15:04:05"))

	start := time.Now()

	// Run clean
	if err := CleanSession(true); err != nil {
		fmt.Printf("%s Clean failed: %v\n", S.Error("✗"), err)
		
		if daemonState.logger != nil {
			daemonState.logger.Log(audit.LogEntry{
				Timestamp: now,
				Operation: "scheduled_clean",
				Result:    "failed",
				Error:     err,
			})
		}
		return
	}

	duration := time.Since(start)
	fmt.Printf("%s Clean completed in %s\n", S.Success("✓"), duration)

	if daemonState.logger != nil {
		daemonState.logger.Log(audit.LogEntry{
			Timestamp: now,
			Operation: "scheduled_clean",
			Result:    "success",
		})
	}
}

func writePidFile(path string) error {
	pid := os.Getpid()
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStatusCmd)

	daemonCmd.Flags().StringVar(&daemonScanInterval, "scan", "1h", "Scan interval (e.g., 30m, 1h, 2h)")
	daemonCmd.Flags().StringVar(&daemonCleanInterval, "clean", "24h", "Clean interval (e.g., 12h, 24h, 7d)")
	daemonCmd.Flags().StringVar(&daemonLogFile, "log", "/var/log/moonbit/daemon.log", "Log file path")
	daemonCmd.Flags().StringVar(&daemonPidFile, "pid", "/var/run/moonbit.pid", "PID file path")
}
