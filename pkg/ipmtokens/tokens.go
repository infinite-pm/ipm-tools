// Package ipmtokens runs the ipmt semantic tokenizer used by both the
// LSP server (cmd/ipm-rpc) and the md-html exporter (pkg/mdhtml).
//
// Token coordinates are 0-based UTF-16 columns, line-relative to the
// surrounding document — caller supplies `lineOffset` to position a
// block inside its containing markdown file.
//
// The token kind is a uint32 INDEX into [SemanticTokenTypes]; the LSP
// protocol uses the same indexing so the encoder in cmd/ipm-rpc can
// pass values through unmodified. Callers that need the wire-format
// string (e.g. the JSON embed-buffer response, or pkg/mdhtml which
// keys off "ipmEvent" / "ipmThing" / …) use [TypeName].
package ipmtokens

import (
	"sort"
	"unicode/utf16"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/lspos"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
)

// SemanticTokenTypes is the LSP token-type legend. Index order is part of
// the LSP wire contract — appending is OK, reordering is not.
var SemanticTokenTypes = []string{
	"ipmEvent",      // 0
	"ipmThing",      // 1
	"ipmConcept",    // 2
	"ipmRelation",   // 3  (L/P/X/N inside --::P-->)
	"ipmArrow",      // 4  (-->, <--, ---, --::X--, etc.)
	"ipmTypeMarker", // 5  (::e ::t ::c ::a ::tip)
	"ipmComment",    // 6
	"ipmString",     // 7
	"ipmTooltip",    // 8  (quoted-string tooltips on nodes and edges)
	"ipmUnresolved", // 9  (`::?` prefix and Unresolved-kind node titles)
}

// SemanticTokenModifiers is the LSP token-modifier legend. Index N here
// corresponds to bit (1<<N) in the [Token.Mods] field.
var SemanticTokenModifiers = []string{
	"alias",     // 0
	"leadsTo",   // 1
	"partOf",    // 2
	"expresses", // 3
	"nearTo",    // 4
	"hasAlias",  // 5
	"marker",    // 6
}

// Modifier bitmask helpers — bit positions mirror SemanticTokenModifiers.
const (
	ModAlias     uint32 = 1 << 0
	ModLeadsTo   uint32 = 1 << 1
	ModPartOf    uint32 = 1 << 2
	ModExpresses uint32 = 1 << 3
	ModNearTo    uint32 = 1 << 4
	ModHasAlias  uint32 = 1 << 5
	ModMarker    uint32 = 1 << 6
)

// Token type indices — mirror SemanticTokenTypes.
const (
	TypEvent      uint32 = 0
	TypThing      uint32 = 1
	TypConcept    uint32 = 2
	TypRelation   uint32 = 3
	TypArrow      uint32 = 4
	TypTypeMarker uint32 = 5
	TypComment    uint32 = 6
	TypString     uint32 = 7
	TypTooltip    uint32 = 8
	TypUnresolved uint32 = 9
)

// Token is one un-encoded semantic token. Positions are 0-based UTF-16
// columns; [Collect] adds the caller-supplied `lineOffset` so a block
// inside a markdown document carries whole-file coordinates.
type Token struct {
	Line     uint32
	StartCol uint32
	Length   uint32
	Type     uint32 // index into SemanticTokenTypes
	Mods     uint32 // bitmask matching SemanticTokenModifiers
}

// TypeName returns the wire-format string for a token-type index, or "" if
// the index is out of range.
func TypeName(idx uint32) string {
	if int(idx) < len(SemanticTokenTypes) {
		return SemanticTokenTypes[idx]
	}
	return ""
}

// TokensForClass supports the inline-ipmt `as-token:NAME` marker: it
// resolves the palette class suffix `name` (e.g. `e-marker`, `L`,
// `type-marker`) to a single synthetic token covering the whole visible
// text — `[0, utf16Len(visible))` carrying the (type, mods) that map to
// `ipm-<name>`. The visible text is NOT parsed as ipmt; only the named
// style is applied. Returns (nil, false) when `visible` is empty or
// `name` is not a known class suffix.
//
// `TokenForClass` (the name → type+mods reverse map) is defined in
// palette.go and derived from the embedded palette.json — the single
// source of truth. Code generation flows the other way:
// vscode-ipm/scripts/gen-palette.ts consumes the same palette.json to
// produce the editor's src/palette.gen.ts. Guarded by the e2e drift test.
func TokensForClass(name, visible string) ([]Token, bool) {
	if visible == "" {
		return nil, false
	}
	typ, mods, ok := TokenForClass(name)
	if !ok {
		return nil, false
	}
	return []Token{{
		Line:     0,
		StartCol: 0,
		Length:   utf16Len(visible),
		Type:     typ,
		Mods:     mods,
	}}, true
}

