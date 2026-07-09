package mdhtml

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	gmtext "github.com/yuin/goldmark/text"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
	"github.com/infinite-pm/ipm-tools/pkg/mdembed"
)

//go:embed palette-prelude.css
var palettePrelude string

// paletteCSS is the static prelude plus the per-token color rules composed
// from the palette source of truth (pkg/ipmtokens/palette.json).
var paletteCSS = palettePrelude +
	"\n/* GEN-START palette — composed from pkg/ipmtokens/palette.json */\n" +
	ipmtokens.PaletteCSSRules() +
	"/* GEN-END palette */\n"

// RenderResult is the per-file outcome of a render run.
type RenderResult struct {
	SrcAbs     string
	OutAbs     string
	Title      string
	IpmtBlocks int
	SVGEmbeds  int
}

// RenderPage produces the full HTML page for one markdown source.
// `srcRel` is the source path relative to the config root, used in the
// header's "View source on GitHub" link.
func RenderPage(cfg *Config, srcAbs, outAbs, srcRel string) (*RenderResult, error) {
	raw, err := os.ReadFile(srcAbs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", srcAbs, err)
	}
	text := markdown.NormalizeLF(string(raw))

	body, res, err := renderBody(cfg, text, srcAbs, outAbs)
	if err != nil {
		return nil, err
	}

	title := extractTitle(text)
	pageHTML := wrapPage(cfg, srcRel, title, body)

	if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(outAbs, []byte(pageHTML), 0o644); err != nil {
		return nil, err
	}

	res.SrcAbs = srcAbs
	res.OutAbs = outAbs
	res.Title = title
	return res, nil
}

// renderBody walks the markdown source line-by-line. ipmt fences and
// `<!-- ipm-svg --> ![](…)` marker pairs are substituted in-place with
// raw-HTML blocks (so goldmark passes them through verbatim); everything
// else is left to goldmark.
func renderBody(cfg *Config, text, srcAbs, outAbs string) (string, *RenderResult, error) {
	// Fail fast on inline ipmt markers that begin a line: CommonMark turns
	// such a line into an HTML block, so the marker + its code span leak
	// out raw (see LineStartInlineIpmt). Silent breakage in published HTML
	// is worse than a hard error that points at the offending line.
	if bad := markdown.LineStartInlineIpmt(text); len(bad) > 0 {
		return "", nil, fmt.Errorf(
			"inline ipmt marker begins line(s) %s: a line starting with `<!--ipmt…-->` is parsed as an HTML block and renders broken — put a non-marker character before the first marker on the line",
			joinInts(bad))
	}

	lines := strings.Split(text, "\n")
	processed := make([]string, 0, len(lines))
	res := &RenderResult{}

	i := 0
	for i < len(lines) {
		line := lines[i]

		fence, fenceInfo, isFence := markdown.ParseFenceOpen(line)

		// Non-ipmt fenced block (```go, ~~~text, a ````md example wrapper…):
		// copy it through verbatim. Its content is literal — an ```ipmt
		// fence or a marker pair inside it must NOT be transformed here;
		// goldmark renders the wrapper as an ordinary code block.
		if isFence && !(fence.Char == '`' && markdown.IsIPMTInfo(fenceInfo)) {
			next := markdown.SkipFence(lines, i, fence)
			processed = append(processed, lines[i:next]...)
			i = next
			continue
		}

		// ipmt fenced block?
		if isFence {
			end := -1
			for j := i + 1; j < len(lines); j++ {
				if fence.ClosedBy(lines[j]) {
					end = j
					break
				}
			}
			var content string
			if end == -1 {
				content = strings.Join(lines[i+1:], "\n")
			} else {
				content = strings.Join(lines[i+1:end], "\n")
			}
			tokens := ipmtokens.Collect(content, content, 0)
			highlightedHTML := HighlightTokens(content, tokens)
			res.IpmtBlocks++

			next := len(lines)
			if end != -1 {
				next = end + 1
			}
			// Skip blank lines between the closing fence and the marker.
			markerStart := next
			for markerStart < len(lines) && strings.TrimSpace(lines[markerStart]) == "" {
				markerStart++
			}

			svgInline := ""
			if markerStart+1 < len(lines) {
				if mk, ok := mdembed.ParseMarker(lines[markerStart], lines[markerStart+1]); ok {
					if inline, ok := readInlineSVG(srcAbs, mk.ImagePath); ok {
						svgInline = inline
						res.SVGEmbeds++
						next = markerStart + 2
					}
				}
			}

			processed = append(processed, "")
			if svgInline != "" {
				processed = append(processed, `<figure class="ipm-diagram">`+svgInline+`</figure>`)
				// Blank line between <figure> (CommonMark HTML block type 6)
				// and <pre> (type 1) — without it goldmark merges both into
				// one type-6 block, and the first blank line *inside* the
				// ipmt source closes the wrapper, re-parsing the remaining
				// ipmt lines as markdown.
				processed = append(processed, "")
			}
			processed = append(processed, `<pre class="ipmt-block"><button class="ipmt-copy" type="button" aria-label="Copy source">Copy</button><code class="language-ipmt">`+highlightedHTML+`</code></pre>`)
			processed = append(processed, "")
			i = next
			continue
		}

		// Stand-alone marker pair (no preceding ipmt fence).
		if i+1 < len(lines) {
			if mk, ok := mdembed.ParseMarker(line, lines[i+1]); ok {
				if inline, ok := readInlineSVG(srcAbs, mk.ImagePath); ok {
					res.SVGEmbeds++
					processed = append(processed, "")
					processed = append(processed, `<figure class="ipm-diagram">`+inline+`</figure>`)
					processed = append(processed, "")
					i += 2
					continue
				}
			}
		}

		processed = append(processed, line)
		i++
	}

	mdText := strings.Join(processed, "\n")
	source := []byte(mdText)
	// WithUnsafe: we synthesize the ipmt block + SVG inline as raw HTML
	// blocks above; goldmark's default "safe" mode would replace them
	// with `<!-- raw HTML omitted -->` and the body would be useless.
	// WithAutoHeadingID: produces GitHub-style stable anchors so the TOC
	// links resolve.
	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
	doc := md.Parser().Parse(gmtext.NewReader(source))

	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, source, doc); err != nil {
		return "", nil, err
	}
	body := buf.String()

	if !cfg.NoTOC {
		if toc := buildTOC(doc, source); toc != "" {
			body = insertTOC(body, toc)
		}
	}

	body = addHeadingControls(body)
	body, inlineN := highlightInlineIpmt(body)
	res.IpmtBlocks += inlineN
	out := rewriteLinksAndImages(body, srcAbs, outAbs)
	return out, res, nil
}

