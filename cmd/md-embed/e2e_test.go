package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Shared binary path, built once by TestMain. Using a pre-built binary lets
// tests set an arbitrary cwd (needed to verify auto-detect of repo root from
// a subdirectory) and runs the whole e2e suite a lot faster than `go run`
// every test.
var binPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "md-embed-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: temp dir:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)
	binPath = filepath.Join(tmpDir, "md-embed")

	repoRoot, err := locateRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: locate repo root:", err)
		os.Exit(1)
	}
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = filepath.Join(repoRoot, "cmd", "md-embed")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: go build:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func locateRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod from %s", wd)
		}
		dir = parent
	}
}

// e2eRun invokes the prebuilt binary with the test's cwd left at the default
// (the Go test runner's cwd, which is the cmd's package directory). Most
// tests use this — they pass --root explicitly, so cwd doesn't matter.
func e2eRun(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	return e2eRunIn(t, "", args...)
}

// e2eRunIn invokes the binary with cwd set to dir (or unset if dir is "").
// Use this when the test needs to verify behavior that depends on cwd, such
// as auto-detect of the repo root.
func e2eRunIn(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("unexpected error launching: %v", err)
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestE2E_InsertMarkerAndRender(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "docs", "x.md")
	writeFile(t, mdPath, "# title\n\n```ipmt\nPatrick ::e --> Bob ::e\n```\n\ntrailing\n")

	// Initial run: should insert one marker and render one SVG.
	stdout, stderr, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("first run exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "insert=1") {
		t.Fatalf("expected insert=1; stdout=%s", stdout)
	}

	mdOut := readFile(t, mdPath)
	if !strings.Contains(mdOut, "<!-- ipm-svg id=100 hash=") {
		t.Fatalf("marker not inserted:\n%s", mdOut)
	}
	if !strings.Contains(mdOut, "![](../_ipm/docs/x/100.ipm.svg)") {
		t.Fatalf("image link missing or wrong path:\n%s", mdOut)
	}

	svgPath := filepath.Join(root, "_ipm", "docs", "x", "100.ipm.svg")
	svg := readFile(t, svgPath)
	if !strings.Contains(svg, "ipm-svg meta:") {
		t.Fatalf("SVG missing meta comment:\n%s", svg)
	}
	if !strings.Contains(svg, "source-id=100") {
		t.Fatalf("SVG meta missing source-id=100:\n%s", svg)
	}

	// Second run: idempotent.
	stdout, _, code = e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("idempotent run exit=%d", code)
	}
	if !strings.Contains(stdout, "insert=0 rehash=0 rerender=0") {
		t.Fatalf("not idempotent; stdout=%s", stdout)
	}
}

func TestE2E_CheckModeExits1WhenStale(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n")

	// No marker yet → --check should fail.
	stdout, _, code := e2eRun(t, "--root", root, "--check")
	if code != 1 {
		t.Fatalf("--check on stale should exit 1, got %d (stdout=%s)", code, stdout)
	}
	if !strings.Contains(stdout, "STALE") {
		t.Fatalf("expected STALE marker in output:\n%s", stdout)
	}

	// Source file should not be modified by --check.
	if readFile(t, mdPath) != "```ipmt\nA --> B\n```\n" {
		t.Fatal("--check modified the markdown file")
	}
}

func TestE2E_CheckPassesAfterWrite(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n")

	if _, _, code := e2eRun(t, "--root", root); code != 0 {
		t.Fatalf("write exit=%d", code)
	}
	if _, _, code := e2eRun(t, "--root", root, "--check"); code != 0 {
		t.Fatalf("post-write --check should exit 0, got %d", code)
	}
}

