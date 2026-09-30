package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmitterNDJSON(t *testing.T) {
	f := filepath.Join(t.TempDir(), "out")
	os.Remove(f)
	fh, err := os.Create(f)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = fh
	e := newEmitter()
	os.Stdout = old
	e.emit("category", map[string]any{"name": "apt", "i": 1, "total": 3})
	e.emit("cancelled", nil)
	fh.Close()
	data, _ := os.ReadFile(f)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 NDJSON lines, got %d: %q", len(lines), string(data))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["t"] != "category" || first["name"] != "apt" {
		t.Fatalf("bad event: %v", first)
	}
	var second map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if second["t"] != "cancelled" {
		t.Fatalf("bad terminal: %v", second)
	}
}

func TestCancelOnEOF(t *testing.T) {
	pr, pw := io.Pipe()
	ctx := cancelOnEOF(pr)
	select {
	case <-ctx.Done():
		t.Fatal("cancelled before EOF")
	default:
	}
	pw.Close()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("no cancel after EOF")
	}
}

func TestThrottler(t *testing.T) {
	tr := newThrottler(50 * time.Millisecond)
	if !tr.ready() {
		t.Fatal("first tick must pass")
	}
	if tr.ready() {
		t.Fatal("second immediate tick must be throttled")
	}
	time.Sleep(60 * time.Millisecond)
	if !tr.ready() {
		t.Fatal("tick after interval must pass")
	}
	var nilT *throttler
	if !nilT.ready() {
		t.Fatal("nil throttler must pass")
	}
}
