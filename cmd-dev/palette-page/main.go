// Command palette-page writes one HTML page that shows every colour of the
// ipmt palette (pkg/ipmtokens/palette.json), so a change of a colour can be
// looked at before it goes anywhere.
//
// The page has three parts:
//
//   - a sample of ipmt source beside the diagram it draws, first as
//     everything white shows it (fgLight: a light editor, a page md-html
//     wrote, a figure), then, further down, as a dark editor shows it (fg on
//     #1f1f1f);
//   - the table of every rule with its two values and their contrast against
//     the background they are meant for;
//   - the colours a diagram itself uses for the three node kinds.
//
// Run from the repo root:
//
//	go run ./cmd-dev/palette-page --out temp/palette.html
//
// Docs: docs/dev-tools/palette-page.md
package main

import (
	"flag"
	"fmt"
	"html"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/ipmsvg"
	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
	"github.com/infinite-pm/ipm-tools/pkg/mdhtml"
)

const (
	darkBG  = "#1f1f1f"
	lightBG = "#ffffff"
)

// sample uses every token the palette colours. First an event (unmarked, and
// once with its optional ::e), a thing and a
// concept with no alias, then the same three kinds with one: an alias is the
// softer step of its kind, and the long title beside it keeps the kind colour.
const sample = `# no aliases
wake up --> make coffee ::e
Alice ::t --> wake up
Alice ::t --> person ::c

# with aliases
wearB::a Patrick wears black t-shirt
  --> swapT::a "Patrick swaps t-shirt" "in a blink" ::tip
pat::a Patrick the magician ::t --> wearB, swapT
hum::a human being ::c
pat --> hum

# relations spelled out, and an undecided node
black t-shirt ::t --::P--> wearB
hum --::X--> mammal ::c
swapT --- magic trick
black t-shirt --::N-- white t-shirt ::t
something ::?et --> swapT
`

// mode is one way the palette is shown.
type mode struct {
	key, title, bg, fg string
	value              func(ipmtokens.PaletteRule) string
}

// modes are in the order the page shows them: white first, because that is
// what the figures, the pages and a light editor share.
var modes = []mode{
	{"light", "Light editor, pages and figures (fgLight)", lightBG, "#3b3b3b", func(r ipmtokens.PaletteRule) string { return r.FGLight }},
	{"dark", "Dark editor (fg)", darkBG, "#cccccc", func(r ipmtokens.PaletteRule) string { return r.FG }},
}

func main() {
	out := flag.String("out", "-", "output HTML file (- for stdout)")
	flag.Parse()

	page, err := build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "palette-page: %v\n", err)
		os.Exit(2)
	}
	if *out == "-" {
		fmt.Print(page)
		return
	}
	if err := os.WriteFile(*out, []byte(page), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "palette-page: %v\n", err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "palette-page: %d rules -> %s\n", len(ipmtokens.Palette), *out)
}