func TestE2E_RehashOnSourceEdit(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n")
	if _, _, code := e2eRun(t, "--root", root); code != 0 {
		t.Fatalf("initial exit=%d", code)
	}
	old := readFile(t, mdPath)

	// Edit the source ipmt content; marker hash should be stale.
	edited := strings.Replace(old, "A --> B", "A --> C", 1)
	writeFile(t, mdPath, edited)

	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("edit run exit=%d", code)
	}
	if !strings.Contains(stdout, "rehash=1") {
		t.Fatalf("expected rehash=1; stdout=%s", stdout)
	}

	// Final marker hash differs from initial.
	finalText := readFile(t, mdPath)
	if extractHash(t, old) == extractHash(t, finalText) {
		t.Fatal("hash should have changed after source edit")
	}
}

func TestE2E_RerenderWhenSVGDeleted(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n")
	if _, _, code := e2eRun(t, "--root", root); code != 0 {
		t.Fatal()
	}
	svgPath := filepath.Join(root, "_ipm", "x", "100.ipm.svg")
	if err := os.Remove(svgPath); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 || !strings.Contains(stdout, "rerender=1") {
		t.Fatalf("rerender path failed; exit=%d stdout=%s", code, stdout)
	}
	if _, err := os.Stat(svgPath); err != nil {
		t.Fatalf("SVG not re-created: %v", err)
	}
}

func TestE2E_ForceRerendersAndForceMetaRewrites(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n")

	// First run inserts + renders. A fresh doc starts at the first key, "100".
	if _, _, code := e2eRun(t, "--root", root); code != 0 {
		t.Fatalf("first run exit=%d", code)
	}
	mdOut := readFile(t, mdPath)
	if !strings.Contains(mdOut, "<!-- ipm-svg id=100 hash=") || !strings.Contains(mdOut, "_ipm/x/100.ipm.svg") {
		t.Fatalf("expected key id=100 + matching image path:\n%s", mdOut)
	}
	if _, err := os.Stat(filepath.Join(root, "_ipm", "x", "100.ipm.svg")); err != nil {
		t.Fatalf("keyed SVG missing: %v", err)
	}
	// The second run is idempotent.
	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 || !strings.Contains(stdout, "insert=0 rehash=0 rerender=0") {
		t.Fatalf("expected idempotent run; exit=%d stdout=%s", code, stdout)
	}

	// --force bypasses the freshness check, but the re-rendered SVG is
	// byte-identical to what's on disk, so the write is skipped rather than
	// churning the file.
	stdout, _, code = e2eRun(t, "--root", root, "--force")
	if code != 0 || !strings.Contains(stdout, "rerender=0 meta-skipped=1") {
		t.Fatalf("--force on a fresh keyed doc should skip the identical rewrite; exit=%d stdout=%s", code, stdout)
	}

	// --force-meta is the escape hatch: rewrite the SVG even when nothing but the
	// generated-by provenance would change.
	stdout, _, code = e2eRun(t, "--root", root, "--force", "--force-meta")
	if code != 0 || !strings.Contains(stdout, "rerender=1") {
		t.Fatalf("--force --force-meta should rewrite the block; exit=%d stdout=%s", code, stdout)
	}
}

func TestE2E_ForceRejectedWithCheck(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.md"), "```ipmt\nA --> B\n```\n")
	_, stderr, code := e2eRun(t, "--root", root, "--check", "--force")
	if code != 2 {
		t.Fatalf("--check --force should exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "defeats --check") {
		t.Fatalf("expected explanatory error; stderr=%s", stderr)
	}
}

func TestE2E_PruneDefaultOn(t *testing.T) {
	// Pruning is on by default — no flag needed.
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n\n```ipmt\nC --> D\n```\n")
	if _, _, code := e2eRun(t, "--root", root); code != 0 {
		t.Fatal()
	}
	// Drop the second block → second SVG becomes orphaned.
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n")

	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("default run exit=%d", code)
	}
	if !strings.Contains(stdout, "pruned=1") {
		t.Fatalf("expected pruned=1 by default; stdout=%s", stdout)
	}
	orphan := filepath.Join(root, "_ipm", "x", "110.ipm.svg")
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan SVG not pruned: %v", err)
	}
	kept := filepath.Join(root, "_ipm", "x", "100.ipm.svg")
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("non-orphan SVG was deleted: %v", err)
	}
}

