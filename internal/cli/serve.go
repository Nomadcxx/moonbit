package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Nomadcxx/moonbit/internal/audit"
	"github.com/Nomadcxx/moonbit/internal/docker"
	"github.com/Nomadcxx/moonbit/internal/session"
	"github.com/Nomadcxx/moonbit/internal/units"
	"github.com/Nomadcxx/moonbit/internal/validation"
	"github.com/spf13/cobra"
)

// Desktop panel protocol. A panel has no TTY, so it drives moonbit with one
// NDJSON request and reads back the same event stream --json produces.
// Closing the request side cancels a running operation (EOF on the request
// reader cancels the context, the same contract as --json on stdin).
//
// Two transports, two powers:
//
//   - `moonbit panel` reads one request from stdin. The panel starts it
//     through `sudo -S`, asking the user's password every time, so it acts
//     exactly as the TUI does under sudo: the invoking user's config and scan
//     cache, every category, Docker, and the schedule units.
//   - The daemon's --socket is open to every local user (0666 by default), so
//     it answers only ping and status. Root reach without a password must not
//     be one connect() away for every account on the machine.

type panelRequest struct {
	Cmd        string   `json:"cmd"`
	Mode       string   `json:"mode,omitempty"`
	Force      bool     `json:"force,omitempty"`
	Categories []string `json:"categories,omitempty"`
	// ScannedAt binds a clean to the scan the panel reviewed (the scan's done
	// event carries it). Scheduled scans replace the cache, so without it the
	// clean would act on files the user never saw.
	ScannedAt time.Time `json:"scanned_at,omitempty"`
	// Op names a Docker cleanup: images or all.
	Op string `json:"op,omitempty"`
	// Target (daemon or timers) and Action (enable or disable) drive the
	// schedule, as the TUI's Schedule screen does.
	Target string `json:"target,omitempty"`
	Action string `json:"action,omitempty"`
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

// handlePanelConn serves one read-only socket request, then closes.
func handlePanelConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	servePanel(conn, conn, false)
}

// servePanel answers exactly one request read from r. privileged is true only
// for `moonbit panel`, which runs as root through sudo.
func servePanel(r io.Reader, w io.Writer, privileged bool) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	var req panelRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &req); err != nil {
		writePanelEvent(w, "error", map[string]any{"msg": "invalid request: " + err.Error()})
		return
	}
	switch req.Cmd {
	case "ping":
		writePanelEvent(w, "pong", nil)
		return
	case "status":
		writePanelEvent(w, "status", panelStatus())
		return
	}
	if !privileged {
		writePanelEvent(w, "error", map[string]any{"msg": "the daemon socket is read-only; run " + req.Cmd + " through `sudo moonbit panel`"})
		return
	}
	switch req.Cmd {
	case "scan":
		runPanelOp(w, br, "panel_scan", []string{req.Mode}, func() error {
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
		runPanelOp(w, br, "panel_clean", []string{fmt.Sprintf("force=%t", req.Force)}, func() error {
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
	case "docker":
		runPanelOp(w, br, "panel_docker", []string{req.Op}, func() error { return panelDocker(req.Op) })
	case "schedule":
		panelSchedule(w, req.Target, req.Action)
	default:
		writePanelEvent(w, "error", map[string]any{"msg": "unknown cmd: " + req.Cmd})
	}
}

// panelDocker runs the TUI's Docker cleanup and reports what Docker freed.
func panelDocker(op string) error {
	spec, ok := docker.PruneSpecFor(op)
	if !ok {
		return fmt.Errorf("unknown docker cleanup %q", op)
	}
	auditLog, _ := audit.NewLogger()
	if auditLog != nil {
		defer auditLog.Close()
	}
	if err := exec.Command("docker", "version").Run(); err != nil {
		if auditLog != nil {
			auditLog.LogDockerOperation(spec.AuditOperation, []string{}, "failed", err)
		}
		return fmt.Errorf("docker is not installed or not running")
	}
	out, err := exec.CommandContext(jsonContext(), "docker", spec.Args...).CombinedOutput()
	if auditLog != nil {
		result := "success"
		if err != nil {
			result = "failed"
		}
		auditLog.LogDockerOperation(spec.AuditOperation, spec.Args, result, err)
	}
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("docker %s: %s", strings.Join(spec.Args, " "), lastLine(msg))
		}
		return err
	}
	jsonEmit.emit("docker_done", map[string]any{"op": op, "reclaimed": dockerReclaimed(string(out))})
	return nil
}

// dockerReclaimed pulls "1.2GB" out of prune's "Total reclaimed space: 1.2GB".
func dockerReclaimed(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Total reclaimed space:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// panelSchedule enables or disables the daemon or the timers, as the TUI's
// Schedule screen does. The units conflict, so systemd stops one mode when
// the other starts.
func panelSchedule(w io.Writer, target, action string) {
	if action != "enable" && action != "disable" {
		writePanelEvent(w, "error", map[string]any{"msg": "schedule action must be enable or disable"})
		return
	}
	var names []string
	switch target {
	case "daemon":
		names = []string{units.Daemon}
	case "timers":
		names = units.Timers
	default:
		writePanelEvent(w, "error", map[string]any{"msg": "schedule target must be daemon or timers"})
		return
	}
	err := units.Apply(action, names...)
	if auditLog, _ := audit.NewLogger(); auditLog != nil {
		result := "success"
		if err != nil {
			result = "failed"
		}
		auditLog.LogSystemdOperation(action, strings.Join(names, ", "), result, err)
		auditLog.Close()
	}
	if err != nil {
		writePanelEvent(w, "error", map[string]any{"msg": err.Error()})
		return
	}
	writePanelEvent(w, "schedule_done", map[string]any{"target": target, "action": action})
}

// runPanelOp takes the operation lock shared with the daemon, the timers and
// the TUI, streams the same NDJSON events as --json, and always ends with
// exactly one terminal event (the operations emit done/clean_done/docker_done
// themselves).
func runPanelOp(w io.Writer, cancel io.Reader, op string, args []string, fn func() error) {
	release, err := acquireOp()
	if err != nil {
		writePanelEvent(w, "error", map[string]any{"msg": err.Error()})
		return
	}
	defer release()
	initJSONTo(w, cancel)
	defer resetJSON()
	err = fn()
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

var panelCmd = &cobra.Command{
	Use:    "panel",
	Hidden: true,
	Short:  "Serve one desktop-panel request on stdin (the panel runs it through sudo)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isRunningAsRoot() {
			return errors.New("moonbit panel must run as root; the desktop panel starts it through sudo")
		}
		// ready tells the panel sudo accepted the password and moonbit is
		// listening, so it can send the request.
		writePanelEvent(os.Stdout, "ready", nil)
		servePanel(os.Stdin, os.Stdout, true)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(panelCmd)
}
