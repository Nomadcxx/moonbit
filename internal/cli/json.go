package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// Machine-readable output (--json). One JSON object per line on stdout;
// everything human stays on stderr or is suppressed. The sysc-shell panel
// plugin consumes these events; it cannot drive the terminal+sudo launcher
// because a panel has no TTY, and it cannot signal a pkexec'd root child
// (EPERM, and pkexec forwards no signals) - so cancellation is cooperative:
// closing stdin cancels the operation.
var (
	jsonOut      bool
	jsonEmit     *emitter
	jsonCtx      = context.Background()
	jsonThrottle = newThrottler(250 * time.Millisecond)
)

// initJSON wires the emitter and the stdin-close cancel context.
func initJSON() { initJSONTo(os.Stdout, os.Stdin) }

// initJSONTo wires the JSON globals to an arbitrary sink (stdout/stdin for the
// CLI, a panel socket connection for the daemon).
func initJSONTo(w io.Writer, cancelRead io.Reader) {
	jsonOut = true
	jsonEmit = newEmitterTo(w)
	jsonCtx = cancelOnEOF(cancelRead)
	jsonThrottle = newThrottler(250 * time.Millisecond)
}

// resetJSON restores the globals after a daemon connection ends, so the
// timer loop keeps using the human output path.
func resetJSON() {
	jsonOut = false
	jsonEmit = nil
	jsonCtx = context.Background()
}

// emitTerminal writes the final event for a failed/cancelled command and
// returns the process exit code. Every --json operation ends in exactly one
// terminal event: done | clean_done | cancelled | error.
func emitTerminal(err error) int {
	if jsonCtx.Err() != nil || errors.Is(err, context.Canceled) {
		jsonEmit.emit("cancelled", nil)
		return 0
	}
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	jsonEmit.emit("error", map[string]any{"msg": msg})
	return 1
}

// jsonTerminal emits the terminal event and exits (CLI path only).
func jsonTerminal(err error) { os.Exit(emitTerminal(err)) }

// emitter writes newline-delimited JSON events to a writer.
type emitter struct{ enc *json.Encoder }

func newEmitter() *emitter { return newEmitterTo(os.Stdout) }

func newEmitterTo(w io.Writer) *emitter { return &emitter{enc: json.NewEncoder(w)} }

func (e *emitter) emit(event string, fields map[string]any) {
	if e == nil {
		return
	}
	evt := make(map[string]any, len(fields)+1)
	evt["t"] = event
	for k, v := range fields {
		evt[k] = v
	}
	_ = e.enc.Encode(evt)
}

// cancelOnEOF cancels when r reaches EOF or is closed.
func cancelOnEOF(r io.Reader) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, _ = io.Copy(io.Discard, r)
		cancel()
	}()
	return ctx
}

// throttler rate-limits progress events; the first call always passes.
type throttler struct {
	interval time.Duration
	last     time.Time
}

func newThrottler(d time.Duration) *throttler { return &throttler{interval: d} }

func (t *throttler) ready() bool {
	if t == nil {
		return true
	}
	now := time.Now()
	if t.last.IsZero() || now.Sub(t.last) >= t.interval {
		t.last = now
		return true
	}
	return false
}