func TestE2E_NoPruneDisablesSweep(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n\n```ipmt\nC --> D\n```\n")
	if _, _, code := e2eRun(t, "--root", root); code != 0 {
		t.Fatal()
	}
	// Drop the second block.
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n")

	stdout, _, code := e2eRun(t, "--root", root, "--no-prune")
	if code != 0 {
		t.Fatalf("--no-prune run exit=%d", code)
	}
	if !strings.Contains(stdout, "pruned=0") {
		t.Fatalf("expected pruned=0 with --no-prune; stdout=%s", stdout)
	}
	orphan := filepath.Join(root, "_ipm", "x", "110.ipm.svg")
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("orphan SVG should still exist with --no-prune: %v", err)
	}
}

func TestE2E_NoFilesNoError(t *testing.T) {
	root := t.TempDir()
	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("empty tree exit=%d stdout=%s", code, stdout)
	}
}

func TestE2E_UnterminatedFenceReportedNotFatal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.md"), "```ipmt\nA --> B\nno close\n")
	stdout, stderr, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("should not crash on unterminated fence; exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "unterminated=1") {
		t.Fatalf("did not report unterminated; stdout=%s", stdout)
	}
}

func TestE2E_SkipsSvgDirWhenWalking(t *testing.T) {
	root := t.TempDir()
	// Plant a fake .md inside _ipm — the walker should skip it.
	writeFile(t, filepath.Join(root, "_ipm", "should-be-skipped.md"), "```ipmt\nQ --> Z\n```\n")
	writeFile(t, filepath.Join(root, "real.md"), "```ipmt\nA --> B\n```\n")
	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout, "files=1") {
		t.Fatalf("walker should have seen 1 .md; stdout=%s", stdout)
	}
}

func TestE2E_AutoDetectRoot_FromSubdir(t *testing.T) {
	// With no --root flag, the tool walks up from cwd to find the repo root
	// (a directory containing .git or .ipm.conf). Running from a
	// nested subdirectory must still produce SVGs at the repo root, not at
	// the subdir.
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mdPath := filepath.Join(root, "docs", "x.md")
	writeFile(t, mdPath, "```ipmt\nA --> B\n```\n")

	// Run with cwd set deep inside the repo.
	subdir := filepath.Join(root, "docs")
	stdout, _, code := e2eRunIn(t, subdir /* no --root */)
	if code != 0 {
		t.Fatalf("auto-detect run exit=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "insert=1") {
		t.Fatalf("expected insert=1; stdout=%s", stdout)
	}
	// SVG must land under repo-root/_ipm/..., NOT subdir/_ipm/...
	wantSVG := filepath.Join(root, "_ipm", "docs", "x", "100.ipm.svg")
	if _, err := os.Stat(wantSVG); err != nil {
		t.Fatalf("SVG not at repo root: %v", err)
	}
	subdirSVG := filepath.Join(subdir, "_ipm")
	if _, err := os.Stat(subdirSVG); err == nil {
		t.Fatalf("SVG was created in subdir instead of repo root")
	}
}

func TestE2E_ConfigFileOverridesDefaultSvgDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Config requests a non-default SVG directory.
	writeFile(t, filepath.Join(root, ".ipm.conf"), "svg-dir = docs/_diagrams\n")
	writeFile(t, filepath.Join(root, "x.md"), "```ipmt\nA --> B\n```\n")

	stdout, _, code := e2eRunIn(t, root)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "loaded .ipm.conf") {
		t.Fatalf("expected 'loaded .ipm.conf' in stdout; got:\n%s", stdout)
	}
	wantSVG := filepath.Join(root, "docs", "_diagrams", "x", "100.ipm.svg")
	if _, err := os.Stat(wantSVG); err != nil {
		t.Fatalf("SVG not at configured svg-dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "_ipm")); err == nil {
		t.Fatalf("SVG was placed at default _ipm despite config override")
	}
}

func TestE2E_CLISvgDirOverridesConfig(t *testing.T) {
	// CLI flag wins over config file.
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".ipm.conf"), "svg-dir = from-config\n")
	writeFile(t, filepath.Join(root, "x.md"), "```ipmt\nA --> B\n```\n")

	stdout, _, code := e2eRunIn(t, root, "--svg-dir", "from-flag")
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s", code, stdout)
	}
	wantSVG := filepath.Join(root, "from-flag", "x", "100.ipm.svg")
	if _, err := os.Stat(wantSVG); err != nil {
		t.Fatalf("SVG not at CLI svg-dir: %v\nstdout=%s", err, stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "from-config")); err == nil {
		t.Fatalf("config svg-dir leaked in despite CLI override")
	}
}

func TestE2E_EmbedIgnoreSkipsConfiguredFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// One exact path, one ** glob.
	writeFile(t, filepath.Join(root, ".ipm.conf"), "embed-ignore = docs/invalid.md, skip/**/*.md\n")
	body := "```ipmt\nA --> B\n```\n"
	keep := filepath.Join(root, "docs", "keep.md")
	ignoredExact := filepath.Join(root, "docs", "invalid.md")
	ignoredGlob := filepath.Join(root, "skip", "nested", "x.md")
	writeFile(t, keep, body)
	writeFile(t, ignoredExact, body)
	writeFile(t, ignoredGlob, body)

	stdout, _, code := e2eRunIn(t, root)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "embed-ignore: skipped 2 file(s)") {
		t.Fatalf("expected 'embed-ignore: skipped 2 file(s)' in stdout; got:\n%s", stdout)
	}
	// The kept file is processed: a marker is inserted.
	if !strings.Contains(readFile(t, keep), "<!-- ipm-svg id=") {
		t.Fatalf("kept file should have a marker inserted:\n%s", readFile(t, keep))
	}
	// Ignored files are left exactly as written — no marker, no SVG.
	for _, f := range []string{ignoredExact, ignoredGlob} {
		if strings.Contains(readFile(t, f), "ipm-svg") {
			t.Fatalf("ignored file %s should be untouched:\n%s", f, readFile(t, f))
		}
	}
	if _, err := os.Stat(filepath.Join(root, "_ipm", "docs", "invalid")); err == nil {
		t.Fatalf("ignored file should not have produced an SVG")
	}
}

func TestE2E_DetailsWrappedBlockRoundTrips(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	// Brand-new file: a <details>-wrapped ipmt fence, no marker yet.
	writeFile(t, mdPath, strings.Join([]string{
		"# title",
		"",
		"<details><summary>ipmt</summary>",
		"",
		"```ipmt",
		"A --> B",
		"```",
		"</details>",
		"",
		"trailing",
		"",
	}, "\n"))

	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("first run exit=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "insert=1") {
		t.Fatalf("expected insert=1; stdout=%s", stdout)
	}

	mdOut := readFile(t, mdPath)
	// Marker must sit AFTER </details>, not after the closing ```.
	idxClosingDetails := strings.Index(mdOut, "</details>")
	idxMarker := strings.Index(mdOut, "<!-- ipm-svg")
	if !(idxClosingDetails > 0 && idxClosingDetails < idxMarker) {
		t.Fatalf("marker should sit after </details>; got:\n%s", mdOut)
	}
	// And the marker must NOT sit inside the wrap (between closing ``` and </details>).
	idxClosingFence := strings.Index(mdOut, "\n```\n")
	if idxClosingFence > 0 && idxClosingFence < idxClosingDetails && idxMarker < idxClosingDetails {
		t.Fatalf("marker landed inside <details> wrap; got:\n%s", mdOut)
	}

	// Idempotent.
	stdout, _, code = e2eRun(t, "--root", root)
	if code != 0 || !strings.Contains(stdout, "insert=0 rehash=0 rerender=0") {
		t.Fatalf("not idempotent after first apply; exit=%d stdout=%s", code, stdout)
	}

	// Edit the source — next run should rehash, and the marker should remain
	// outside the wrap.
	mdOut = strings.Replace(mdOut, "A --> B", "A --> C", 1)
	writeFile(t, mdPath, mdOut)
	stdout, _, code = e2eRun(t, "--root", root)
	if code != 0 || !strings.Contains(stdout, "rehash=1") {
		t.Fatalf("rehash didn't fire after edit; exit=%d stdout=%s", code, stdout)
	}
	mdOut = readFile(t, mdPath)
	if strings.Count(mdOut, "<!-- ipm-svg ") != 1 {
		t.Fatalf("expected exactly one marker; got %d in:\n%s", strings.Count(mdOut, "<!-- ipm-svg "), mdOut)
	}
}

