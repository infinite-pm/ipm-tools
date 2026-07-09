package mdhtml

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGlobMd_absoluteDoubleStar guards the absolute-`**` rooting fix:
// a glob whose prefix is an absolute path must root the walk at that
// absolute prefix, not re-anchor it under configDir.
func TestGlobMd_absoluteDoubleStar(t *testing.T) {
	// Lay out an absolute docs tree somewhere other than configDir.
	docs := t.TempDir()
	sub := filepath.Join(docs, "guide")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(sub, "a.md")
	if err := os.WriteFile(want, []byte("# a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "b.md"), []byte("# b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	configDir := t.TempDir() // unrelated config dir
	pattern := filepath.Join(docs, "**", "*.md")

	got, err := globMd(configDir, pattern)
	if err != nil {
		t.Fatalf("globMd: %v", err)
	}
	found := map[string]bool{}
	for _, g := range got {
		found[g] = true
	}
	if !found[want] {
		t.Errorf("absolute ** glob did not find %q; got %v", want, got)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 matches under absolute prefix, got %d: %v", len(got), got)
	}
}

// TestResolveMappings_absoluteFileSrcRel guards the File-mapping SrcRel fix:
// an absolute `src` must yield a config-relative SrcRel (matching the Globs
// branch), not a raw absolute path that would corrupt the GitHub source URL.
func TestResolveMappings_absoluteFileSrcRel(t *testing.T) {
	configDir := t.TempDir()
	srcAbs := filepath.Join(configDir, "docs", "page.md")
	if err := os.MkdirAll(filepath.Dir(srcAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcAbs, []byte("# p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		OutDir: filepath.Join(configDir, "_site"),
		Files:  []FileMapping{{Src: srcAbs, Out: "page.html"}},
	}
	got, err := ResolveMappings(cfg, configDir)
	if err != nil {
		t.Fatalf("ResolveMappings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(got))
	}
	want := filepath.Join("docs", "page.md")
	if got[0].SrcRel != want {
		t.Errorf("SrcRel = %q, want %q (config-relative, not absolute)", got[0].SrcRel, want)
	}
	if filepath.IsAbs(got[0].SrcRel) {
		t.Errorf("SrcRel must not be absolute: %q", got[0].SrcRel)
	}
}

// TestResolveMappings_relativeFileSrcRel confirms the common relative-src
// case is unchanged: SrcRel equals the configured src path.
func TestResolveMappings_relativeFileSrcRel(t *testing.T) {
	configDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configDir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "docs", "page.md"), []byte("# p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		OutDir: filepath.Join(configDir, "_site"),
		Files:  []FileMapping{{Src: filepath.Join("docs", "page.md"), Out: "page.html"}},
	}
	got, err := ResolveMappings(cfg, configDir)
	if err != nil {
		t.Fatalf("ResolveMappings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(got))
	}
	if want := filepath.Join("docs", "page.md"); got[0].SrcRel != want {
		t.Errorf("SrcRel = %q, want %q", got[0].SrcRel, want)
	}
}
