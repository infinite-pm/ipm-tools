// Package mdembed — srcsvg.go
//
// RenderSourceSVG converts ipmt source text into a syntax-coloured SVG, for
// embedding in Markdown rendered by hosts that do not natively highlight the
// `ipmt` language (notably GitHub). The output is a self-contained SVG with
// inline fill colours, no external CSS, no external fonts — same conventions
// as the diagram-SVG output produced by the rest of this package.
//
// Token classes mirror the four scopes declared in the VS Code extension's
// tmLanguage grammar (vscode-ipm/syntaxes/ipmt.tmLanguage.json):
//
//	comment.line.number-sign.ipmt  -> srcTokComment
//	string.quoted.double.ipmt      -> srcTokString
//	keyword.operator.arrow.ipmt    -> srcTokArrow      (incl. --::P--, --::L--, --::X--, --::N--)
//	storage.type.node.ipmt         -> srcTokTypeMarker (::e / ::t / ::c / ::a / ::tip)
//	(default node text)            -> srcTokDefault
package mdembed

import (
	"fmt"
	"regexp"
	"strings"
)

// srcTokKind classifies a token for syntax-colouring purposes.
type srcTokKind int

const (
	srcTokDefault srcTokKind = iota
	srcTokComment
	srcTokString
	srcTokArrow
	srcTokTypeMarker
)

// srcTok is a piece of a single source line ready to emit as one <tspan>.
type srcTok struct {
	Text string
	Kind srcTokKind
}

// srcColors maps each token class to a hex colour suitable for both light
// and (most) dark GitHub backgrounds. Light-leaning palette: GitHub renders
// READMEs with a near-white background by default, so colours need to read
// against white. Reasonable contrast on GitHub Dark too, where the
// background is dark grey rather than black.
var srcColors = map[srcTokKind]string{
	srcTokDefault:    "#24292f", // GitHub default text
	srcTokComment:    "#6e7781", // muted grey
	srcTokString:     "#0a3069", // navy
	srcTokArrow:      "#cf222e", // red (arrows / operators)
	srcTokTypeMarker: "#8250df", // purple (::e ::t ::c ::a ::tip and ::P ::L ::X ::N)
}

// Patterns mirroring the tmLanguage grammar. Order matters: a longer / more
// specific match must be tried first.
var (
	// Explicit-relation arrow:  --::P-->, --::L-->, --::X-->, --::N--
	srcReExplicitArrow = regexp.MustCompile(`^(--::[PLXN]--[>]?)`)
	// Forward/reverse arrow:    -->, <--
	srcReArrow = regexp.MustCompile(`^(-->|<--)`)
	// Near-to (undirected):     --- (exactly three dashes)
	srcReNearTo = regexp.MustCompile(`^(---)`)
	// Type / alias / tooltip marker: ::e ::t ::c ::a ::tip
	srcReTypeMarker = regexp.MustCompile(`^(::(?:tip|[etca]))\b`)
)

