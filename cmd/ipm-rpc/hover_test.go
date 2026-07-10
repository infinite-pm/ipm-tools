package main

import (
	"strings"
	"testing"

	lsp "go.lsp.dev/protocol"
)

func TestTokenAt_recognizesAllMarkers(t *testing.T) {
	// Each fixture line; the column points roughly into the middle of the
	// token we expect to recognize. The expected `want` is the recognized
	// token text.
	cases := []struct {
		line string
		col  int
		want string
	}{
		// Type markers.
		{"a ::e", 3, "::e"},
		{"a ::t", 3, "::t"},
		{"a ::c", 3, "::c"},
		{"name::a Foo bar", 5, "::a"},
		{"Foo ::tip", 5, "::tip"},
		// Arrows.
		{"a --> b", 3, "-->"},
		{"a <-- b", 3, "<--"},
		{"a --- b", 3, "---"},
		// Explicit relations — longest-prefix wins over `-->` alone.
		{"a --::P--> b", 4, "--::P-->"},
		{"a --::L--> b", 4, "--::L-->"},
		{"a --::X--> b", 4, "--::X-->"},
		{"a --::N-- b", 4, "--::N--"},
	}
	for _, tc := range cases {
		got, start, end := tokenAt(tc.line, tc.col)
		if got != tc.want {
			t.Errorf("tokenAt(%q, %d) = %q (start=%d end=%d), want %q",
				tc.line, tc.col, got, start, end, tc.want)
		}
	}
}

func TestTokenAt_returnsEmptyOutsideToken(t *testing.T) {
	// Bare identifier text — no recognized token at that position.
	if got, _, _ := tokenAt("Patrick wears black", 5); got != "" {
		t.Errorf("expected no token in plain text; got %q", got)
	}
}

func TestHover_returnsMarkdownForTypeMarker(t *testing.T) {
	srv := newCmdTestServer(t)
	srv.docs[lsp.DocumentURI("file:///tmp/x.ipmt")] = &document{
		uri:      lsp.DocumentURI("file:///tmp/x.ipmt"),
		language: "ipmt",
		text:     "alice ::e --> bob ::e\n",
	}
	h, err := srv.hover(t.Context(), &lsp.HoverParams{
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI("file:///tmp/x.ipmt")},
			Position:     lsp.Position{Line: 0, Character: 7}, // mid `::e`
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h == nil {
		t.Fatal("expected hover for ::e, got nil")
	}
	if h.Contents.Kind != lsp.Markdown {
		t.Errorf("expected markdown kind, got %v", h.Contents.Kind)
	}
	if !strings.Contains(h.Contents.Value, "Event marker") {
		t.Errorf("hover text missing 'Event marker'; got:\n%s", h.Contents.Value)
	}
}

func TestHover_returnsMarkdownForArrow(t *testing.T) {
	srv := newCmdTestServer(t)
	srv.docs[lsp.DocumentURI("file:///tmp/x.ipmt")] = &document{
		uri:      lsp.DocumentURI("file:///tmp/x.ipmt"),
		language: "ipmt",
		text:     "alice --> bob\n",
	}
	h, err := srv.hover(t.Context(), &lsp.HoverParams{
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI("file:///tmp/x.ipmt")},
			Position:     lsp.Position{Line: 0, Character: 7}, // mid `-->`
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h == nil {
		t.Fatal("expected hover for -->, got nil")
	}
	if !strings.Contains(h.Contents.Value, "Leads-to arrow") {
		t.Errorf("hover text missing 'Leads-to arrow'; got:\n%s", h.Contents.Value)
	}
}

func TestHover_nilOutsideToken(t *testing.T) {
	srv := newCmdTestServer(t)
	srv.docs[lsp.DocumentURI("file:///tmp/x.ipmt")] = &document{
		uri:      lsp.DocumentURI("file:///tmp/x.ipmt"),
		language: "ipmt",
		text:     "Patrick wears black t-shirt ::e\n",
	}
	h, err := srv.hover(t.Context(), &lsp.HoverParams{
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI("file:///tmp/x.ipmt")},
			Position:     lsp.Position{Line: 0, Character: 5}, // in "Patrick"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h != nil {
		t.Fatalf("expected nil hover on plain text; got %+v", h)
	}
}

// TestHover_nonASCIIPrefix guards the UTF-16/byte-offset conversion: a
// non-ASCII rune before the token makes the byte index and the LSP UTF-16
// column diverge. The cursor column is UTF-16 (per the LSP spec); hover must
// map it to the right byte offset before scanning and map the matched span
// back to UTF-16 for the returned Range.
func TestHover_nonASCIIPrefix(t *testing.T) {
	srv := newCmdTestServer(t)
	// "café " is 5 runes / 5 UTF-16 code units, but 6 bytes (é is 2 bytes).
	// "::e" therefore starts at byte 6 but UTF-16 column 5.
	srv.docs[lsp.DocumentURI("file:///tmp/x.ipmt")] = &document{
		uri:      lsp.DocumentURI("file:///tmp/x.ipmt"),
		language: "ipmt",
		text:     "café ::e\n",
	}
	h, err := srv.hover(t.Context(), &lsp.HoverParams{
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI("file:///tmp/x.ipmt")},
			Position:     lsp.Position{Line: 0, Character: 6}, // mid `::e` in UTF-16 columns
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h == nil {
		t.Fatal("expected hover for ::e after non-ASCII prefix, got nil")
	}
	if !strings.Contains(h.Contents.Value, "Event marker") {
		t.Errorf("hover text missing 'Event marker'; got:\n%s", h.Contents.Value)
	}
	// Range must be in UTF-16 columns: "::e" spans columns 5..8.
	if h.Range == nil {
		t.Fatal("expected a hover Range")
	}
	if h.Range.Start.Character != 5 || h.Range.End.Character != 8 {
		t.Errorf("hover Range = [%d,%d), want [5,8) UTF-16 columns",
			h.Range.Start.Character, h.Range.End.Character)
	}
}

func TestHover_unknownDocumentReturnsNil(t *testing.T) {
	srv := newCmdTestServer(t)
	h, err := srv.hover(t.Context(), &lsp.HoverParams{
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI("file:///nothing.ipmt")},
			Position:     lsp.Position{Line: 0, Character: 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h != nil {
		t.Fatalf("expected nil hover for unknown document; got %+v", h)
	}
}
