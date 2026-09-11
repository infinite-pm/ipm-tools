package mdhtml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// writeFile writes a test fixture, creating its directory.
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
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestRenderMapping_headMetadata: a globbed page one directory deep gets its
// description from the first paragraph (inline ipmt marker dropped, code span
// and link text kept), a canonical URL from site_url + its output path, the
// Open Graph / Twitter tags, and icon hrefs adjusted for its depth.
func TestRenderMapping_headMetadata(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "docs", "page.md")
	writeFile(t, src, "# Page\n\n"+
		"First <!--ipmt:as-token:e-title-->`event` paragraph with a [link](./other.md) and\n"+
		"a second line.\n\nSecond paragraph.\n")

	cfg := &Config{
		TitleTemplate: "{{title}} — Site",
		Head: HeadConfig{
			SiteURL:     "https://example.org/intro",
			SiteName:    "example.org",
			Image:       "https://example.org/og.png",
			ImageWidth:  1200,
			ImageHeight: 628,
			Icons: []IconLink{
				{Href: "../favicon.svg", Type: "image/svg+xml"},
				{Rel: "apple-touch-icon", Href: "https://example.org/touch.png"},
			},
		},
	}
	out := filepath.Join(dir, "_site", "docs", "page.html")
	res, err := RenderMapping(cfg, ResolvedMapping{
		SrcAbs: src, OutAbs: out, SrcRel: "docs/page.md", OutRel: "docs/page.html",
	})
	if err != nil {
		t.Fatalf("RenderMapping: %v", err)
	}
	const desc = "First event paragraph with a link and a second line."
	if res.Description != desc {
		t.Errorf("Description = %q, want %q", res.Description, desc)
	}

	html := readFile(t, out)
	for _, want := range []string{
		`<title>Page — Site</title>`,
		`<meta name="description" content="` + desc + `">`,
		`<link rel="canonical" href="https://example.org/intro/docs/page.html">`,
		`<meta property="og:type" content="website">`,
		`<meta property="og:site_name" content="example.org">`,
		`<meta property="og:url" content="https://example.org/intro/docs/page.html">`,
		`<meta property="og:title" content="Page — Site">`,
		`<meta property="og:description" content="` + desc + `">`,
		`<meta property="og:image" content="https://example.org/og.png">`,
		`<meta property="og:image:width" content="1200">`,
		`<meta property="og:image:height" content="628">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<meta name="twitter:image" content="https://example.org/og.png">`,
		`<link rel="icon" href="../../favicon.svg" type="image/svg+xml">`,
		`<link rel="apple-touch-icon" href="https://example.org/touch.png">`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestRenderMapping_fileOverridesAndIndexURL: a file entry's title is used as
// given (no title_template), its description wins over the first paragraph,
// and index.html is addressed as the directory.
func TestRenderMapping_fileOverridesAndIndexURL(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "README.md")
	writeFile(t, src, "# Name\n\nAuto description.\n")

	cfg := &Config{
		TitleTemplate: "{{title}} — Site",
		Head:          HeadConfig{SiteURL: "https://example.org/intro/"},
	}
	out := filepath.Join(dir, "_site", "index.html")
	if _, err := RenderMapping(cfg, ResolvedMapping{
		SrcAbs: src, OutAbs: out, SrcRel: "README.md", OutRel: "index.html",
		Title: "Custom title", Description: "Custom description.",
	}); err != nil {
		t.Fatalf("RenderMapping: %v", err)
	}

	html := readFile(t, out)
	for _, want := range []string{
		`<title>Custom title</title>`,
		`<meta name="description" content="Custom description.">`,
		`<link rel="canonical" href="https://example.org/intro/">`,
		`<meta property="og:title" content="Custom title">`,
		`<meta name="twitter:card" content="summary">`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
		}
	}
	for _, unwanted := range []string{"Auto description.\">", "og:image", "Name — Site"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("output unexpectedly contains %q", unwanted)
		}
	}
}

