package main

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	iplog "github.com/infinite-pm/ipm-tools/pkg/ipm/log"
)

// newCmdTestServer builds a bare server suitable for direct method calls
// (no jrpc2 transport). The notify hook silently discards anything the
// server would publish.
func newCmdTestServer(t *testing.T) *server {
	t.Helper()
	iplog.SetupDiscard()
	s := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.notifyOverride = func(string, any) {}
	return s
}

// fixtureRepo creates a temp git repo with one .md containing one ipmt
// block, and returns the .md absolute path.
func fixtureRepo(t *testing.T) (rootDir, mdPath string) {
	t.Helper()
	rootDir = t.TempDir()
	if err := exec.Command("git", "init", "-q", rootDir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	mdPath = filepath.Join(rootDir, "x.md")
	if err := os.WriteFile(mdPath, []byte("```ipmt\na ::e --> b ::e\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return rootDir, mdPath
}

func TestExecuteCommand_ipmEmbed_writesFiles(t *testing.T) {
	srv := newCmdTestServer(t)
	_, mdPath := fixtureRepo(t)

	out, err := srv.cmdEmbedOrCheck(t.Context(), []any{
		map[string]any{"file": mdPath},
	}, false /* dryRun */)
	if err != nil {
		t.Fatalf("ipm.embed: %v", err)
	}
	if !out.Changed {
		t.Fatalf("expected Changed=true; got %+v", out)
	}
	if out.Stats.InsertMarker != 1 {
		t.Errorf("expected InsertMarker=1; got %+v", out.Stats)
	}

	// Verify the SVG actually landed on disk.
	svgPath := filepath.Join(filepath.Dir(mdPath), "_ipm", "x", "100.ipm.svg")
	if _, err := os.Stat(svgPath); err != nil {
		t.Fatalf("expected SVG at %s; got %v", svgPath, err)
	}

	// Verify the .md got the marker.
	mdAfter, _ := os.ReadFile(mdPath)
	if !strings.Contains(string(mdAfter), "<!-- ipm-svg") {
		t.Fatalf("expected marker in md after embed; got:\n%s", mdAfter)
	}
}

func TestExecuteCommand_ipmCheck_isReadOnly(t *testing.T) {
	srv := newCmdTestServer(t)
	rootDir, mdPath := fixtureRepo(t)
	mdBefore, _ := os.ReadFile(mdPath)

	out, err := srv.cmdEmbedOrCheck(t.Context(), []any{
		map[string]any{"file": mdPath},
	}, true /* dryRun = ipm.check */)
	if err != nil {
		t.Fatalf("ipm.check: %v", err)
	}
	if out.Changed {
		t.Fatalf("ipm.check must not write; Changed=true: %+v", out)
	}
	if out.Stats.InsertMarker != 1 {
		t.Errorf("ipm.check should still report InsertMarker=1; got %+v", out.Stats)
	}

	// .md should be untouched.
	mdAfter, _ := os.ReadFile(mdPath)
	if string(mdAfter) != string(mdBefore) {
		t.Fatalf("ipm.check rewrote the file:\n  before: %q\n  after:  %q", mdBefore, mdAfter)
	}

	// No SVG should have been written.
	svgPath := filepath.Join(rootDir, "_ipm", "x", "100.ipm.svg")
	if _, err := os.Stat(svgPath); !os.IsNotExist(err) {
		t.Fatalf("ipm.check wrote an SVG; err=%v", err)
	}
}

func TestExecuteCommand_ipmEmbed_idempotent(t *testing.T) {
	srv := newCmdTestServer(t)
	_, mdPath := fixtureRepo(t)

	// First run lands changes.
	if _, err := srv.cmdEmbedOrCheck(t.Context(), []any{map[string]any{"file": mdPath}}, false); err != nil {
		t.Fatal(err)
	}
	// Second run must be a no-op.
	out, err := srv.cmdEmbedOrCheck(t.Context(), []any{map[string]any{"file": mdPath}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Changed {
		t.Errorf("second run unexpectedly Changed=true; stats: %+v", out.Stats)
	}
	if out.Stats.InsertMarker != 0 || out.Stats.Rehash != 0 || out.Stats.Rerender != 0 {
		t.Errorf("expected zero changes on idempotent run; got %+v", out.Stats)
	}
}

func TestExecuteCommand_fileURIWorks(t *testing.T) {
	srv := newCmdTestServer(t)
	_, mdPath := fixtureRepo(t)
	uri := "file://" + mdPath

	out, err := srv.cmdEmbedOrCheck(t.Context(), []any{map[string]any{"file": uri}}, true)
	if err != nil {
		t.Fatalf("file:// URI: %v", err)
	}
	if out.Stats.InsertMarker != 1 {
		t.Errorf("expected 1 insert via URI; got %+v", out.Stats)
	}
}

func TestExecuteCommand_missingArgErrors(t *testing.T) {
	srv := newCmdTestServer(t)
	if _, err := srv.cmdEmbedOrCheck(t.Context(), nil, false); err == nil {
		t.Fatal("expected error for missing arg")
	}
	if _, err := srv.cmdEmbedOrCheck(t.Context(), []any{"not a map"}, false); err == nil {
		t.Fatal("expected error for non-map arg")
	}
	if _, err := srv.cmdEmbedOrCheck(t.Context(), []any{map[string]any{}}, false); err == nil {
		t.Fatal("expected error for missing 'file'")
	}
}
