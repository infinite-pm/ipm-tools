package main

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStdioFlagAccepted spawns the built binary with `--stdio` (the flag
// vscode-languageclient unconditionally appends when transport ==
// TransportKind.stdio) and verifies it does NOT exit immediately with the
// "flag provided but not defined" error. We can't easily run a full LSP
// handshake here without pulling in test infrastructure, so we settle for:
// the process is still running 200ms after spawn (it would die in <50ms
// otherwise) AND the binary didn't print the Go flag-package error to
// stderr.
//
// Regression test for vscode-infinite-pm <-> ipm-rpc startup failure.
func TestStdioFlagAccepted(t *testing.T) {
	bin := buildBinary(t)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--stdio", "--quiet")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Read stderr in the background; we'll consume what's accumulated when
	// the process exits or we time out.
	stderrCh := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(stderrPipe)
		stderrCh <- b
	}()

	// Give the server a moment to either die from a bad flag OR settle into
	// its LSP read loop. ipm-rpc with a bad flag dies in <50ms; the read
	// loop will block indefinitely on stdin.
	time.Sleep(200 * time.Millisecond)

	// Close stdin → triggers a clean exit on the LSP read loop.
	_ = stdin.Close()

	waitErr := cmd.Wait()
	stderrBytes := <-stderrCh
	stderr := string(stderrBytes)

	if strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("ipm-rpc rejected --stdio:\n%s", stderr)
	}
	// We DO expect a clean exit (or signal-terminated), not an exit code 2
	// (flag parse error) or 1 (other early failure).
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 2 {
			t.Fatalf("exit code 2 (flag parse error); stderr:\n%s", stderr)
		}
	}
}

// TestVersionFlag verifies `ipm-rpc --version` prints a version line and
// exits 0. The VS Code extension uses this to display server info; if the
// flag silently disappears, the extension's "Show Server Info" command
// will show an empty version.
func TestVersionFlag(t *testing.T) {
	bin := buildBinary(t)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		t.Fatalf("--version failed: %v", err)
	}
	line := strings.TrimSpace(string(out))
	if !strings.HasPrefix(line, "ipm-rpc ") {
		t.Fatalf(`expected "ipm-rpc <version>" prefix, got: %q`, line)
	}
	if len(line) <= len("ipm-rpc ") {
		t.Fatalf("expected a version string after `ipm-rpc `, got: %q", line)
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "ipm-rpc-test")
	build := exec.Command("go", "build", "-o", out, ".")
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, msg)
	}
	return out
}
