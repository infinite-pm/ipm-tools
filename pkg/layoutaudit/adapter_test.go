package layoutaudit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// An era whose CLI is not today's is entered through a generated adapter, so
// everything downstream keeps talking to one binary that takes --in x.ipmt.
func TestAdapterRunsTheEraRecipe(t *testing.T) {
	bin := t.TempDir()
	path, err := writeAdapter(bin, []string{
		`printf '{"version":"old","in":"%s"}' "$(basename {in})" > {tmp}`,
		`cat {tmp}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Invoked exactly as the sweep invokes an engine.
	out, err := exec.Command(path, "--in", "/some/where/case.ipmt", "--out", "-", "--pretty=true").Output()
	if err != nil {
		t.Fatalf("adapter failed on the sweep's own arguments: %v", err)
	}
	if got := string(out); got != `{"version":"old","in":"case.ipmt"}` {
		t.Fatalf("adapter produced %q", got)
	}
	if info, err := os.Stat(path); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("adapter is not executable: %v", err)
	}
}

// The bug that cost an entire era of history: on a CACHE HIT the builder
// returned the binary called layout-gen, ignoring the adapter. The sweep then
// fed .ipmt to a tool that takes JSON, every diagram failed, and the column
// reported "nothing moved" when the truth was "nothing ran".
func TestACachedEraIsStillEnteredThroughItsAdapter(t *testing.T) {
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Dir(filepath.Dir(repo)) // .../pkg/layoutaudit -> repo root
	sha, err := Git(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Skip("not a git checkout")
	}

	cache := t.TempDir()
	binDir := filepath.Join(cache, Short(sha))
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A cache populated by an earlier build of that era.
	for _, name := range []string{"layout-gen", "ipmt-parse", "adapter"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	e, err := BuildEngineWith(repo, sha, "cached", cache, "", false,
		BuildOptions{Packages: []string{"./cmd/ipmt-parse"}, Pipeline: []string{"{bin}/ipmt-parse --in {in}"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(e.LayoutGen, "adapter") {
		t.Fatalf("a cached era resolved to %q, not its adapter", e.LayoutGen)
	}

	// And an era WITHOUT a recipe still resolves to layout-gen.
	plain, err := BuildEngineWith(repo, sha, "cached", cache, "", false, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(plain.LayoutGen, "layout-gen") {
		t.Fatalf("an ordinary era resolved to %q", plain.LayoutGen)
	}
}