// TestRenderPage_noHeadConfig: without a head config a page gains only its
// description - no canonical, Open Graph, Twitter or icon tags - and images
// scale without distorting.
func TestRenderPage_noHeadConfig(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.md")
	writeFile(t, src, "# T\n\nHello world.\n")
	out := filepath.Join(dir, "_site", "in.html")
	if _, err := RenderPage(&Config{}, src, out, "in.md"); err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	html := readFile(t, out)
	if !strings.Contains(html, `<meta name="description" content="Hello world.">`) {
		t.Errorf("description meta missing")
	}
	if !strings.Contains(html, `img { max-width: 100%; height: auto; }`) {
		t.Errorf("img rule should keep the aspect ratio (height: auto)")
	}
	for _, unwanted := range []string{"canonical", "og:", "twitter:", `rel="icon"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("output unexpectedly contains %q", unwanted)
		}
	}
}

// TestRenderPage_diagramAltBecomesAccessibleName: the alt text of an embed's
// markdown image becomes the inlined <svg>'s accessible name.
func TestRenderPage_diagramAltBecomesAccessibleName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.md")
	writeFile(t, src, "# T\n\n```ipmt\na ::e --> b ::e\n```\n"+
		"<!-- ipm-svg id=01 hash=ab12cd34 -->\n"+
		"![Two events, a leads to b](./d.svg)\n")
	writeFile(t, filepath.Join(dir, "d.svg"), `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>`)
	out := filepath.Join(dir, "_site", "in.html")
	if _, err := RenderPage(&Config{}, src, out, "in.md"); err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	want := `<figure class="ipm-diagram"><svg role="img" aria-label="Two events, a leads to b" xmlns=`
	if html := readFile(t, out); !strings.Contains(html, want) {
		t.Errorf("output missing %q", want)
	}
}

func TestLabelSVG(t *testing.T) {
	const svg = `<svg xmlns="x"><title>edge</title></svg>`
	cases := []struct {
		name, svg, alt, want string
	}{
		{"labelled", svg, "A diagram", `<svg role="img" aria-label="A diagram" xmlns="x"><title>edge</title></svg>`},
		{"escaped", svg, `say "hi" & <go>`, `<svg role="img" aria-label="say &quot;hi&quot; &amp; &lt;go&gt;" xmlns="x"><title>edge</title></svg>`},
		{"no alt", svg, "  ", svg},
		{"already has a role", `<svg role="img" xmlns="x"></svg>`, "A", `<svg role="img" xmlns="x"></svg>`},
		{"already labelled", `<svg aria-label="B" xmlns="x"></svg>`, "A", `<svg aria-label="B" xmlns="x"></svg>`},
		{"not an svg", `<!-- c --><svg></svg>`, "A", `<!-- c --><svg></svg>`},
	}
	for _, c := range cases {
		if got := labelSVG(c.svg, c.alt); got != c.want {
			t.Errorf("%s: labelSVG = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestClipDescription(t *testing.T) {
	if got := clipDescription("short enough", 160); got != "short enough" {
		t.Errorf("short text changed: %q", got)
	}
	long := strings.Repeat("word ", 50) + "γ(3,4) tail"
	got := clipDescription(long, 40)
	if utf8.RuneCountInString(got) > 40 {
		t.Errorf("clipped to %d runes, want <= 40: %q", utf8.RuneCountInString(got), got)
	}
	if !strings.HasSuffix(got, "word…") {
		t.Errorf("want a cut at a word boundary ending in …, got %q", got)
	}
}

func TestPageURL(t *testing.T) {
	cases := []struct{ site, rel, want string }{
		{"https://x.org/intro", "index.html", "https://x.org/intro/"},
		{"https://x.org/intro/", "docs/a.html", "https://x.org/intro/docs/a.html"},
		{"https://x.org/", "sub/index.html", "https://x.org/sub/"},
	}
	for _, c := range cases {
		if got := pageURL(c.site, c.rel); got != c.want {
			t.Errorf("pageURL(%q, %q) = %q, want %q", c.site, c.rel, got, c.want)
		}
	}
}

// TestResolveMappings_outRelAndOverrides: both mapping kinds carry their
// output path relative to out_dir; only file entries carry overrides.
func TestResolveMappings_outRelAndOverrides(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "# R\n")
	writeFile(t, filepath.Join(dir, "docs", "a.md"), "# A\n")
	cfg := &Config{
		OutDir: "_site",
		Files:  []FileMapping{{Src: "README.md", Out: "index.html", Title: "T", Description: "D"}},
		Globs:  []GlobMapping{{Src: "docs/**/*.md"}},
	}
	ms, err := ResolveMappings(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ResolvedMapping{}
	for _, m := range ms {
		got[m.SrcRel] = m
	}
	if r := got["README.md"]; r.OutRel != "index.html" || r.Title != "T" || r.Description != "D" {
		t.Errorf("README.md mapping = %+v", r)
	}
	if a := got[filepath.Join("docs", "a.md")]; a.OutRel != "docs/a.html" || a.Title != "" || a.Description != "" {
		t.Errorf("docs/a.md mapping = %+v", a)
	}
}
