package mdhtml

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// HeadConfig is what search engines and link previews read from a page's
// <head>. Every field is optional.
type HeadConfig struct {
	// SiteURL is the public URL of out_dir, e.g. "https://infinite.pm/intro/".
	// When set, each page gets a canonical link and og:url: SiteURL plus the
	// page's output path, with a trailing "index.html" dropped.
	SiteURL string `json:"site_url,omitempty"`
	// SiteName fills og:site_name.
	SiteName string `json:"site_name,omitempty"`
	// Description is the fallback meta description, for a page with neither a
	// file-entry description nor a first paragraph to take one from.
	Description string `json:"description,omitempty"`
	// Image is the absolute URL of the link-preview image (og:image,
	// twitter:image); ImageWidth and ImageHeight are its size in pixels.
	Image       string `json:"image,omitempty"`
	ImageWidth  int    `json:"image_width,omitempty"`
	ImageHeight int    `json:"image_height,omitempty"`
	// Icons are the favicon links. A relative href is given against the output
	// root and adjusted per page depth, the same way header.logo is.
	Icons []IconLink `json:"icons,omitempty"`
}

// IconLink is one icon <link> in the page head.
type IconLink struct {
	Rel   string `json:"rel,omitempty"` // default "icon"
	Href  string `json:"href"`
	Type  string `json:"type,omitempty"`  // e.g. "image/svg+xml"
	Sizes string `json:"sizes,omitempty"` // e.g. "32x32"
}

// descriptionMax bounds a description taken from a page's first paragraph:
// search results show about 155-160 characters, link previews fewer.
const descriptionMax = 160

// pageMeta is what a page's <head> needs beyond its source: a file entry's
// overrides, and where the page lands under out_dir.
type pageMeta struct {
	Title       string // replaces the computed <title> when set
	Description string // replaces the first-paragraph description when set
	OutRel      string // output path relative to out_dir; "" = no canonical URL
}

// RenderMapping renders one resolved mapping: RenderPage plus the per-page
// head metadata the mapping carries - title and description overrides, and
// the output path the canonical URL is built from.
func RenderMapping(cfg *Config, m ResolvedMapping) (*RenderResult, error) {
	return renderPage(cfg, m.SrcAbs, m.OutAbs, m.SrcRel, pageMeta{
		Title: m.Title, Description: m.Description, OutRel: m.OutRel,
	})
}

// pageTitle is a page's <title>: a file entry's title as given, else
// title_template over the H1, else the H1, else the source file name.
func pageTitle(cfg *Config, srcRel, h1, override string) string {
	if override != "" {
		return override
	}
	t := h1
	if cfg.TitleTemplate != "" {
		t = strings.ReplaceAll(cfg.TitleTemplate, "{{title}}", h1)
		t = strings.ReplaceAll(t, "{{path}}", srcRel)
	}
	if t == "" {
		t = filepath.Base(srcRel)
	}
	return t
}

// headTags renders a page's description, canonical link, Open Graph and
// Twitter card tags, and icon links. With an empty head config only the
// description is emitted, and only when the page has one.
func headTags(cfg *Config, srcRel, title, description, outRel string) string {
	h := cfg.Head
	var b strings.Builder
	meta := func(attr, key, val string) {
		if val != "" {
			fmt.Fprintf(&b, "<meta %s=\"%s\" content=\"%s\">\n", attr, key, escapeHTML(val))
		}
	}

	if description == "" {
		description = h.Description
	}
	meta("name", "description", description)

	url := ""
	if h.SiteURL != "" && outRel != "" {
		url = pageURL(h.SiteURL, outRel)
		fmt.Fprintf(&b, "<link rel=\"canonical\" href=\"%s\">\n", escapeHTML(url))
	}

	if h.SiteURL != "" || h.SiteName != "" || h.Image != "" {
		meta("property", "og:type", "website")
		meta("property", "og:site_name", h.SiteName)
		meta("property", "og:url", url)
		meta("property", "og:title", title)
		meta("property", "og:description", description)
		meta("property", "og:image", h.Image)
		if h.Image != "" && h.ImageWidth > 0 && h.ImageHeight > 0 {
			meta("property", "og:image:width", strconv.Itoa(h.ImageWidth))
			meta("property", "og:image:height", strconv.Itoa(h.ImageHeight))
		}
		card := "summary"
		if h.Image != "" {
			card = "summary_large_image"
		}
		meta("name", "twitter:card", card)
		meta("name", "twitter:image", h.Image)
	}

	for _, ic := range h.Icons {
		if ic.Href == "" {
			continue
		}
		href := ic.Href
		if !hasScheme(href) && !strings.HasPrefix(href, "/") {
			href = upTo(srcRel) + href
		}
		rel := ic.Rel
		if rel == "" {
			rel = "icon"
		}
		fmt.Fprintf(&b, "<link rel=\"%s\" href=\"%s\"", escapeHTML(rel), escapeHTML(href))
		if ic.Type != "" {
			fmt.Fprintf(&b, " type=\"%s\"", escapeHTML(ic.Type))
		}
		if ic.Sizes != "" {
			fmt.Fprintf(&b, " sizes=\"%s\"", escapeHTML(ic.Sizes))
		}
		b.WriteString(">\n")
	}
	return b.String()
}

// pageURL joins the public URL of out_dir with a page's output path. A
// directory's index.html is addressed as the directory itself.
func pageURL(siteURL, outRel string) string {
	p := filepath.ToSlash(outRel)
	if p == "index.html" {
		p = ""
	} else if strings.HasSuffix(p, "/index.html") {
		p = strings.TrimSuffix(p, "index.html")
	}
	return strings.TrimRight(siteURL, "/") + "/" + p
}

// outRelOf is outAbs relative to the output root, slash-separated; "" when it
// does not lie under it.
func outRelOf(outDirAbs, outAbs string) string {
	rel, err := filepath.Rel(outDirAbs, outAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// firstParagraphText returns the plain text of the first paragraph that has
// any: link and code-span text kept, images and raw HTML (inline ipmt markers
// included) dropped, whitespace collapsed. "" when no paragraph has text.
func firstParagraphText(doc ast.Node, source []byte) string {
	found := ""
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindParagraph {
			return ast.WalkContinue, nil
		}
		if t := inlineText(n, source); t != "" {
			found = t
			return ast.WalkStop, nil
		}
		return ast.WalkSkipChildren, nil
	})
	return found
}

// inlineText is the visible text under an inline container.
func inlineText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.RawHTML, *ast.Image:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			b.Write(t.Segment.Value(source))
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(t.Value)
		}
		return ast.WalkContinue, nil
	})
	return strings.Join(strings.Fields(b.String()), " ")
}

// clipDescription cuts s to at most max runes, at a word boundary, marking
// the cut with "…".
func clipDescription(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max-1])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:-") + "…"
}

// labelSVG gives an inlined diagram the accessible name its markdown image
// carries: `![Two events in a chain](x.svg)` puts role="img" and that
// aria-label on the <svg>, so the alt text GitHub shows on the image survives
// inlining. No alt, or an <svg> that already has a role or a label, is left
// as it is.
func labelSVG(svg, alt string) string {
	alt = strings.TrimSpace(alt)
	if alt == "" || !strings.HasPrefix(svg, "<svg") {
		return svg
	}
	open := svg
	if end := strings.IndexByte(svg, '>'); end >= 0 {
		open = svg[:end]
	}
	if strings.Contains(open, " role=") || strings.Contains(open, "aria-label=") {
		return svg
	}
	return `<svg role="img" aria-label="` + escapeHTML(alt) + `"` + svg[len("<svg"):]
}