// inlineIpmtRE matches the two marker forms followed by the rendered
// inline `<code>…</code>` in the goldmark HTML. Goldmark passes the
// comment through verbatim (raw inline HTML); the marker may be
// separated from the code element by whitespace (newlines included).
//
// Group 1: the as-token NAME (empty for a bare `<!--ipmt-->`).
// Group 2: the (HTML-escaped) inline-code content goldmark emitted.
//
// The old `cut=`/`as=` attribute syntax does NOT match (no backward
// compatibility) — such a comment is left untouched in the output.
var inlineIpmtRE = regexp.MustCompile(`(?is)<!--\s*ipmt(?::as-token:([A-Za-z0-9-]+))?\s*-->\s*<code>(.*?)</code>`)

// highlightInlineIpmt rewrites each marker+`<code>` pair into a
// `<code class="language-ipmt">` carrying per-token <span class="ipm-…">
// markup. The marker comment is consumed. Returns the transformed body
// plus the count of fragments processed (for `--verbose`).
//
//   - bare `<!--ipmt-->` → tokenize the visible code as a valid ipmt
//     fragment (same `ipmtokens.Collect` + `HighlightTokens` pipeline as
//     fenced ```ipmt``` blocks).
//   - `<!--ipmt:as-token:NAME-->` → paint the whole visible code in the
//     style `NAME` (one synthetic token via `ipmtokens.TokensForClass`);
//     an unknown NAME renders the code uncolored.
//
// The inline-code content goldmark emits is HTML-escaped, so we unescape
// before tokenizing and re-escape inside `HighlightTokens`.
func highlightInlineIpmt(body string) (string, int) {
	count := 0
	out := inlineIpmtRE.ReplaceAllStringFunc(body, func(match string) string {
		sub := inlineIpmtRE.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		name := sub[1]
		raw := unescapeHTMLBasic(sub[2])
		var tokens []ipmtokens.Token
		if name != "" {
			// as-token: paint the whole text in the named style (unknown
			// NAME → no tokens → uncolored).
			tokens, _ = ipmtokens.TokensForClass(name, raw)
		} else {
			tokens = ipmtokens.Collect(raw, raw, 0)
		}
		highlighted := HighlightTokens(raw, tokens)
		count++
		return `<code class="language-ipmt">` + highlighted + `</code>`
	})
	return out, count
}

