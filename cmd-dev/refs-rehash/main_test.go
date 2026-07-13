package main

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/filterutil"
)

func sha1Of(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func writeTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"one-event.ipmt":        "A ::e\n",
		"one-event.json":        "{\"nodes\":[]}\n",
		"one-event.layout.json": "{\"version\":\"1\"}\n",
		"one-event.ipm.svg":     "<svg/>\n",
		"one-event.md":          "# one event\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refs := `[
    {
        "source": "../../docs/dev/layout-gen/layout-alg.md",
        "source_sha1": "keep-me-untouched",
        "items": [
            {
                "destination": "one-event",
                "content_sha1": "keep-me-too",
                "outputs": {
                    "ipmt": "stale0000",
                    "json": "stale1111",
                    "layout.json": "stale2222",
                    "ipm.svg": "stale3333",
                    "md": "stale4444"
                }
            }
        ]
    }
]
`
	if err := os.WriteFile(filepath.Join(dir, "_refs.json"), []byte(refs), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRehashDirRefreshesStaleOutputs(t *testing.T) {
	dir := writeTestDir(t)

	stale, err := rehashDir(dir, false, false)
	if err != nil {
		t.Fatalf("rehashDir: %v", err)
	}
	if !stale {
		t.Fatal("expected stale outputs to be detected")
	}

	refs, err := filterutil.LoadRefs(dir)
	if err != nil {
		t.Fatalf("reload refs: %v", err)
	}
	item := refs[0].Items[0]
	want := map[string]string{
		"ipmt":        sha1Of("A ::e\n"),
		"json":        sha1Of("{\"nodes\":[]}\n"),
		"layout.json": sha1Of("{\"version\":\"1\"}\n"),
		"ipm.svg":     sha1Of("<svg/>\n"),
		"md":          sha1Of("# one event\n"),
	}
	for k, v := range want {
		if item.Outputs[k] != v {
			t.Errorf("outputs[%q] = %q, want %q", k, item.Outputs[k], v)
		}
	}
	if refs[0].SourceSHA1 != "keep-me-untouched" {
		t.Errorf("source_sha1 was touched: %q", refs[0].SourceSHA1)
	}
	if item.ContentSHA1 != "keep-me-too" {
		t.Errorf("content_sha1 was touched: %q", item.ContentSHA1)
	}

	// Second run must be a no-op.
	stale, err = rehashDir(dir, false, false)
	if err != nil {
		t.Fatalf("rehashDir second run: %v", err)
	}
	if stale {
		t.Error("second run still reported stale outputs")
	}
}

func TestRehashDirCheckDoesNotWrite(t *testing.T) {
	dir := writeTestDir(t)
	before, err := os.ReadFile(filepath.Join(dir, "_refs.json"))
	if err != nil {
		t.Fatal(err)
	}

	stale, err := rehashDir(dir, true, false)
	if err != nil {
		t.Fatalf("rehashDir --check: %v", err)
	}
	if !stale {
		t.Fatal("expected --check to report stale outputs")
	}

	after, err := os.ReadFile(filepath.Join(dir, "_refs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("--check modified _refs.json")
	}
}

func TestRehashItemDropsMissingAndAddsNew(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "case.ipmt"), []byte("A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "case.ipm.svg"), []byte("<svg/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := map[string]string{
		"ipmt":        "stale",
		"layout.json": "file-is-gone",
	}
	fresh := rehashItem(dir, "case", old)

	if _, ok := fresh["layout.json"]; ok {
		t.Error("missing layout.json file should drop its key")
	}
	if fresh["ipm.svg"] != sha1Of("<svg/>\n") {
		t.Errorf("newly present ipm.svg not picked up: %v", fresh)
	}
	if fresh["ipmt"] != sha1Of("A\n") {
		t.Errorf("ipmt extension key not rehashed: %v", fresh)
	}
}

func TestWriteJSONAtomicMatchesSyncFormatting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "_refs.json")
	if err := writeJSONAtomic(path, []map[string]string{{"a": "b"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasSuffix(s, "\n") {
		t.Error("output must end with a newline (json.Encoder convention)")
	}
	if !strings.Contains(s, "    \"a\": \"b\"") {
		t.Errorf("output must use 4-space indent like sync-test-cases, got:\n%s", s)
	}
}
