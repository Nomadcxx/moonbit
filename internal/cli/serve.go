package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/Nomadcxx/moonbit/internal/session"
	"github.com/Nomadcxx/moonbit/internal/validation"
)

// Panel control socket. A desktop panel has no TTY, so it cannot use the
// terminal+sudo launcher, and it cannot signal a root process - the daemon
// serves one NDJSON command per connection and reuses the exact --json event
// stream. Closing the connection cancels the running operation (EOF on the
// connection reader cancels the context, same contract as --json on stdin).
//
// Access model: the socket defaults to 0666, i.e. any local user may trigger
// scan/clean. That is deliberate - those commands only ever act on the
// daemon's own root-owned config and session cache under /var (the unit sets
// ProtectHome + XDG_CACHE_HOME=/var/cache), so every local user already has
// this power through the enabled system timers. Paths are revalidated against
// root-owned config before deletion (validation.RevalidateCache); nothing
// user-writable feeds the delete list. Use --socket-mode to tighten.

type panelRequest struct {
	Cmd        string   `json:"cmd"`
	Mode       string   `json:"mode,omitempty"`
	Force      bool     `json:"force,omitempty"`
	Categories []string `json:"categories,omitempty"`
	// ScannedAt binds a clean to the scan the panel reviewed (the scan's done
	// event carries it). Scheduled scans replace the cache, so without it the
	// clean would act on files the user never saw.
	ScannedAt time.Time `json:"scanned_at,omitempty"`
}

// withPanelCategories applies a request's category selection to the same
// package globals the CLI flags feed, for the duration of fn. runPanelOp
// serializes operations via opSem, so the temporary mutation is safe.
func withPanelCategories(cats []string, fn func() error) error {
	if len(cats) == 0 {
		return fn()
	}
	inc, exc := includeCategories, excludeCategories
	includeCategories = cats
	excludeCategories = nil
	defer func() { includeCategories, excludeCategories = inc, exc }()
	return fn()
}

type panelServer struct{ ln net.Listener }

func listenPanelSocket(path string, mode os.FileMode) (*panelServer, error) {
	// A stale socket from a dead daemon would make Listen fail EADDRINUSE.
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSocket != 0 {
		os.Remove(path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if mode != 0 {
		if err := os.Chmod(path, mode); err != nil {
			_ = ln.Close()
			return nil, err
		}
	}
	return &panelServer{ln: ln}, nil
}

func (s *panelServer) Close() error { return s.ln.Close() }

// Serve accepts connections until the listener is closed.
func (s *panelServer) Serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed (daemon shutdown)
		}
		go handlePanelConn(conn)
	}
}

// handlePanelConn serves exactly one request, then closes.
func handlePanelConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	var req panelRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &req); err != nil {
		writePanelEvent(conn, "error", map[string]any{"msg": "invalid request: " + err.Error()})
		return
	}
	switch req.Cmd {
	case "ping":
		writePanelEvent(conn, "pong", nil)
	case "status":
		writePanelEvent(conn, "status", panelStatus())
	case "scan":
		runPanelOp(conn, br, "panel_scan", []string{req.Mode}, func() error {
			if req.Mode != "" {
				if err := validation.ValidateMode(req.Mode); err != nil {
					return err
				}
			}
			if daemonState != nil {
				daemonState.setLastScanTime(time.Now())
				daemonState.incrementScanCount()
			}
			// The mode is this request's alone: scheduled scans and cleans
			// keep the daemon's own.
			return withPanelCategories(req.Categories, func() error {
				return ScanAndSaveWithMode(req.Mode)
			})
		})
	case "clean":
		runPanelOp(conn, br, "panel_clean", []string{fmt.Sprintf("force=%t", req.Force)}, func() error {
			if daemonState != nil {
				daemonState.setLastCleanTime(time.Now())
				daemonState.incrementCleanCount()
			}
			return withPanelCategories(req.Categories, func() error {
				cleanScannedAt = req.ScannedAt
				defer func() { cleanScannedAt = time.Time{} }()
				return CleanSession(!req.Force)
			})
		})
	default:
		writePanelEvent(conn, "error", map[string]any{"msg": "unknown cmd: " + req.Cmd})
	}
}

// runPanelOp serializes with the timer loop via opSem, streams the same
// NDJSON events as --json to the connection, and always ends with exactly one
// terminal event (the operation functions emit done/clean_done themselves).
func runPanelOp(conn net.Conn, cancel io.Reader, op string, args []string, fn func() error) {
	select {
	case opSem <- struct{}{}:
		defer func() { <-opSem }()
	default:
		writePanelEvent(conn, "error", map[string]any{"msg": "another operation in progress"})
		return
	}
	initJSONTo(conn, cancel)
	defer resetJSON()
	err := fn()
	panelAudit(op, args, err)
	if err != nil {
		emitTerminal(err)
	}
	// On success the operation already emitted its terminal event.
}

func panelAudit(op string, args []string, err error) {
	if daemonState == nil {
		return
	}
	logger := daemonState.auditLogger()
	if logger == nil {
		return
	}
	entry := audit.LogEntry{Timestamp: time.Now(), Operation: op, Args: args, Result: "success"}
	if err != nil {
		entry.Result = "failed"
		entry.Error = err
	}
	logger.Log(entry)
}

func panelStatus() map[string]any {
	st := map[string]any{"daemon": daemonState != nil}
	if daemonState != nil {
		s := daemonState.stats()
		st["last_scan"] = s.LastScanTime
		st["last_clean"] = s.LastCleanTime
		st["scan_count"] = s.ScanCount
		st["clean_count"] = s.CleanCount
		st["files_cleaned"] = s.FilesCleaned
		st["space_freed"] = s.SpaceFreed
	}
	if mgr, err := session.NewManager(); err == nil && mgr.Exists() {
		if cache, err := mgr.Load(); err == nil {
			c := map[string]any{
				"files":      cache.TotalFiles,
				"bytes":      cache.TotalSize,
				"scanned_at": cache.ScannedAt,
			}
			if cache.ScanResults != nil {
				c["categories"] = categoryRollup(cache.ScanResults.Files)
			}
			st["cache"] = c
		}
	}
	return st
}

func writePanelEvent(w io.Writer, event string, fields map[string]any) {
	newEmitterTo(w).emit(event, fields)
}
