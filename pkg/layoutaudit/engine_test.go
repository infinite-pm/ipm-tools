package layoutaudit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// tinyEngineRepo is a git repository holding the smallest thing that builds:
// enough to exercise export → build → cache without compiling this project.
func tinyEngineRepo(t *testing.T) (repo, sha string) {
	t.Helper()
	repo = t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module tiny\n\ngo 1.21\n")
	write("cmd/layout-gen/main.go", "package main\n\nfunc main() {}\n")
	// Bulk, standing in for the corpora and testdata a real checkout carries —
	// the reason a stale tree is expensive rather than merely untidy.
	for _, name := range []string{"a", "b", "c", "d"} {
		write("tests/corpus/"+name+".ipmt", "t"+name+"\n")
	}

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"add", "-A"},
		{"commit", "-qm", "tiny"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	sha, err := Git(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Skip("no git")
	}
	return repo, sha
}

// The binaries are the cache; the tree they were built from is scratch.
//
// Leaving it behind is not free. A checkout of this repository is ~11 MB and
// ~2,000 files, a history sweep exports one per engine commit, and they landed
// under temp/ — which the editor watches recursively. 280 of them had piled up
// to 3.1 GB and 550,000 watched files.
func TestTheSourceTreeDoesNotOutliveItsBuild(t *testing.T) {
	repo, sha := tinyEngineRepo(t)
	cache := t.TempDir()

	e, err := BuildEngineWith(repo, sha, "old", cache, "", false,
		BuildOptions{Packages: []string{"./cmd/layout-gen"}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(e.LayoutGen); err != nil {
		t.Fatalf("no engine binary was cached: %v", err)
	}

	srcDir := filepath.Join(cache, "src", Short(sha))
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		n := 0
		filepath.Walk(srcDir, func(string, os.FileInfo, error) error { n++; return nil })
		t.Errorf("the exported tree outlived its build: %s still holds %d entries", srcDir, n)
	}
}

// A build that FAILED is the one time the tree is worth reading, and the error
// names it — so that one stays.
func TestAFailedBuildKeepsItsTreeToLookAt(t *testing.T) {
	repo, sha := tinyEngineRepo(t)
	cache := t.TempDir()

	_, err := BuildEngineWith(repo, sha, "old", cache, "", false,
		BuildOptions{Packages: []string{"./cmd/does-not-exist"}})
	if err == nil {
		t.Fatal("building a package that is not there succeeded")
	}
	srcDir := filepath.Join(cache, "src", Short(sha))
	if _, statErr := os.Stat(srcDir); statErr != nil {
		t.Errorf("the tree a failed build refers to was deleted: %v", statErr)
	}
}