func TestE2E_PosBeforeIsPreserved(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	// User has manually placed the marker BEFORE the ipmt fence and tagged it
	// with pos=before. Initial hash is a placeholder; first run rehashes.
	writeFile(t, mdPath, strings.Join([]string{
		"intro paragraph",
		"",
		"<!-- ipm-svg id=01 hash=00000000 pos=before -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
		"```ipmt",
		"A --> B",
		"```",
		"",
		"trailing",
		"",
	}, "\n"))

	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("initial run exit=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "rehash=1") {
		t.Fatalf("expected rehash=1 (placeholder hash); stdout=%s", stdout)
	}

	mdOut := readFile(t, mdPath)
	if !strings.Contains(mdOut, "pos=before") {
		t.Fatalf("pos=before should survive the rehash; got:\n%s", mdOut)
	}
	idxComment := strings.Index(mdOut, "<!-- ipm-svg")
	idxFence := strings.Index(mdOut, "```ipmt")
	if !(idxComment < idxFence) {
		t.Fatalf("marker should still sit above the fence; got:\n%s", mdOut)
	}
	if strings.Contains(mdOut, "hash=00000000") {
		t.Fatalf("stale hash should have been replaced:\n%s", mdOut)
	}

	// Second run is fully idempotent.
	stdout, _, code = e2eRun(t, "--root", root)
	if code != 0 || !strings.Contains(stdout, "insert=0 rehash=0 rerender=0") {
		t.Fatalf("not idempotent after rehash; exit=%d stdout=%s", code, stdout)
	}
}