// utf16Len returns the number of UTF-16 code units in s.
func utf16Len(s string) uint32 {
	return uint32(len(utf16.Encode([]rune(s))))
}

// nodeTypeToTokenAny is the broader companion of nodeTypeToToken: it additionally
// returns TypUnresolved for model.Unresolved, so every emission site that
// paints "this is a node of THIS kind" (title, alias decl, alias ref, edge
// endpoint reference) renders Unresolved nodes in the neutral grey
// ipmUnresolved color. Without this, aliased / referenced Unresolved nodes
// were dropped (nodeTypeToToken returns false), making `alpha::a ::?etc`
// and any later `alpha` reference go uncolored.
func nodeTypeToTokenAny(t model.NodeType) (uint32, bool) {
	if t == model.Unresolved {
		return TypUnresolved, true
	}
	return nodeTypeToToken(t)
}

// nodeTypeToToken maps the parser's NodeType into our semantic token type
// index.
func nodeTypeToToken(t model.NodeType) (uint32, bool) {
	switch t {
	case model.Event:
		return TypEvent, true
	case model.Thing:
		return TypThing, true
	case model.Concept:
		return TypConcept, true
	}
	return 0, false
}

func relationToModifier(rel model.SstLinkType) uint32 {
	switch rel {
	case model.LeadsTo:
		return ModLeadsTo
	case model.PartOf:
		return ModPartOf
	case model.Expresses:
		return ModExpresses
	case model.NearTo:
		return ModNearTo
	}
	return 0
}