func build() (string, error) {
	g, err := parser.ParseIPMTBytes([]byte(sample), "sample.ipmt")
	if err != nil {
		return "", fmt.Errorf("the sample does not parse: %w", err)
	}
	svg, err := ipmsvg.GenerateFromIPM(g)
	if err != nil {
		return "", fmt.Errorf("the sample does not draw: %w", err)
	}
	code := mdhtml.HighlightTokens(sample, ipmtokens.Collect(sample, sample, 0))

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">\n<title>ipmt palette</title>\n<style>\n")
	b.WriteString(`body { font: 15px/1.45 -apple-system, "Segoe UI", Roboto, sans-serif; margin: 24px; color: #222; background: #f3f3f3; }
h1 { font-size: 22px; } h2 { font-size: 17px; margin-top: 32px; }
p.note { max-width: 70em; color: #444; }
.row { display: flex; gap: 16px; flex-wrap: wrap; align-items: flex-start; }
.panel { border: 1px solid #c8c8c8; border-radius: 6px; overflow: hidden; background: #fff; }
.panel h3 { margin: 0; padding: 6px 12px; font-size: 13px; background: #e4e4e4; color: #222; }
.panel pre { margin: 0; padding: 12px 16px; font: 14px/1.5 "DejaVu Sans Mono", Menlo, Consolas, monospace; }
.panel .svg { padding: 12px; background: #fff; }
.panel .svg svg { max-width: 520px; height: auto; }
table { border-collapse: collapse; background: #fff; }
th, td { border: 1px solid #d0d0d0; padding: 4px 10px; text-align: left; font-size: 13px; }
th { background: #e4e4e4; }
td.sw { font: 14px "DejaVu Sans Mono", Menlo, Consolas, monospace; white-space: nowrap; }
td.low { outline: 2px solid #d00000; outline-offset: -2px; }
.chip { display: inline-block; width: 1.1em; height: 1.1em; border-radius: 3px; vertical-align: -0.2em; margin-right: 6px; border: 1px solid rgba(128,128,128,.5); }
`)
	for _, m := range modes {
		fmt.Fprintf(&b, ".%s pre { background: %s; color: %s; }\n", m.key, m.bg, m.fg)
		for _, r := range ipmtokens.Palette {
			fmt.Fprintf(&b, ".%s .ipm-%s { color: %s;%s }\n", m.key, r.Class, m.value(r), style(r))
		}
	}
	b.WriteString("</style></head><body>\n<h1>ipmt palette</h1>\n")
	b.WriteString(`<p class="note">Generated by <code>cmd-dev/palette-page</code> from <code>pkg/ipmtokens/palette.json</code>. ` +
		`Each rule has two values: <b>fg</b> for dark editor themes, and <b>fgLight</b> for every white background: ` +
		`a light editor theme, the pages md-html writes, and the figures drawn from the palette.</p>` + "\n")

	// One section per mode, each with the diagram beside it: the question is
	// always whether the source reads like the picture.
	diagram := stripXMLDecl(string(svg))
	for _, m := range modes {
		fmt.Fprintf(&b, "<h2>%s, beside its diagram</h2>\n<div class=\"row\">\n", html.EscapeString(m.title))
		fmt.Fprintf(&b, "<div class=\"panel %s\"><h3>source, on %s</h3><pre>%s</pre></div>\n", m.key, m.bg, code)
		fmt.Fprintf(&b, "<div class=\"panel\"><h3>The diagram</h3><div class=\"svg\">%s</div></div>\n</div>\n", diagram)
	}

	b.WriteString("<h2>Every rule</h2>\n<p class=\"note\">The number is the contrast of the text against its background. " +
		"A red frame marks a value below 4.5:1 (WCAG AA for text).</p>\n<table>\n<tr><th>class</th><th>token</th>")
	for _, m := range modes {
		fmt.Fprintf(&b, "<th>%s</th>", html.EscapeString(m.title))
	}
	b.WriteString("<th>style</th></tr>\n")
	for _, r := range ipmtokens.Palette {
		tok := r.Type
		if r.Mod != "" {
			tok += "." + r.Mod
		}
		fmt.Fprintf(&b, "<tr><td><code>%s</code></td><td>%s</td>", r.Class, tok)
		for _, m := range modes {
			v := m.value(r)
			c := contrast(v, m.bg)
			low := ""
			if c < 4.5 {
				low = " low"
			}
			fmt.Fprintf(&b, "<td class=\"sw%s\" style=\"background:%s;color:%s;%s\">%s Sample %.2f:1</td>", low, m.bg, v, style(r), v, c)
		}
		fmt.Fprintf(&b, "<td>%s</td></tr>\n", strings.TrimSpace(strings.ReplaceAll(style(r), ";", " ")))
	}
	b.WriteString("</table>\n")

	b.WriteString("<h2>The diagram's own colours</h2>\n<p class=\"note\">Set in <code>pkg/ipmsvg</code>, not in the palette, " +
		"and read from it here: the stroke, the fill, the colour and the weight of the text of a node of each kind. " +
		"Only an event is written bold.</p>\n<table>\n<tr><th>kind</th><th>stroke</th><th>fill</th><th>text</th><th>weight</th><th>as drawn</th></tr>\n")
	for _, k := range []struct{ name, kind string }{
		{"event", "event"}, {"thing", "thing"}, {"concept", "concept"},
		{"undecided", "unresolved"},
	} {
		fill, stroke, text, bold := ipmsvg.KindStyle(k.kind)
		weight, css := "normal", ""
		if bold {
			weight, css = "bold", "font-weight:bold;"
		}
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td>"+
			"<td><span style=\"display:inline-block;padding:6px 16px;border:2px solid %s;border-radius:4px;background:%s;color:%s;%sfont-family:Helvetica,Inter,Arial,sans-serif\">%s</span></td></tr>\n",
			k.name, chip(stroke), chip(fill), chip(text), weight, stroke, fill, text, css, k.name)
	}
	b.WriteString("</table>\n</body></html>\n")
	return b.String(), nil
}

func chip(hex string) string {
	return fmt.Sprintf("<span class=\"chip\" style=\"background:%s\"></span><code>%s</code>", hex, hex)
}

func style(r ipmtokens.PaletteRule) string {
	s := ""
	if r.Bold {
		s += " font-weight: bold;"
	}
	if r.Italic {
		s += " font-style: italic;"
	}
	return s
}

func stripXMLDecl(s string) string {
	if i := strings.Index(s, "<svg"); i > 0 {
		return s[i:]
	}
	return s
}

func luminance(hex string) float64 {
	var c [3]float64
	for i := 0; i < 3; i++ {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		s := float64(v) / 255
		if s <= 0.03928 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}