func TestE2E_VerbosePreviewListsEveryBlock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "```ipmt\nA --> B\n```\n\n```ipmt\nC --> D\n```\n")
	writeFile(t, filepath.Join(root, "docs", "b.md"), "```ipmt\nE --> F\n```\n")

	// Read-only verbose preview of a fresh tree — what a first-time user sees.
	stdout, _, code := e2eRun(t, "--root", root, "--check", "--verbose")
	if code != 1 {
		t.Fatalf("expected --check exit 1 (stale), got %d", code)
	}
	lines := strings.Count(stdout, "INSERT ")
	if lines != 3 {
		t.Fatalf("expected 3 INSERT lines, got %d in:\n%s", lines, stdout)
	}
	if !strings.Contains(stdout, "a.md:") || !strings.Contains(stdout, "docs/b.md:") {
		t.Fatalf("verbose output missing expected file references:\n%s", stdout)
	}
	if !strings.Contains(stdout, "id=100") || !strings.Contains(stdout, "id=110") {
		t.Fatalf("verbose output missing expected IDs:\n%s", stdout)
	}

	// --quiet wins over --verbose: no per-block lines.
	stdout, _, _ = e2eRun(t, "--root", root, "--check", "--verbose", "--quiet")
	if strings.Contains(stdout, "INSERT") {
		t.Fatalf("--quiet should suppress verbose output; stdout=%s", stdout)
	}
}