// Collect parses the given ipmt source and emits semantic tokens for node
// names, alias spans, arrow spans, and quoted-tooltip spans. Positions
// are returned in whole-file LSP coordinates given the line offset of
// the block within its containing markdown document (0 for pure .ipmt
// files).
//
// `wholeText` is currently unused: every span is sourced from the parser's
// graph.Src.Lex structure and positioned via a mapper built from blockText
// (plus lineOffset). The parameter is retained for call-site symmetry and a
// possible future cross-block mapper. Callers may pass blockText for it.
func Collect(wholeText, blockText string, lineOffset uint32) []Token {
	graph, err := parser.ParseIPMTBytes([]byte(blockText), "")
	if err != nil {
		return nil
	}
	mapper := lspos.NewMapper(blockText)

	out := make([]Token, 0, len(graph.Nodes)*2+len(graph.Edges))

	// Node-name tokens + alias-presence modifier.
	for _, n := range graph.Nodes {
		var nodeType uint32
		if n.Type == model.Unresolved {
			// Unresolved-kind node titles paint with the neutral grey
			// ipmUnresolved color (the same color as the `::?` prefix marker).
			// Other call sites of nodeTypeToToken still skip Unresolved, so
			// the endpoint pass / alias decls don't accidentally repaint
			// Unresolved nodes elsewhere.
			nodeType = TypUnresolved
		} else {
			t, ok := nodeTypeToToken(n.Type)
			if !ok {
				continue
			}
			nodeType = t
		}
		rng, ok := graph.Src.Positions.Nodes[n.ID]
		if !ok {
			continue
		}
		startLine, startCol, endLine, endCol := mapper.OffsetRangeToUTF16(rng[0], rng[1])
		if endLine != startLine {
			endCol = startCol + 1
		}
		var mods uint32
		if n.Alias != "" {
			mods |= ModHasAlias
		}
		out = append(out, Token{
			Line:     startLine + lineOffset,
			StartCol: startCol,
			Length:   endCol - startCol,
			Type:     nodeType,
			Mods:     mods,
		})
	}

	// Alias declarations (`<alias>::a`) and references (bare occurrences that
	// resolved to an alias) — both positioned by the parser (graph.Src.Lex).
	// Edge-endpoint refs are also colored by the endpoint pass below; the
	// identical tokens dedupe in Flatten.
	for _, ad := range graph.Src.Lex.AliasDecls {
		if kind, ok := nodeTypeToTokenAny(ad.Type); ok {
			emitMatchRange(&out, []int{ad.Span[0], ad.Span[1]}, 0, mapper, lineOffset, kind, ModAlias)
		}
	}
	for _, ar := range graph.Src.Lex.AliasRefs {
		if kind, ok := nodeTypeToTokenAny(ar.Type); ok {
			emitMatchRange(&out, []int{ar.Span[0], ar.Span[1]}, 0, mapper, lineOffset, kind, ModAlias)
		}
	}

	// Edge arrow tokens + endpoint kind tokens.
	nodeByID := make(map[int]model.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodeByID[n.ID] = n
	}
	for _, e := range graph.Edges {
		pos, ok := graph.Src.Positions.Edges[e.ID]
		if !ok {
			continue
		}
		mods := relationToModifier(e.SstLinkType)
		emitArrowSpanTokens(&out, blockText, pos.Arrow[0], pos.Arrow[1], mapper, lineOffset, mods)

		for _, side := range []struct {
			id  int
			rng []int
		}{
			{e.Source, pos.Source[:]},
			{e.Target, pos.Target[:]},
		} {
			n, ok := nodeByID[side.id]
			if !ok {
				continue
			}
			kind, ok := nodeTypeToTokenAny(n.Type)
			if !ok {
				continue
			}
			if len(side.rng) < 2 || side.rng[1] <= side.rng[0] {
				continue
			}
			if declRng, ok := graph.Src.Positions.Nodes[n.ID]; ok {
				if declRng[0] == side.rng[0] && declRng[1] == side.rng[1] {
					continue
				}
			}
			// An endpoint referenced by the node's alias is colored as the
			// alias short-form (`*-alias`), not the plain `*-title` kind —
			// an aliased node's title color is reserved for its long form.
			// Emit it here from the parser's exact span rather than leaning
			// on the global alias-ref regex, which misses line-leading
			// references. Long-form re-mentions (no alias, or the full name)
			// fall through to the kind title.
			endpointMods := uint32(0)
			if n.Alias != "" && side.rng[1] <= len(blockText) && blockText[side.rng[0]:side.rng[1]] == n.Alias {
				endpointMods = ModAlias
			}
			emitMatchRange(&out, side.rng, 0, mapper, lineOffset, kind, endpointMods)
		}
	}

	// Comments — whole-line `#` (graph.Src.Lex.Comments).
	for _, c := range graph.Src.Lex.Comments {
		emitMatchRange(&out, []int{c[0], c[1]}, 0, mapper, lineOffset, TypComment, 0)
	}

	// Tooltips — node tooltips (`"…" ::tip`) from the parser. Edge tooltips
	// (`--"…"--`) are emitted by emitArrowSpanTokens above (it scans quoted
	// runs to the closing quote), so no regex is needed.
	for _, t := range graph.Src.Lex.Tooltips {
		emitMatchRange(&out, []int{t[0], t[1]}, 0, mapper, lineOffset, TypTooltip, 0)
	}

	// Strings — quoted node titles (graph.Src.Lex.Strings). Emitted after
	// the node-name pass so the string color wins on the title span.
	for _, s := range graph.Src.Lex.Strings {
		emitMatchRange(&out, []int{s[0], s[1]}, 0, mapper, lineOffset, TypString, 0)
	}

	// Kind markers (`::e`/`::t`/`::c`) and type markers (`::a`/`::tip`) —
	// from the parser, so a `::e` inside a comment or quoted string is never
	// mis-tagged (the parser doesn't tokenize those as markers).
	for _, km := range graph.Src.Lex.KindMarkers {
		kind, ok := nodeTypeToToken(km.Type)
		if !ok {
			continue
		}
		emitMatchRange(&out, []int{km.Span[0], km.Span[1]}, 0, mapper, lineOffset, kind, ModMarker)
	}
	for _, tm := range graph.Src.Lex.TypeMarkers {
		emitMatchRange(&out, []int{tm[0], tm[1]}, 0, mapper, lineOffset, TypTypeMarker, 0)
	}
	// `::?` prefix markers — neutral grey ipmUnresolved color.
	for _, up := range graph.Src.Lex.UnresolvedPrefixes {
		emitMatchRange(&out, []int{up[0], up[1]}, 0, mapper, lineOffset, TypUnresolved, 0)
	}

	return Flatten(out)
}

