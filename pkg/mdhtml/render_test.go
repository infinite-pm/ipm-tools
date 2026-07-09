package mdhtml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderPage_endToEnd exercises the full pipeline: ipmt fenced block
// → highlighted code, `<!-- ipm-svg --> ![](…)` marker → inlined SVG,
// `[link](./foo.md)` → `./foo.html`, `![alt](./img.png)` → copied to
// out_dir. Keeps the inputs small so a regression in any one substep
// produces a clear, focused failure.
func TestRenderPage_endToEnd(t *testing.T) {
	dir := t.TempDir()

	// Source .md with all four interesting shapes.
	srcMD := dir + "/in.md"
	const md = "# Title\n" +
		"\n" +
		"See [next page](./other.md#sec).\n" +
		"\n" +
		"```ipmt\n" +
		"bob ::t --> Alice ::e\n" +
		"```\n" +
		"<!-- ipm-svg id=01 hash=ab12cd34 -->\n" +
		"![diagram](./diag.svg)\n" +
		"\n" +
		"![screenshot](./shot.png)\n"
	if err := os.WriteFile(srcMD, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inlined SVG: minimal but with a prolog + DOCTYPE so the stripper
	// gets exercised.
	const svgBody = `<?xml version="1.0"?>
<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">
<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><circle r="5"/></svg>`
	if err := os.WriteFile(dir+"/diag.svg", []byte(svgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/shot.png", []byte("not-really-png"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		OutDir: dir + "/_site",
		Header: HeaderConfig{
			HomeURL: "https://infinite.pm/",
			Brand:   "infinite.pm",
			Menu:    []MenuLink{{Label: "lab", URL: "https://lab.infinite.pm"}},
		},
		Footer: FooterConfig{
			Text:        "© infinite.pm",
			Repo:        "https://github.com/x/y",
			Branch:      "main",
			SourceLabel: "GitHub",
		},
	}

	outHTML := dir + "/_site/in.html"
	res, err := RenderPage(cfg, srcMD, outHTML, "in.md")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if res.IpmtBlocks != 1 {
		t.Errorf("IpmtBlocks = %d, want 1", res.IpmtBlocks)
	}
	if res.SVGEmbeds != 1 {
		t.Errorf("SVGEmbeds = %d, want 1", res.SVGEmbeds)
	}

	raw, err := os.ReadFile(outHTML)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	html := string(raw)

	checks := []string{
		`<title>Title</title>`,
		`<header class="top"><a class="brand" href="https://infinite.pm/">`,                     // brand → main site
		`<span class="inf">infinite</span><span class="dot">.</span><span class="pm">pm</span>`, // coloured brand
		`<nav class="top-links"><a href="https://lab.infinite.pm">lab</a></nav>`,                // header menu
		`<footer class="site"><div class="frow"><span>© infinite.pm</span>`,                     // footer tagline
		`<a class="gh" href="https://github.com/x/y/blob/main/in.md" target="_blank"`,           // footer "view source"
		`<a href="./other.html#sec">next page</a>`,                                              // .md → .html, fragment preserved
		`<figure class="ipm-diagram"><svg`,                                                      // inlined SVG (XML prolog stripped)
		`<pre class="ipmt-block"><button class="ipmt-copy"`,                                     // ipmt code wrapper + copy button
		`<code class="language-ipmt">`,                                                          // ipmt code element
		`<span class="ipm-e-title">Alice</span>`,                                                // highlighted token
		`<img src="./shot.png"`,                                                                 // image kept as relative link
	}
	for _, c := range checks {
		if !strings.Contains(html, c) {
			t.Errorf("output missing substring %q", c)
		}
	}
	if strings.Contains(html, "<?xml") {
		t.Errorf("XML prolog leaked into inlined SVG")
	}
	if strings.Contains(html, "<!DOCTYPE svg") {
		t.Errorf("SVG DOCTYPE leaked into output")
	}

	// Image asset was copied alongside the output.
	if _, err := os.Stat(filepath.Join(dir, "_site", "shot.png")); err != nil {
		t.Errorf("expected shot.png to be copied to out_dir: %v", err)
	}
}

// TestRenderPage_inlineIpmt exercises the inline highlighting path:
// `<!--ipmt-->` before a backtick code span MUST produce a
// <code class="language-ipmt"> element with per-token spans, and the
// marker comment itself MUST be consumed (not appear in the output).
func TestRenderPage_inlineIpmt(t *testing.T) {
	dir := t.TempDir()
	srcMD := dir + "/in.md"
	const md = "# Title\n" +
		"\n" +
		"A small color taxonomy (<!--ipmt-->`t-shirt ::c`, <!--ipmt-->`black ::c`).\n"
	if err := os.WriteFile(srcMD, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{OutDir: dir + "/_site"}
	if _, err := RenderPage(cfg, srcMD, dir+"/_site/in.html", "in.md"); err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	raw, err := os.ReadFile(dir + "/_site/in.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)

	// Check only the <body> region — the embedded stylesheet contains
	// a comment that mentions `<!--ipmt-->` literally to document this
	// very rule.
	bodyStart := strings.Index(html, "<body>")
	if bodyStart < 0 {
		t.Fatalf("no <body> in output: %q", html)
	}
	body := html[bodyStart:]
	if strings.Contains(body, "<!--ipmt-->") || strings.Contains(body, "<!-- ipmt -->") {
		t.Errorf("marker comment leaked into output body: %q", body)
	}
	for _, want := range []string{
		`<code class="language-ipmt">`,
		// Identifier: concept (blue) → ipm-c-title.
		`<span class="ipm-c-title">t-shirt</span>`,
		`<span class="ipm-c-title">black</span>`,
		// `::c` marker: bold variant → ipm-c-marker.
		`<span class="ipm-c-marker">::c</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestRenderPage_inlineIpmtAsToken exercises `<!--ipmt:as-token:NAME-->`:
// arbitrary text (here "(orange)", not valid ipmt) is painted in the
// named style as ONE span, and the marker comment is consumed.
func TestRenderPage_inlineIpmtAsToken(t *testing.T) {
	dir := t.TempDir()
	srcMD := dir + "/in.md"
	const md = "# Title\n" +
		"\n" +
		"The event color is <!--ipmt:as-token:e-marker-->`(orange)`; the " +
		"<!--ipmt:as-token:relation-->`::P` letter is purple.\n"
	if err := os.WriteFile(srcMD, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{OutDir: dir + "/_site"}
	if _, err := RenderPage(cfg, srcMD, dir+"/_site/in.html", "in.md"); err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	raw, err := os.ReadFile(dir + "/_site/in.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	bodyStart := strings.Index(html, "<body>")
	body := html[bodyStart:]

	for _, want := range []string{
		`<code class="language-ipmt"><span class="ipm-e-marker">(orange)</span></code>`,
		`<code class="language-ipmt"><span class="ipm-relation">::P</span></code>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("as-token output missing %q; body:\n%s", want, body)
		}
	}
	if strings.Contains(body, "as-token") || strings.Contains(body, "<!--ipmt") {
		t.Errorf("marker leaked into output body")
	}
}

// TestRenderPage_tildeWrappedExampleStaysLiteral is the tilde-wrapper
// twin: a ```ipmt fence and a marker pair shown inside a ~~~~md example
// are literal — no ipmt highlight block, no SVG inlining.
func TestRenderPage_tildeWrappedExampleStaysLiteral(t *testing.T) {
	dir := t.TempDir()
	srcMD := dir + "/in.md"
	const md = "# Title\n" +
		"\n" +
		"~~~~md\n" +
		"```ipmt unresolved\n" +
		"deploy ::?etc --::X--> safety ::?etc\n" +
		"```\n" +
		"<!-- ipm-svg id=01 hash=ab12cd34 -->\n" +
		"![diagram](./diag.svg)\n" +
		"~~~~\n"
	if err := os.WriteFile(srcMD, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{OutDir: dir + "/_site"}
	outHTML := dir + "/_site/in.html"
	res, err := RenderPage(cfg, srcMD, outHTML, "in.md")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if res.IpmtBlocks != 0 {
		t.Errorf("IpmtBlocks = %d, want 0 (tilde-wrapped example is literal)", res.IpmtBlocks)
	}
	if res.SVGEmbeds != 0 {
		t.Errorf("SVGEmbeds = %d, want 0 (marker in tilde example is literal)", res.SVGEmbeds)
	}
}

// TestRenderPage_nestedExampleStaysLiteral pins the fence-nesting rule in
// renderBody: a ```ipmt fence and a marker pair shown inside a ````md
// documentation example (the docs/ipmt-unresolved.md pattern) are literal
// — no ipmt highlight block, no SVG inlining, and the example's text
// survives into the published HTML.
func TestRenderPage_nestedExampleStaysLiteral(t *testing.T) {
	dir := t.TempDir()
	srcMD := dir + "/in.md"
	const md = "# Title\n" +
		"\n" +
		"````md\n" +
		"```ipmt unresolved\n" +
		"deploy ::?etc --::X--> safety ::?etc\n" +
		"```\n" +
		"<!-- ipm-svg id=01 hash=ab12cd34 -->\n" +
		"![diagram](./diag.svg)\n" +
		"````\n"
	if err := os.WriteFile(srcMD, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{OutDir: dir + "/_site"}
	outHTML := dir + "/_site/in.html"
	res, err := RenderPage(cfg, srcMD, outHTML, "in.md")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if res.IpmtBlocks != 0 {
		t.Errorf("IpmtBlocks = %d, want 0 (example is literal)", res.IpmtBlocks)
	}
	if res.SVGEmbeds != 0 {
		t.Errorf("SVGEmbeds = %d, want 0 (marker in example is literal)", res.SVGEmbeds)
	}
	raw, err := os.ReadFile(outHTML)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	if !strings.Contains(html, "```ipmt unresolved") {
		t.Error("published HTML must show the example's inner fence verbatim")
	}
	if !strings.Contains(html, "ipm-svg id=01") {
		t.Error("published HTML must show the example's marker verbatim")
	}
}