// tokenizeSourceLine breaks a single source line into colour-tagged tokens.
// Comments and strings short-circuit the rest of the scan because the
// tmLanguage grammar treats them as exclusive spans.
func tokenizeSourceLine(line string) []srcTok {
	// Whole-line comment: leading whitespace, then '#' to EOL.
	if t := strings.TrimLeft(line, " \t"); strings.HasPrefix(t, "#") {
		// Preserve the leading whitespace as a default-coloured run so
		// indentation is visible.
		lead := line[:len(line)-len(t)]
		toks := []srcTok{}
		if lead != "" {
			toks = append(toks, srcTok{Text: lead, Kind: srcTokDefault})
		}
		toks = append(toks, srcTok{Text: t, Kind: srcTokComment})
		return toks
	}

	var out []srcTok
	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		out = append(out, srcTok{Text: buf.String(), Kind: srcTokDefault})
		buf.Reset()
	}

	i := 0
	for i < len(line) {
		c := line[i]

		// Strings: "..." (may contain backslash-escapes).
		if c == '"' {
			flush()
			start := i
			j := i + 1
			for j < len(line) {
				if line[j] == '\\' && j+1 < len(line) {
					j += 2
					continue
				}
				if line[j] == '"' {
					j++
					break
				}
				j++
			}
			out = append(out, srcTok{Text: line[start:j], Kind: srcTokString})
			i = j
			continue
		}

		// Arrows — try the most-specific first.
		if loc := srcReExplicitArrow.FindStringIndex(line[i:]); loc != nil {
			flush()
			out = append(out, srcTok{Text: line[i : i+loc[1]], Kind: srcTokArrow})
			i += loc[1]
			continue
		}
		if loc := srcReArrow.FindStringIndex(line[i:]); loc != nil {
			flush()
			out = append(out, srcTok{Text: line[i : i+loc[1]], Kind: srcTokArrow})
			i += loc[1]
			continue
		}
		if loc := srcReNearTo.FindStringIndex(line[i:]); loc != nil {
			flush()
			out = append(out, srcTok{Text: line[i : i+loc[1]], Kind: srcTokArrow})
			i += loc[1]
			continue
		}

		// Type marker: ::e / ::t / ::c / ::a / ::tip
		if c == ':' && i+1 < len(line) && line[i+1] == ':' {
			if loc := srcReTypeMarker.FindStringIndex(line[i:]); loc != nil {
				flush()
				out = append(out, srcTok{Text: line[i : i+loc[1]], Kind: srcTokTypeMarker})
				i += loc[1]
				continue
			}
		}

		buf.WriteByte(c)
		i++
	}
	flush()
	return out
}

// xmlEscape escapes the five chars XML requires inside element text and
// attribute values.
func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// RenderSourceSVG produces a colour-syntax-highlighted SVG of the given ipmt
// source. The SVG is self-contained: a single <svg> root with a background
// <rect>, one <text> element per source line, one <tspan> per token. Width
// and height are computed from the longest line and the line count using a
// fixed monospace metric (8 px advance, 18 px line height).
func RenderSourceSVG(content string) ([]byte, error) {
	// Normalize line endings, strip trailing blank lines.
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}

	const (
		charW       = 8  // px per monospace char (approx for 13px text)
		lineH       = 18 // px per line
		padX        = 12 // left/right padding
		padY        = 10 // top/bottom padding
		fontSize    = 13
		fontFamily  = `ui-monospace, "SF Mono", "Cascadia Mono", "DejaVu Sans Mono", Menlo, Consolas, monospace`
		bgColor     = "#ffffff"
		borderColor = "#d0d7de"
	)

	// Compute width from the widest line.
	maxLen := 0
	tokensByLine := make([][]srcTok, len(lines))
	for i, line := range lines {
		// Replace tabs with 4 spaces for layout (the SVG text will use the
		// expanded form too, so visual width matches advance calculation).
		line = strings.ReplaceAll(line, "\t", "    ")
		lines[i] = line
		tokensByLine[i] = tokenizeSourceLine(line)
		if l := len([]rune(line)); l > maxLen {
			maxLen = l
		}
	}
	if maxLen == 0 {
		maxLen = 1
	}

	width := padX*2 + maxLen*charW
	height := padY*2 + len(lines)*lineH

	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`+"\n",
		width, height, width, height)
	// Background panel (rounded corners, GitHub-style soft border).
	fmt.Fprintf(&b, `  <rect x="0" y="0" width="%d" height="%d" rx="6" ry="6" fill="%s" stroke="%s" stroke-width="1"/>`+"\n",
		width, height, bgColor, borderColor)

	// One <text> per source line.
	for i, toks := range tokensByLine {
		y := padY + (i+1)*lineH - 4 // baseline
		fmt.Fprintf(&b, `  <text x="%d" y="%d" font-family='%s' font-size="%d" xml:space="preserve">`,
			padX, y, fontFamily, fontSize)
		for _, t := range toks {
			col := srcColors[t.Kind]
			fmt.Fprintf(&b, `<tspan fill="%s">%s</tspan>`, col, xmlEscape(t.Text))
		}
		fmt.Fprint(&b, "</text>\n")
	}

	fmt.Fprint(&b, `</svg>`+"\n")
	return []byte(b.String()), nil
}