func TestE2E_Include_InsertMarkerAndRender(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "docs", "x.md")
	ipmtPath := filepath.Join(root, "docs", "swap.ipmt")
	writeFile(t, ipmtPath, "A --> B\n")
	src := strings.Join([]string{
		"# title",
		"",
		"<!-- ipm-include src=./swap.ipmt -->",
		"",
		"trailing",
	}, "\n") + "\n"
	writeFile(t, mdPath, src)

	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("write exit=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "insert=1") {
		t.Fatalf("expected insert=1; stdout=%s", stdout)
	}

	mdOut := readFile(t, mdPath)
	// ID should default to the .ipmt filename basename.
	if !strings.Contains(mdOut, "<!-- ipm-svg id=swap hash=") {
		t.Fatalf("include marker not inserted with id from filename; got:\n%s", mdOut)
	}
	if !strings.Contains(mdOut, "![](../_ipm/docs/x/swap.ipm.svg)") {
		t.Fatalf("include image path wrong; got:\n%s", mdOut)
	}
	// The new marker grammar carries no source= or src= attributes.
	// (The <!-- ipm-include src=... --> line still has src=, but that's the
	// source declaration, not the marker.)
	markerStart := strings.Index(mdOut, "<!-- ipm-svg ")
	markerEnd := strings.Index(mdOut[markerStart:], "-->") + markerStart
	markerLine := mdOut[markerStart : markerEnd+3]
	if strings.Contains(markerLine, "source=") || strings.Contains(markerLine, "src=") {
		t.Fatalf("legacy source=/src= attributes leaked into marker line %q (full md:\n%s\n)", markerLine, mdOut)
	}

	svgPath := filepath.Join(root, "_ipm", "docs", "x", "swap.ipm.svg")
	svg := readFile(t, svgPath)
	if !strings.Contains(svg, "source-id=swap") {
		t.Fatalf("include SVG meta missing source-id=swap; got:\n%s", svg)
	}

	stdout, _, code = e2eRun(t, "--root", root)
	if code != 0 || !strings.Contains(stdout, "insert=0 rehash=0 rerender=0") {
		t.Fatalf("include not idempotent; exit=%d stdout=%s", code, stdout)
	}
}

func TestE2E_Include_RehashOnIpmtEdit(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "docs", "x.md")
	ipmtPath := filepath.Join(root, "docs", "block.ipmt")

	writeFile(t, ipmtPath, "A --> B\n")
	writeFile(t, mdPath, "# t\n\n<!-- ipm-include src=./block.ipmt -->\n")

	if _, _, code := e2eRun(t, "--root", root); code != 0 {
		t.Fatal()
	}
	mdOut := readFile(t, mdPath)
	oldHash := extractHash(t, mdOut)

	// Edit the sibling file → next run rehashes.
	writeFile(t, ipmtPath, "A --> C\n")
	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 || !strings.Contains(stdout, "rehash=1") {
		t.Fatalf("ipmt edit did not trigger rehash; exit=%d stdout=%s", code, stdout)
	}
	if extractHash(t, readFile(t, mdPath)) == oldHash {
		t.Fatalf("hash unchanged after src ipmt edit")
	}
}

func TestE2E_Include_MissingSrcFailsCheck(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "x.md")
	writeFile(t, mdPath, "<!-- ipm-include src=./missing.ipmt -->\n")

	stdout, stderr, code := e2eRun(t, "--root", root, "--check")
	if code != 1 {
		t.Fatalf("expected check exit 1 on missing src, got %d (stdout=%s stderr=%s)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "missing-src=1") {
		t.Fatalf("expected missing-src=1 in stdout; got:\n%s", stdout)
	}
	if !strings.Contains(stderr, "cannot read src=") {
		t.Fatalf("expected diagnostic about missing src in stderr; got:\n%s", stderr)
	}
}