// joinInts formats a slice of line numbers as a comma-separated list for
// the LineStartInlineIpmt error message.
func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// unescapeHTMLBasic reverses the escapes applied to inline-code content.
// goldmark emits only `&amp;`, `&lt;`, `&gt;`, and `&quot;` for inline code
// (it leaves a literal `'` un-escaped); the `&#39;`/`&#34;` reversals below
// are defensive for content produced by other HTML producers. We need the
// raw source back so the tokenizer sees real characters; HighlightTokens
// re-escapes when it emits the per-token spans.
func unescapeHTMLBasic(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&#34;", "\"")
	s = strings.ReplaceAll(s, "&amp;", "&") // must come LAST
	return s
}

// headingRE matches an h2-h6 element that carries an `id` attribute.
// Capture 1 = tag (h2..h6); 2 = id value; 3 = inner content.
var headingRE = regexp.MustCompile(`(?s)<(h[2-6])\s+id="([^"]+)">(.*?)</h[2-6]>`)

// addHeadingControls appends a hover-revealed control cluster to every
// H2+ heading: a `#` permalink to the heading's own id, and a `↑` link
// to the top of the page. The cluster is hidden by default — see the
// `.heading-ctls` CSS in wrapPage.
func addHeadingControls(html string) string {
	return headingRE.ReplaceAllStringFunc(html, func(m string) string {
		sub := headingRE.FindStringSubmatch(m)
		tag, id, content := sub[1], sub[2], sub[3]
		ctls := `<span class="heading-ctls">` +
			`<a class="heading-ctl-anchor" href="#` + id + `" title="Permalink to this section" aria-label="Permalink">#</a>` +
			`<a class="heading-ctl-top" href="#" title="Back to top" aria-label="Back to top">↑</a>` +
			`</span>`
		return `<` + tag + ` id="` + id + `">` + content + ctls + `</` + tag + `>`
	})
}

// tocEntry is one heading collected for the table of contents.
type tocEntry struct {
	Level int
	Text  string
	ID    string
}

// buildTOC walks the AST and emits a `<nav class="toc">` block with one
// entry per H2+ heading. Returns "" if the doc has no headings deeper
// than H1 — a TOC with one item is just noise.
func buildTOC(doc ast.Node, source []byte) string {
	var entries []tocEntry
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok || h.Level < 2 {
			return ast.WalkContinue, nil
		}
		id, _ := h.AttributeString("id")
		entries = append(entries, tocEntry{
			Level: h.Level,
			Text:  string(h.Text(source)),
			ID:    bytesToString(id),
		})
		return ast.WalkSkipChildren, nil
	})
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<nav class="toc"><div class="toc-title">Contents</div><ul>`)
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		fmt.Fprintf(&b, `<li class="toc-lvl-%d"><a href="#%s">%s</a></li>`,
			e.Level, e.ID, escapeHTML(e.Text))
	}
	b.WriteString(`</ul></nav>`)
	return b.String()
}

// bytesToString coerces goldmark's []byte attribute values to a string;
// if the attribute is missing or a different type it returns "".
func bytesToString(v any) string {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return ""
}

// insertTOC drops the TOC immediately after the closing `</h1>` of the
// first H1. If the body has no H1 the TOC goes at the top so it's still
// visible.
func insertTOC(body, toc string) string {
	if i := strings.Index(body, "</h1>"); i >= 0 {
		j := i + len("</h1>")
		return body[:j] + "\n" + toc + "\n" + body[j:]
	}
	return toc + "\n" + body
}

// readInlineSVG reads the SVG file referenced by the marker, strips its
// XML prolog / DOCTYPE so it can be embedded inside an HTML document,
// and returns the inline content. Returns ok=false if the file can't be
// read — caller treats that as "no inline available, leave the image as
// a regular <img>".
func readInlineSVG(srcAbs, imagePath string) (string, bool) {
	if imagePath == "" {
		return "", false
	}
	svgAbs := imagePath
	if !filepath.IsAbs(svgAbs) {
		svgAbs = filepath.Join(filepath.Dir(srcAbs), imagePath)
	}
	raw, err := os.ReadFile(svgAbs)
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "<?xml") {
		if i := strings.Index(s, "?>"); i >= 0 {
			s = strings.TrimSpace(s[i+2:])
		}
	}
	if strings.HasPrefix(s, "<!DOCTYPE") || strings.HasPrefix(s, "<!doctype") {
		if i := strings.Index(s, ">"); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
		}
	}
	return s, true
}

var (
	hrefRE   = regexp.MustCompile(`href="([^"]*)"`)
	imgSrcRE = regexp.MustCompile(`<img\s+([^>]*?)src="([^"]*)"([^>]*)>`)
)

