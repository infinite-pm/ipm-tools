package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryCommandAnswersVersion builds every binary under cmd/ and asks it
// --version.
//
// It walks the directory rather than listing the commands, so one added later
// is covered without anyone remembering to extend this test. That is precisely
// the failure being guarded: --version was implemented in ipm-rpc alone — the
// binary the VS Code extension interrogates — while the other six shipped in
// the same release tarball and answered `flag provided but not defined:
// -version`, which is a poor reply from a tool downloaded as part of a
// versioned archive.
//
// cmd-dev/ is deliberately not covered: those tools are not released.
func TestEveryCommandAnswersVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds every command in cmd/")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	found := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		found++
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bin := filepath.Join(outDir, name)
			build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "./cmd/"+name)
			build.Dir = root
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build: %v\n%s", err, out)
			}
			out, err := exec.CommandContext(t.Context(), bin, "--version").CombinedOutput()
			if err != nil {
				t.Fatalf("--version: %v\n%s", err, out)
			}
			line := strings.TrimSpace(string(out))
			prefix := name + " "
			if !strings.HasPrefix(line, prefix) {
				t.Fatalf("expected %q prefix, got %q", prefix, line)
			}
			if strings.TrimSpace(strings.TrimPrefix(line, prefix)) == "" {
				t.Fatalf("no version after the tool name: %q", line)
			}
		})
	}
	if found == 0 {
		t.Fatal("no commands found under cmd/ — the walk is looking in the wrong place")
	}
}
