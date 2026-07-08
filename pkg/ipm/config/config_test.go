package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRepoRoot_findsGitDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "examples"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := FindRepoRoot(filepath.Join(root, "docs", "examples"))
	if got != root {
		t.Fatalf("FindRepoRoot from subdir = %q, want %q", got, root)
	}
}

func TestFindRepoRoot_findsGitFile(t *testing.T) {
	// `.git` as a file (git worktree / submodule) — also recognized.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ..."), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FindRepoRoot(root)
	if got != root {
		t.Fatalf("FindRepoRoot with .git file = %q, want %q", got, root)
	}
}

func TestFindRepoRoot_findsConfigBeforeGit(t *testing.T) {
	// If a config file lives at a *closer* ancestor than .git, the closer
	// ancestor wins. (Tested as: place .git at outer, conf at inner; walk
	// from a sub-subdir; expect the inner dir.)
	outer := t.TempDir()
	if err := os.Mkdir(filepath.Join(outer, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, FileName), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(inner, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got := FindRepoRoot(sub)
	if got != inner {
		t.Fatalf("FindRepoRoot = %q, want inner %q (config closer than .git)", got, inner)
	}
}

func TestFindRepoRoot_noMatchReturnsStart(t *testing.T) {
	root := t.TempDir() // no .git, no config
	got := FindRepoRoot(root)
	want, _ := filepath.Abs(root)
	if got != want {
		t.Fatalf("FindRepoRoot with no marker = %q, want %q (start unchanged)", got, want)
	}
}

func TestLoad_absent(t *testing.T) {
	root := t.TempDir()
	_, present, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("Load on missing file should return present=false")
	}
}

func TestLoad_parsesSvgDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName),
		[]byte("# header comment\nsvg-dir = docs/_diagrams  # inline comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, present, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("expected present=true")
	}
	if cfg.SVGDir == nil || *cfg.SVGDir != "docs/_diagrams" {
		t.Fatalf("SVGDir = %v, want pointer to %q", cfg.SVGDir, "docs/_diagrams")
	}
}

func TestLoad_emptyFile(t *testing.T) {
	// File exists but is empty → present=true, all fields nil.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte("# only a comment\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, present, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("present should be true even for comment-only file")
	}
	if cfg.SVGDir != nil {
		t.Fatalf("SVGDir should be nil, got %v", cfg.SVGDir)
	}
}

func TestLoad_unknownKey(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte("bogus = something\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestLoad_malformedLine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte("no equals sign here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root); err == nil {
		t.Fatal("expected error for malformed line")
	}
}

func TestLoad_malformedEmbedIgnoreGlob(t *testing.T) {
	// A pattern with an unclosed '[' is a bad glob; Load must error rather than
	// accept a pattern that would silently match nothing.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte("embed-ignore = docs/[bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root); err == nil {
		t.Fatal("expected error for malformed embed-ignore glob")
	}
}

func TestLoad_parsesEmbedIgnore(t *testing.T) {
	// Comma- and whitespace-separated patterns, plus a second accumulating line.
	root := t.TempDir()
	body := "embed-ignore = docs/md-embed.md, docs/invalid.md   docs/x.md\n" +
		"embed-ignore = docs/**/draft-*.md\n"
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docs/md-embed.md", "docs/invalid.md", "docs/x.md", "docs/**/draft-*.md"}
	if len(cfg.EmbedIgnore) != len(want) {
		t.Fatalf("EmbedIgnore = %v, want %v", cfg.EmbedIgnore, want)
	}
	for i := range want {
		if cfg.EmbedIgnore[i] != want[i] {
			t.Fatalf("EmbedIgnore[%d] = %q, want %q", i, cfg.EmbedIgnore[i], want[i])
		}
	}
}

func TestConfig_IsIgnored(t *testing.T) {
	cfg := Config{EmbedIgnore: []string{
		"docs/md-embed.md", // exact path
		"docs/*.md",        // single segment: direct children only
		"examples/**/*.md", // ** spans any depth
		"build/**",         // trailing ** soaks up everything under build/
	}}
	cases := []struct {
		rel  string
		want bool
	}{
		{"docs/md-embed.md", true},        // exact
		{"docs/how-to.md", true},          // docs/*.md
		{"docs/dev/layout.md", false},     // * does not cross "/"
		{"examples/a.md", true},           // ** matches zero segments
		{"examples/x/y/z.md", true},       // ** matches many segments
		{"examples/x/y/z.txt", false},     // suffix *.md must still match
		{"build/anything/here.svg", true}, // trailing **
		{"README.md", false},              // unmatched
	}
	for _, c := range cases {
		if got := cfg.IsIgnored(c.rel); got != c.want {
			t.Errorf("IsIgnored(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
	// OS-separated input is normalized to slashes before matching.
	if !cfg.IsIgnored(filepath.FromSlash("examples/x/y/z.md")) {
		t.Error("IsIgnored should normalize OS separators to slashes")
	}
	// No patterns → never ignored.
	if (Config{}).IsIgnored("anything.md") {
		t.Error("empty EmbedIgnore should ignore nothing")
	}
}