// Flatten resolves the intentionally-overlapping tokens [Collect] emits
// into a sorted, NON-OVERLAPPING stream — the canonical output every
// consumer (LSP wire encoder, md-html, preview) can use directly.
//
// Resolution is per-character LAST-WINS BY EMISSION ORDER: for every
// UTF-16 column a later token in the input overwrites an earlier one on
// the columns they share. This is byte-for-byte the same rule
// pkg/mdhtml.HighlightTokens and previewHighlight.ts:renderFromLspTokens
// apply when rendering, so flattening here changes no rendered output —
// it only moves the resolution upstream so the LSP can ship a spec-clean
// (disjoint, sorted) token set instead of relying on client leniency.
//
// Tokens that map to no CSS class (ClassSuffix == "") are skipped, never
// painting over a colored token — matching the `continue` both renderers
// do. [Collect] never emits such tokens (all its types are valid), but
// the guard keeps Flatten equivalent to the renderers for any input.
//
// All [Collect] tokens are single-line (it clamps a range that crosses a
// newline to one character on the start line), so painting per line is
// exact.
func Flatten(tokens []Token) []Token {
	if len(tokens) == 0 {
		return nil
	}
	type cell struct {
		typ, mods uint32
		set       bool
	}
	rows := map[uint32][]cell{}
	for _, t := range tokens {
		if ClassSuffix(TypeName(t.Type), t.Mods) == "" {
			continue
		}
		end := t.StartCol + t.Length
		row := rows[t.Line]
		if uint32(len(row)) < end {
			grown := make([]cell, end)
			copy(grown, row)
			row = grown
			rows[t.Line] = row
		}
		for c := t.StartCol; c < end; c++ {
			row[c] = cell{t.Type, t.Mods, true}
		}
	}
	lineNums := make([]uint32, 0, len(rows))
	for ln := range rows {
		lineNums = append(lineNums, ln)
	}
	sort.Slice(lineNums, func(i, j int) bool { return lineNums[i] < lineNums[j] })

	out := make([]Token, 0, len(tokens))
	for _, ln := range lineNums {
		row := rows[ln]
		for c := 0; c < len(row); {
			if !row[c].set {
				c++
				continue
			}
			start := c
			cur := row[c]
			for c < len(row) && row[c].set && row[c].typ == cur.typ && row[c].mods == cur.mods {
				c++
			}
			out = append(out, Token{
				Line:     ln,
				StartCol: uint32(start),
				Length:   uint32(c - start),
				Type:     cur.typ,
				Mods:     cur.mods,
			})
		}
	}
	return out
}

// emitArrowSpanTokens walks `blockText[start:end]` — one edge's arrow
// span — and emits non-overlapping tokens for arrow-symbol runs,
// relation markers, and embedded tooltip/identifier labels.
func emitArrowSpanTokens(out *[]Token, blockText string, start, end int, mapper *lspos.Mapper, lineOffset uint32, mods uint32) {
	// Arrow syntax is single-line. The parser's `Arrow` range can spill
	// over a `\n` into the first character of the next line's target —
	// clamp the walker to the current line.
	if nl := indexOfByteIn(blockText, start, end, '\n'); nl >= 0 {
		end = nl
	}
	i := start
	for i < end {
		ch := blockText[i]
		switch {
		case ch == ' ' || ch == '\t':
			i++
		case ch == '-' || ch == '<' || ch == '>':
			j := i + 1
			for j < end {
				c := blockText[j]
				if c != '-' && c != '<' && c != '>' {
					break
				}
				j++
			}
			emitMatchRange(out, []int{i, j}, 0, mapper, lineOffset, TypArrow, mods)
			i = j
		case ch == ':' && i+2 < end && blockText[i+1] == ':' && isRelationLetter(blockText[i+2]):
			emitMatchRange(out, []int{i, i + 3}, 0, mapper, lineOffset, TypRelation, 0)
			i += 3
		case ch == '"':
			// Quoted edge tooltip — scan to the closing quote so a
			// multi-word tooltip (`--"a b c"-->`) is one token, not split on
			// its interior spaces. Handles `\"`/`\\` escapes.
			j := i + 1
			for j < end {
				if blockText[j] == '\\' {
					j += 2
					continue
				}
				if blockText[j] == '"' {
					j++
					break
				}
				j++
			}
			if j > end {
				j = end
			}
			emitMatchRange(out, []int{i, j}, 0, mapper, lineOffset, TypTooltip, 0)
			i = j
		default:
			j := i
			for j < end {
				c := blockText[j]
				if c == ' ' || c == '\t' || c == '-' || c == '<' || c == '>' {
					break
				}
				if c == ':' && j+2 < end && blockText[j+1] == ':' && isRelationLetter(blockText[j+2]) {
					break
				}
				j++
			}
			if j > i {
				emitMatchRange(out, []int{i, j}, 0, mapper, lineOffset, TypTooltip, 0)
			}
			i = j
		}
	}
}

func isRelationLetter(c byte) bool {
	return c == 'L' || c == 'P' || c == 'X' || c == 'N'
}

func indexOfByteIn(s string, start, end int, b byte) int {
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	for i := start; i < end; i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func emitMatchRange(out *[]Token, idx []int, baseOffset int, mapper *lspos.Mapper, lineOffset uint32, tokType, tokMods uint32) {
	if idx == nil {
		return
	}
	absStart := baseOffset + idx[0]
	absEnd := baseOffset + idx[1]
	startLine, startCol, endLine, endCol := mapper.OffsetRangeToUTF16(absStart, absEnd)
	if endLine != startLine {
		endCol = startCol + 1
	}
	*out = append(*out, Token{
		Line:     startLine + lineOffset,
		StartCol: startCol,
		Length:   endCol - startCol,
		Type:     tokType,
		Mods:     tokMods,
	})
}
