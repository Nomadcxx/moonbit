package cli

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// roundTrip runs one request through handlePanelConn over an in-memory pipe
// and returns the response events.
func roundTrip(t *testing.T, req string) []map[string]any {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() { handlePanelConn(server); close(done) }()
	if _, err := client.Write([]byte(req + "\n")); err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	sc := bufio.NewScanner(client)
	for sc.Scan() {
		var e map[string]any
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("non-NDJSON line: %q", sc.Text())
		}
		events = append(events, e)
	}
	client.Close()
	<-done
	if err := sc.Err(); err != nil && !strings.Contains(err.Error(), "use of closed") {
		t.Fatal(err)
	}
	return events
}

func TestPanelPing(t *testing.T) {
	evs := roundTrip(t, `{"cmd":"ping"}`)
	if len(evs) != 1 || evs[0]["t"] != "pong" {
		t.Fatalf("want single pong, got %v", evs)
	}
}

func TestPanelBadRequest(t *testing.T) {
	evs := roundTrip(t, `not json`)
	if len(evs) != 1 || evs[0]["t"] != "error" {
		t.Fatalf("want single error, got %v", evs)
	}
}

func TestPanelUnknownCmd(t *testing.T) {
	evs := roundTrip(t, `{"cmd":"frobnicate"}`)
	if len(evs) != 1 || evs[0]["t"] != "error" {
		t.Fatalf("want single error, got %v", evs)
	}
}

func TestPanelStatusShape(t *testing.T) {
	evs := roundTrip(t, `{"cmd":"status"}`)
	if len(evs) != 1 || evs[0]["t"] != "status" {
		t.Fatalf("want single status, got %v", evs)
	}
	if evs[0]["daemon"] != false {
		t.Fatalf("daemon flag wrong outside daemon: %v", evs[0]["daemon"])
	}
}

func TestPanelBusy(t *testing.T) {
	opSem <- struct{}{}
	defer func() { <-opSem }()
	evs := roundTrip(t, `{"cmd":"scan","mode":"quick"}`)
	if len(evs) != 1 || evs[0]["t"] != "error" || !strings.Contains(evs[0]["msg"].(string), "progress") {
		t.Fatalf("want busy error, got %v", evs)
	}
}

func TestPanelSocketRoundTrip(t *testing.T) {
	path := t.TempDir() + "/panel.sock"
	srv, err := listenPanelSocket(path, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte(`{"cmd":"ping"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, `"pong"`) {
		t.Fatalf("want pong, got %q", line)
	}
	conn.Close()
}