// rewriteLinksAndImages does two things:
//   - `[text](./foo.md#sec)` → `<a href="./foo.html#sec">` (relative .md
//     links get the extension swapped; fragments + queries preserved).
//   - `![alt](./img.png)` → copy `./img.png` to the matching relative
//     location under the output directory; URL stays as-is.
//
// Absolute URLs (with a scheme) and `data:` URIs are left alone.
func rewriteLinksAndImages(html, srcAbs, outAbs string) string {
	html = hrefRE.ReplaceAllStringFunc(html, func(m string) string {
		sub := hrefRE.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		u := sub[1]
		if hasScheme(u) {
			return m
		}
		path, suffix := splitURLSuffix(u)
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return m
		}
		path = strings.TrimSuffix(path, filepath.Ext(path)) + ".html"
		return `href="` + path + suffix + `"`
	})

	html = imgSrcRE.ReplaceAllStringFunc(html, func(m string) string {
		sub := imgSrcRE.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		u := sub[2]
		if hasScheme(u) || strings.HasPrefix(u, "data:") {
			return m
		}
		srcImg := filepath.Join(filepath.Dir(srcAbs), u)
		outImg := filepath.Join(filepath.Dir(outAbs), u)
		_ = copyFile(srcImg, outImg) // best-effort; missing image is reported by --check
		return m
	})
	return html
}

// hasScheme reports whether u is an absolute URL with a scheme (and thus
// must be left untouched). It guards against the CommonMark/RFC-3986
// ambiguity where a relative path whose first segment contains a colon —
// e.g. `a:b.md` or `C:/x/y.md` — parses with a non-empty url.Scheme. Such
// a value is a relative file path, not an external link, so a `.md` target
// must still be rewritten. We therefore treat u as scheme-bearing only when
// it has an authority (`scheme://…`, e.g. http/https) or its opaque part is
// not a relative document path we would otherwise rewrite (so `mailto:`,
// `data:`, `tel:` are still left alone, but `a:b.md` is not).
func hasScheme(u string) bool {
	pu, err := url.Parse(u)
	if err != nil {
		return false
	}
	if pu.Scheme == "" {
		return false
	}
	if strings.HasPrefix(u, pu.Scheme+"://") {
		return true // authority-based URL (http, https, ftp, …)
	}
	// Opaque `scheme:rest` form. If rest looks like a relative path with a
	// `.md` (or `.markdown`) extension, treat it as a path, not a scheme,
	// so the .md→.html rewrite still applies.
	path, _ := splitURLSuffix(u)
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".md" || ext == ".markdown" {
		return false
	}
	return true
}

func splitURLSuffix(u string) (path, suffix string) {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i], u[i:]
	}
	return u, ""
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// extractTitle returns the first ATX H1 (`# Foo`) in the source, or "" if
// none is present. Lines inside fenced code blocks are skipped so a `# `
// comment in an opening ```bash/```python block isn't mistaken for the
// page title (the goldmark-rendered <h1> respects fences too).
func extractTitle(mdText string) string {
	// Length-aware fence tracking (pkg/markdown/fence.go) — a naive
	// any-```-line toggle desyncs on nested examples (````md holding ```ipmt).
	var open *markdown.Fence
	for line := range strings.SplitSeq(mdText, "\n") {
		if open != nil {
			if open.ClosedBy(line) {
				open = nil
			}
			continue
		}
		if f, _, ok := markdown.ParseFenceOpen(line); ok {
			open = &f
			continue
		}
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(t[2:])
		}
	}
	return ""
}

// githubIcon is the small GitHub mark for the footer "view source" link
// (matches the main infinite.pm site).
const githubIcon = `<svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/></svg>`

// renderBrand splits a brand like "infinite.pm" into the coloured spans the main
// site uses (first segment green, dots muted, last segment blue).
func renderBrand(brand string) string {
	parts := strings.Split(brand, ".")
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(`<span class="dot">.</span>`)
		}
		cls := "inf"
		if i == len(parts)-1 {
			cls = "pm"
		}
		fmt.Fprintf(&b, `<span class="%s">%s</span>`, cls, escapeHTML(p))
	}
	return b.String()
}

