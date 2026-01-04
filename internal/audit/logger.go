package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	mu       sync.Mutex
	filePath string
	file     *os.File
}

type LogEntry struct {
	Timestamp time.Time
	Operation string
	User      string
	Args      []string
	Result    string
	Error     error
}

func NewLogger() (*Logger, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	logDir := filepath.Join(homeDir, ".local", "share", "moonbit", "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, "audit.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log: %w", err)
	}

	return &Logger{
		filePath: logPath,
		file:     file,
	}, nil
}

func (l *Logger) Log(entry LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	if entry.User == "" {
		entry.User = os.Getenv("USER")
		if entry.User == "" {
			entry.User = os.Getenv("SUDO_USER")
		}
		if entry.User == "" {
			entry.User = "unknown"
		}
	}

	logLine := fmt.Sprintf("[%s] user=%s operation=%s args=%v result=%s",
		entry.Timestamp.Format(time.RFC3339),
		entry.User,
		entry.Operation,
		entry.Args,
		entry.Result,
	)

	if entry.Error != nil {
		logLine += fmt.Sprintf(" error=%v", entry.Error)
	}

	logLine += "\n"

	if _, err := l.file.WriteString(logLine); err != nil {
		return fmt.Errorf("failed to write audit log: %w", err)
	}

	return l.file.Sync()
}

func (l *Logger) LogPackageOperation(operation string, packages []string, result string, err error) error {
	return l.Log(LogEntry{
		Operation: fmt.Sprintf("package_%s", operation),
		Args:      packages,
		Result:    result,
		Error:     err,
	})
}

func (l *Logger) LogSystemdOperation(operation string, unit string, result string, err error) error {
	return l.Log(LogEntry{
		Operation: fmt.Sprintf("systemd_%s", operation),
		Args:      []string{unit},
		Result:    result,
		Error:     err,
	})
}

func (l *Logger) LogDockerOperation(operation string, args []string, result string, err error) error {
	return l.Log(LogEntry{
		Operation: fmt.Sprintf("docker_%s", operation),
		Args:      args,
		Result:    result,
		Error:     err,
	})
}

func (l *Logger) LogCleanOperation(filesDeleted int, bytesFreed uint64, err error) error {
	result := fmt.Sprintf("deleted=%d bytes=%d", filesDeleted, bytesFreed)
	return l.Log(LogEntry{
		Operation: "clean",
		Result:    result,
		Error:     err,
	})
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
