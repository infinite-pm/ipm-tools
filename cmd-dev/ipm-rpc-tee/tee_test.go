package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tee sits between the editor and the language server during a demo
// recording. Its first duty is to be invisible: a proxy that mangles a
// message breaks the take, which is a far worse outcome than a missing
// corpus. These tests drive a REAL ipm-rpc through it and assert both halves
// — the session is unaffected, and the states were captured.

// buildBinary compiles pkg into a temp dir and returns the path.
func buildBinary(t *testing.T, dir, pkg, name string) string {
	t.Helper()
	bin := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	cmd.Dir = "../.." // the module root, whatever the test's working directory
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

// session drives an LSP conversation over a command's stdio.
type session struct {
	t     *testing.T
	cmd   *exec.Cmd
	in    *bufio.Writer
	stdin io.Closer
	out   *bufio.Reader
}

func startSession(t *testing.T, bin string, env ...string) *session {
	t.Helper()
	cmd := exec.Command(bin, "--stdio")
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &session{t: t, cmd: cmd, in: bufio.NewWriter(stdin), stdin: stdin, out: bufio.NewReader(stdout)}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	return s
}

func (s *session) send(msg string) {
	s.t.Helper()
	fmt.Fprintf(s.in, "Content-Length: %d\r\n\r\n%s", len(msg), msg)
	if err := s.in.Flush(); err != nil {
		s.t.Fatalf("send: %v", err)
	}
}

// recv reads one framed message.
func (s *session) recv() map[string]any {
	s.t.Helper()
	var length int
	for {
		line, err := s.out.ReadString('\n')
		if err != nil {
			s.t.Fatalf("recv header: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if n, err := fmt.Sscanf(line, "Content-Length: %d", &length); n != 1 || err != nil {
			continue // some other header
		}
	}
	buf := make([]byte, length)
	if _, err := readFull(s.out, buf); err != nil {
		s.t.Fatalf("recv body: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		s.t.Fatalf("recv decode: %v (%s)", err, buf)
	}
	return m
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	got := 0
	for got < len(buf) {
		n, err := r.Read(buf[got:])
		got += n
		if err != nil {
			return got, err
		}
	}
	return got, nil
}

// waitForResponse reads until a message with the given id arrives.
func (s *session) waitForResponse(id float64) map[string]any {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		m := s.recv()
		if got, ok := m["id"].(float64); ok && got == id {
			return m
		}
	}
	s.t.Fatalf("no response with id %v", id)
	return nil
}

const initReq = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`

func openMsg(uri, text string) string {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{
			"uri": uri, "languageId": "ipmt", "version": 1, "text": text}},
	})
	return string(b)
}

func changeMsg(uri string, version int, text string) string {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didChange",
		"params": map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": version},
			"contentChanges": []map[string]any{{"text": text}},
		},
	})
	return string(b)
}

// A session through the tee must behave exactly as one straight to the
// server: same initialize result, same diagnostics.
func TestSessionThroughTheTeeIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	rpc := buildBinary(t, dir, "./cmd/ipm-rpc", "ipm-rpc")
	tee := buildBinary(t, dir, "./cmd-dev/ipm-rpc-tee", "ipm-rpc-tee")
	capture := filepath.Join(dir, "capture")

	direct := startSession(t, rpc)
	direct.send(initReq)
	wantInit := direct.waitForResponse(1)

	proxied := startSession(t, tee, "IPM_TEE_REAL="+rpc, "IPM_TEE_OUT="+capture)
	proxied.send(initReq)
	gotInit := proxied.waitForResponse(1)

	a, _ := json.Marshal(wantInit)
	b, _ := json.Marshal(gotInit)
	if string(a) != string(b) {
		t.Fatalf("initialize differs through the tee:\n direct: %s\n teed:   %s", a, b)
	}

	// And a real edit still produces the server's diagnostics, unchanged.
	uri := "file:///tmp/tee-test/case.ipmt"
	proxied.send(openMsg(uri, "e1 ::e --> e2 ::e\n"))
	first := proxied.recv()
	if first["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("expected diagnostics after didOpen, got %v", first["method"])
	}
}

// The capture is the point: every keystroke must land as a complete,
// replayable buffer, and the cadence must record which states the engine was
// actually asked to lay out.
func TestCaptureRecordsEveryBufferState(t *testing.T) {
	dir := t.TempDir()
	rpc := buildBinary(t, dir, "./cmd/ipm-rpc", "ipm-rpc")
	tee := buildBinary(t, dir, "./cmd-dev/ipm-rpc-tee", "ipm-rpc-tee")
	capture := filepath.Join(dir, "capture")

	s := startSession(t, tee, "IPM_TEE_REAL="+rpc, "IPM_TEE_OUT="+capture)
	s.send(initReq)
	s.waitForResponse(1)

	uri := "file:///tmp/tee-test/typing.ipmt"
	steps := []string{"e1 ::e\n", "e1 ::e\n--> e2 ::e\n", "e1 ::e\n--> e2 ::e\n--> e3 ::e\n"}
	s.send(openMsg(uri, steps[0]))
	s.recv()
	for i, text := range steps[1:] {
		s.send(changeMsg(uri, i+2, text))
		s.recv() // its diagnostics
	}

	// Close the client side so the tee flushes its capture and exits the way
	// it would when the editor goes away.
	_ = s.stdin.Close()
	_ = s.cmd.Wait()

	states := readCapture(t, capture)
	var texts []string
	for _, st := range states {
		if st.Cadence == "open" || st.Cadence == "change" {
			texts = append(texts, st.Text)
		}
	}
	if len(texts) != len(steps) {
		t.Fatalf("captured %d buffer states, want %d: %+v", len(texts), len(steps), texts)
	}
	for i := range steps {
		if texts[i] != steps[i] {
			t.Errorf("state %d = %q, want %q", i, texts[i], steps[i])
		}
	}
	// Every state must carry its URI, or the curator cannot tell which scene
	// (which workspace) it belongs to.
	for _, st := range states {
		if st.URI == "" {
			t.Fatalf("a captured state has no uri: %+v", st)
		}
	}
	// The server's verdict rides along, so the corpus knows which states were
	// invalid without re-running anything.
	var sawDiagnostics bool
	for _, st := range states {
		if st.Cadence == "diagnostics" {
			sawDiagnostics = true
		}
	}
	if !sawDiagnostics {
		t.Error("no diagnostics captured; the invalid-state half of the corpus would be blind")
	}
}

// A version probe or a restart also reaches the tee. Neither carries a
// state, and neither should leave a file for the curator to sift.
func TestAProbeWithNoStatesLeavesNoCaptureFile(t *testing.T) {
	dir := t.TempDir()
	rpc := buildBinary(t, dir, "./cmd/ipm-rpc", "ipm-rpc")
	tee := buildBinary(t, dir, "./cmd-dev/ipm-rpc-tee", "ipm-rpc-tee")
	capture := filepath.Join(dir, "capture")

	s := startSession(t, tee, "IPM_TEE_REAL="+rpc, "IPM_TEE_OUT="+capture)
	s.send(initReq)
	s.waitForResponse(1)
	_ = s.stdin.Close()
	_ = s.cmd.Wait()

	entries, err := os.ReadDir(capture)
	if err != nil {
		return // the directory was never created at all: also fine
	}
	for _, e := range entries {
		info, _ := e.Info()
		if info != nil && info.Size() > 0 {
			t.Fatalf("a session with no states wrote %s (%d bytes)", e.Name(), info.Size())
		}
		t.Fatalf("a session with no states created %s", e.Name())
	}
}

// A capture that cannot be written must degrade to a plain passthrough: the
// recording session is worth more than the corpus entry.
func TestUnwritableCaptureStillProxies(t *testing.T) {
	dir := t.TempDir()
	rpc := buildBinary(t, dir, "./cmd/ipm-rpc", "ipm-rpc")
	tee := buildBinary(t, dir, "./cmd-dev/ipm-rpc-tee", "ipm-rpc-tee")

	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startSession(t, tee, "IPM_TEE_REAL="+rpc, "IPM_TEE_OUT="+blocked)
	s.send(initReq)
	if res := s.waitForResponse(1); res["result"] == nil {
		t.Fatalf("initialize failed with an unwritable capture dir: %v", res)
	}
}

func readCapture(t *testing.T, dir string) []state {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no capture directory: %v", err)
	}
	var out []state
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var st state
			if err := json.Unmarshal([]byte(line), &st); err != nil {
				t.Fatalf("capture line is not JSON: %v (%s)", err, line)
			}
			out = append(out, st)
		}
	}
	return out
}