// upTo is the "../" prefix that gets a page back to the output root. A page
// written to docs/examples/x.html is two deep, so an asset named against the
// root needs two of them — without which every page but the top one asks for a
// logo beside itself.
func upTo(srcRel string) string {
	n := strings.Count(filepath.ToSlash(filepath.Dir(srcRel)), "/")
	if filepath.Dir(srcRel) == "." {
		return ""
	}
	return strings.Repeat("../", n+1)
}

// renderMenu builds the header nav from the configured links.
func renderMenu(menu []MenuLink) string {
	if len(menu) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<nav class="top-links">`)
	for _, m := range menu {
		fmt.Fprintf(&b, `<a href="%s">%s</a>`, escapeHTML(m.URL), escapeHTML(m.Label))
	}
	b.WriteString(`</nav>`)
	return b.String()
}

func wrapPage(cfg *Config, srcRel, title, body string) string {
	titleText := title
	if cfg.TitleTemplate != "" {
		titleText = strings.ReplaceAll(cfg.TitleTemplate, "{{title}}", title)
		titleText = strings.ReplaceAll(titleText, "{{path}}", srcRel)
	}
	if titleText == "" {
		titleText = filepath.Base(srcRel)
	}
	extra := ""
	if cfg.ExtraCSS != "" {
		extra = fmt.Sprintf(`<link rel="stylesheet" href="%s">`, cfg.ExtraCSS)
	}

	brand := renderBrand(cfg.Header.Brand)
	if cfg.Header.Logo != "" {
		// the real logo, not letters coloured by dot-splitting: that can only
		// give the last segment ONE colour, so "pm" comes out all blue where the
		// logo has an orange p and a blue m
		alt := cfg.Header.LogoAlt
		if alt == "" {
			alt = cfg.Header.Brand
		}
		brand = fmt.Sprintf(`<img src="%s" alt="%s">`,
			escapeHTML(upTo(srcRel)+cfg.Header.Logo), escapeHTML(alt))
	}
	header := `<header class="top"><a class="brand" href="` + escapeHTML(cfg.Header.HomeURL) + `">` +
		brand + `</a>` + renderMenu(cfg.Header.Menu) + `</header>`

	source := ""
	if cfg.Footer.Repo != "" {
		source = fmt.Sprintf(`<a class="gh" href="%s/blob/%s/%s" target="_blank" rel="noopener noreferrer" title="View source on GitHub">%s%s</a>`,
			cfg.Footer.Repo, cfg.Footer.Branch, srcRel, githubIcon, escapeHTML(cfg.Footer.SourceLabel))
	}
	footer := ""
	if cfg.Footer.Text != "" || source != "" {
		footer = `<footer class="site"><div class="frow"><span>` + escapeHTML(cfg.Footer.Text) + `</span>` + source + `</div></footer>`
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s</title>
<style>
%s
html { font-size: 17px; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  max-width: 920px; margin: 2em auto; padding: 0 1.2em;
  color: #1f2328; line-height: 1.6;
}
h1, h2, h3, h4 { line-height: 1.25; margin-top: 1.6em; margin-bottom: 0.5em; }
h1 { font-size: 2em; border-bottom: 1px solid #e5e5e5; padding-bottom: 0.3em; }
h2 { font-size: 1.5em; border-bottom: 1px solid #eaecef; padding-bottom: 0.2em; }
h3 { font-size: 1.2em; }
h4 { font-size: 1.05em; }
p, ul, ol, blockquote, pre, table { margin: 0.8em 0; }
a { color: #0969da; text-decoration: none; }
a:hover { text-decoration: underline; }
code, kbd, samp { font-family: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace; font-size: 0.92em; }
:not(pre) > code { background: #f6f8fa; padding: 0.15em 0.4em; border-radius: 3px; }
/* Inline ipmt: drop the inline-code grey background so simultaneous
 * contrast doesn't shift the per-token colors away from how the same
 * tokens look in the editor pane / fenced blocks. */
code.language-ipmt { background: transparent; padding: 0; border-radius: 0; }
blockquote { border-left: 4px solid #d0d7de; padding: 0 1em; color: #57606a; margin-left: 0; }
:root { --orange:#ff8000; --green:#4d8a3a; --blue:#3a78c2; --muted:#57606a; --ink:#1f2328; --line:#e3e7ee; }
header.top { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 0.6em 0; margin-bottom: 1.8em; border-bottom: 1px solid var(--line); }
.brand { display: inline-flex; align-items: baseline; gap: .02em; font-weight: 800; font-size: 1.15rem; letter-spacing: -.01em; text-decoration: none; }
.brand .inf { color: var(--green); } .brand .dot { color: var(--muted); font-weight: 600; } .brand .pm { color: var(--blue); }
.brand img { height: 1.5rem; width: auto; display: block; }
nav.top-links a { color: var(--muted); text-decoration: none; font-size: 0.92rem; font-weight: 600; margin-left: 1.1rem; border-bottom: 2px solid transparent; padding-bottom: 2px; transition: color .15s, border-color .15s; }
nav.top-links a:hover { color: var(--ink); border-color: var(--orange); text-decoration: none; }
footer.site { border-top: 1px solid var(--line); margin-top: 3em; padding: 1.6em 0 2em; color: var(--muted); font-size: 0.9rem; }
footer.site .frow { display: flex; align-items: center; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
footer.site a { color: var(--muted); text-decoration: none; border-bottom: 1px solid transparent; }
footer.site a:hover { color: var(--ink); border-color: var(--orange); text-decoration: none; }
footer.site .gh { display: inline-flex; align-items: center; gap: .4rem; }
pre.ipmt-block { position: relative; padding: 0.7em 0.9em; border-radius: 4px; overflow-x: auto; background: #f6f8fa; }
.ipmt-copy {
  position: absolute; top: 0.4em; right: 0.4em;
  font-family: inherit; font-size: 0.8em;
  padding: 0.15em 0.55em; border: 1px solid #d0d7de; border-radius: 4px;
  background: #ffffff; color: #57606a; cursor: pointer;
  opacity: 0; transition: opacity 0.12s ease;
}
.ipmt-copy:hover { color: #0969da; border-color: #0969da; }
pre.ipmt-block:hover .ipmt-copy, .ipmt-copy:focus { opacity: 1; }
figure.ipm-diagram { margin: 1em 0; }
figure.ipm-diagram svg { max-width: 100%%; height: auto; }
img { max-width: 100%%; }
table { border-collapse: collapse; margin: 1em 0; }
th, td { border: 1px solid #d0d7de; padding: 0.4em 0.7em; text-align: left; vertical-align: top; }
th { background: #f6f8fa; font-weight: 600; }
nav.toc { background: #f6f8fa; border: 1px solid #d0d7de; border-radius: 6px; padding: 0.8em 1.2em; margin: 1.2em 0 2em; font-size: 0.95em; }
nav.toc .toc-title { font-weight: 600; margin-bottom: 0.3em; color: #57606a; text-transform: uppercase; font-size: 0.8em; letter-spacing: 0.05em; }
nav.toc ul { list-style: none; padding-left: 0; margin: 0; }
nav.toc li { margin: 0.15em 0; }
nav.toc li.toc-lvl-3 { padding-left: 1.4em; }
nav.toc li.toc-lvl-4 { padding-left: 2.8em; }
nav.toc li.toc-lvl-5 { padding-left: 4.2em; }
nav.toc li.toc-lvl-6 { padding-left: 5.6em; }
h2, h3, h4, h5, h6 { position: relative; }
.heading-ctls {
  display: inline-flex; gap: 0.15em; margin-left: 0.5em;
  font-size: 0.7em; font-weight: normal; vertical-align: middle;
  opacity: 0; transition: opacity 0.12s ease;
}
.heading-ctls a {
  color: #57606a; text-decoration: none;
  padding: 0 0.35em; border-radius: 3px;
}
.heading-ctls a:hover { color: #0969da; background: #eaeef2; text-decoration: none; }
h2:hover .heading-ctls, h3:hover .heading-ctls, h4:hover .heading-ctls,
h5:hover .heading-ctls, h6:hover .heading-ctls,
.heading-ctls:focus-within { opacity: 1; }
</style>
%s
</head>
<body>
%s
%s
%s
<script>
document.addEventListener('click', function (e) {
  var btn = e.target.closest && e.target.closest('.ipmt-copy');
  if (!btn) return;
  var code = btn.parentElement.querySelector('code');
  if (!code || !navigator.clipboard) return;
  navigator.clipboard.writeText(code.textContent).then(function () {
    var prev = btn.textContent;
    btn.textContent = 'Copied';
    setTimeout(function () { btn.textContent = prev; }, 1200);
  });
});
</script>
</body>
</html>
`, escapeHTML(titleText), paletteCSS, extra, header, body, footer)
}
