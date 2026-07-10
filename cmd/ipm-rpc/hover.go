package main

import (
	"context"
	"strings"

	lsp "go.lsp.dev/protocol"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/lspos"
)

// hover returns a markdown-formatted hover for the token under the cursor.
// Phase 1 implementation: lexical only — scan the line text for known
// ipmt tokens around the cursor position. Semantic hover (resolve an alias
// reference to its declaration, show inferred SST kind for a node name)
// is deferred to a follow-up round when we maintain an indexed AST per
// document.
//
// Conservative behavior: when the cursor isn't on a recognized token,
// return nil — LSP treats that as "no hover available."
func (s *server) hover(ctx context.Context, params *lsp.HoverParams) (*lsp.Hover, error) {
	s.mu.Lock()
	doc, ok := s.docs[params.TextDocument.URI]
	s.mu.Unlock()
	if !ok {
		return nil, nil
	}
	line := lineAt(doc.text, int(params.Position.Line))
	if line == "" {
		return nil, nil
	}
	// params.Position.Character is a UTF-16 code-unit column per the LSP spec,
	// but tokenAt scans/slices the line by bytes. Convert the cursor column to
	// a byte offset within the line (correct for non-ASCII prefixes such as
	// accented node names), then map the matched byte span back to UTF-16
	// columns for the returned Range.
	mapper := lspos.NewMapper(doc.text)
	lineStart := mapper.LineColUTF16ToOffset(params.Position.Line, 0)
	cursorOff := mapper.LineColUTF16ToOffset(params.Position.Line, params.Position.Character)
	col := cursorOff - lineStart
	tok, start, end := tokenAt(line, col)
	if tok == "" {
		return nil, nil
	}
	doc_, ok := hoverDocs[tok]
	if !ok {
		return nil, nil
	}
	_, startCol := mapper.OffsetToUTF16(lineStart + start)
	_, endCol := mapper.OffsetToUTF16(lineStart + end)
	return &lsp.Hover{
		Contents: lsp.MarkupContent{
			Kind:  lsp.Markdown,
			Value: doc_,
		},
		Range: &lsp.Range{
			Start: lsp.Position{Line: params.Position.Line, Character: startCol},
			End:   lsp.Position{Line: params.Position.Line, Character: endCol},
		},
	}, nil
}

// hoverDocs holds the markdown content shown for each recognized ipmt
// token. Curated by hand; deliberately short — gopls-quality docs with
// links / examples are a follow-up.
var hoverDocs = map[string]string{
	"::e":      "**Event marker** (`::e`)\n\nDeclares the node as an **event** — a transient happening at the model's timescale.\nRendered as an orange node.",
	"::t":      "**Thing marker** (`::t`)\n\nDeclares the node as a **thing** — a persistent participant.\nRendered as a green node. `::t` is rarely needed explicitly: bare nodes default to things.",
	"::c":      "**Concept marker** (`::c`)\n\nDeclares the node as a **concept** — an invariant property that events or things can express.\nRendered as a blue node.",
	"::a":      "**Alias marker** (`::a`)\n\nAttaches a short stable identifier to the node. Use `name::a Long node title ::e` to declare; later references can use `name` alone.",
	"::tip":    "**Tooltip marker** (`::tip`)\n\nAttaches an explanatory tooltip to the preceding node — shown when the reader hovers the rendered diagram.",
	"-->":      "**Leads-to arrow** (`-->`)\n\nForward edge from source to target. Between two events: temporal/causal flow (the L relation). Between a thing and an event: participation (P, part-of).",
	"<--":      "**Reverse arrow** (`<--`)\n\nSame edge as `-->` written right-to-left for readability. The logical direction still flows from the *source* of the relation to the *target*.",
	"---":      "**Near-to (undirected)** (`---`)\n\nSimilarity / proximity between two nodes of the same kind. No direction; the N relation.",
	"--::P-->": "**Explicit part-of** (`--::P-->`)\n\nExplicit containment: the source is part-of the target.",
	"--::L-->": "**Explicit leads-to** (`--::L-->`)\n\nExplicit temporal/causal flow.",
	"--::X-->": "**Explicit expresses** (`--::X-->`)\n\nThe source expresses a property of the target (the X / expresses relation).",
	"--::N--":  "**Explicit near-to** (`--::N--`)\n\nUndirected similarity between two nodes.",
}

// lineAt returns the i-th line (0-based) of text, without the trailing
// newline. Returns "" for out-of-range i.
func lineAt(text string, i int) string {
	lines := strings.Split(text, "\n")
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}

// tokenAt finds the recognized ipmt token spanning the given column on the
// given line. Returns the token text, its start column, and its end column
// (LSP positions are 0-based; end is exclusive in LSP ranges).
//
// We try longest-prefix matching at every plausible start position around
// the cursor, since some tokens are substrings of others (`-->` vs
// `--::P-->`). The first hit wins.
func tokenAt(line string, col int) (tok string, start, end int) {
	if col > len(line) {
		col = len(line)
	}
	// Candidate token alphabet, ordered longest-first so substring matches
	// are preferred over their shorter cousins.
	candidates := []string{
		"--::P-->", "--::L-->", "--::X-->", "--::N--",
		"-->", "<--", "---",
		"::tip", "::e", "::t", "::c", "::a",
	}
	// Slide a window over the line. For each candidate, check every start
	// position whose span contains `col`.
	for _, c := range candidates {
		L := len(c)
		// Maximum start: anywhere from col-L+1 up to col (inclusive), but
		// clamped to the line bounds.
		minStart := col - L + 1
		if minStart < 0 {
			minStart = 0
		}
		maxStart := col
		if maxStart > len(line)-L {
			maxStart = len(line) - L
		}
		for s := minStart; s <= maxStart; s++ {
			if s < 0 {
				continue
			}
			if s+L > len(line) {
				continue
			}
			if line[s:s+L] == c {
				return c, s, s + L
			}
		}
	}
	return "", 0, 0
}