func TestE2E_VisibleAndIncludeCoexistInOneFile(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "mix.md")
	writeFile(t, filepath.Join(root, "sibling.ipmt"), "Z --> W\n")

	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
		"<!-- ipm-include src=./sibling.ipmt -->",
	}, "\n") + "\n"
	writeFile(t, mdPath, src)

	stdout, _, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("mixed-kinds run exit=%d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "blocks=2") {
		t.Fatalf("expected blocks=2; stdout=%s", stdout)
	}
	// Both SVGs exist: visible defaults to positional id=01;
	// include defaults to filename "sibling".
	for _, name := range []string{"100.ipm.svg", "sibling.ipm.svg"} {
		p := filepath.Join(root, "_ipm", "mix", name)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing SVG %s: %v", name, err)
		}
	}
	stdout, _, code = e2eRun(t, "--root", root)
	if code != 0 || !strings.Contains(stdout, "insert=0 rehash=0 rerender=0") {
		t.Fatalf("mixed-kinds idempotence broken; exit=%d stdout=%s", code, stdout)
	}
}

func TestE2E_GitIgnoredSVGDir_emitsWarning(t *testing.T) {
	// Prereq I: when _ipm/ is .gitignore'd, md-embed must warn that GitHub
	// won't render the committed diagrams. Best-effort — uses `git
	// check-ignore` so the test needs a real git repo.
	root := t.TempDir()
	gitInit := exec.Command("git", "init", "-q", root)
	if err := gitInit.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	writeFile(t, filepath.Join(root, ".gitignore"), "_ipm/\n")
	writeFile(t, filepath.Join(root, "x.md"), "```ipmt\nA --> B\n```\n")

	stdout, stderr, code := e2eRun(t, "--root", root)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	// slog text handler emits `level=WARN msg="svg output dir is git-ignored…"`.
	if !strings.Contains(stderr, "git-ignored") {
		t.Fatalf("expected gitignore warning on stderr; got:\n%s", stderr)
	}
}

func TestE2E_GitIgnoredSVGDir_quietSuppressesWarning(t *testing.T) {
	root := t.TempDir()
	gitInit := exec.Command("git", "init", "-q", root)
	if err := gitInit.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	writeFile(t, filepath.Join(root, ".gitignore"), "_ipm/\n")
	writeFile(t, filepath.Join(root, "x.md"), "```ipmt\nA --> B\n```\n")

	_, stderr, code := e2eRun(t, "--root", root, "--quiet")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "git-ignored") {
		t.Fatalf("--quiet should suppress the gitignore warning; got:\n%s", stderr)
	}
}

func TestE2E_GitIgnoredSVGDir_silentWhenNotIgnored(t *testing.T) {
	// Control: a tracked _ipm/ produces no warning.
	root := t.TempDir()
	gitInit := exec.Command("git", "init", "-q", root)
	if err := gitInit.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	writeFile(t, filepath.Join(root, "x.md"), "```ipmt\nA --> B\n```\n")

	_, stderr, _ := e2eRun(t, "--root", root)
	if strings.Contains(stderr, "git-ignored") {
		t.Fatalf("untracked _ipm/ should not warn; got:\n%s", stderr)
	}
}

func TestE2E_GitIgnoredSVGDir_silentOutsideGitRepo(t *testing.T) {
	// Control: not a git repo at all — git check-ignore exits non-zero,
	// we stay silent.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.md"), "```ipmt\nA --> B\n```\n")

	_, stderr, _ := e2eRun(t, "--root", root)
	if strings.Contains(stderr, "git-ignored") {
		t.Fatalf("non-git tree should not warn; got:\n%s", stderr)
	}
}

func extractHash(t *testing.T, mdText string) string {
	t.Helper()
	_, rest, ok := strings.Cut(mdText, "hash=")
	if !ok {
		t.Fatalf("no hash in:\n%s", mdText)
	}
	val, _, ok := strings.Cut(rest, " ")
	if !ok {
		t.Fatal("malformed hash attr")
	}
	return val
}
