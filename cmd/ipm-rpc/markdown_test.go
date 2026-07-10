package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	lsp "go.lsp.dev/protocol"

	iplog "github.com/infinite-pm/ipm-tools/pkg/ipm/log"
)

// runDiagnosticsOn returns the diagnostics the server would produce for a
// document with the given language ID and body. The test stays in-process —
// no jrpc2 transport — by calling publishDiagnostics with a stub server
// that captures the notification payload instead of pushing it on the wire.
func runDiagnosticsOn(t *testing.T, languageID, body string) []lsp.Diagnostic {
	t.Helper()
	iplog.SetupDiscard()
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var captured []lsp.Diagnostic
	srv.notifyOverride = func(method string, params any) {
		if method != "textDocument/publishDiagnostics" {
			return
		}
		p, ok := params.(*lsp.PublishDiagnosticsParams)
		if !ok {
			t.Fatalf("unexpected params type %T", params)
		}
		captured = p.Diagnostics
	}

	doc := &document{
		uri:      lsp.DocumentURI("file:///tmp/x"),
		language: languageID,
		text:     body,
		version:  1,
	}
	srv.publishDiagnostics(t.Context(), doc)
	return captured
}

func TestMarkdown_noFencesNoDiagnostics(t *testing.T) {
	body := "# Hello\n\nJust some prose, no ipmt blocks.\n"
	ds := runDiagnosticsOn(t, "markdown", body)
	if len(ds) != 0 {
		t.Fatalf("plain markdown should produce no diagnostics; got %d:\n%+v", len(ds), ds)
	}
}

func TestMarkdown_validFenceNoDiagnostics(t *testing.T) {
	body := strings.Join([]string{
		"# Demo",
		"",
		"```ipmt",
		"a ::e --> b ::e",
		"```",
		"",
	}, "\n")
	ds := runDiagnosticsOn(t, "markdown", body)
	if len(ds) != 0 {
		t.Fatalf("valid ipmt fence should produce no diagnostics; got:\n%+v", ds)
	}
}

func TestMarkdown_parseErrorPositionInWholeFile(t *testing.T) {
	// The ipmt fence opens on line 3 (0-based: line 2). The bad content is
	// on the second line of the block, which is line 4 (0-based: 3) of the
	// whole file. The diagnostic must point at line 3 (0-based), not at
	// line 1 (0-based) of the block.
	body := strings.Join([]string{
		"intro",             // line 0
		"",                  // line 1
		"```ipmt",           // line 2 (fence open)
		"a ::e --> b ::e",   // line 3 (block local line 0)
		"!!!! invalid !!!!", // line 4 (block local line 1, parse error here)
		"```",               // line 5 (fence close)
		"",                  // line 6
	}, "\n")
	ds := runDiagnosticsOn(t, "markdown", body)
	if len(ds) != 1 {
		t.Fatalf("expected 1 diagnostic from the bad block; got %d:\n%+v", len(ds), ds)
	}
	gotLine := ds[0].Range.Start.Line
	if gotLine < 3 || gotLine > 4 {
		t.Errorf("diagnostic on wrong line: got %d, want 3 or 4 (block content lines)", gotLine)
	}
}

func TestMarkdown_unterminatedFenceFlagged(t *testing.T) {
	body := strings.Join([]string{
		"intro",
		"",
		"```ipmt",
		"a ::e",
		// no closing fence
	}, "\n")
	ds := runDiagnosticsOn(t, "markdown", body)
	if len(ds) == 0 {
		t.Fatalf("expected at least one diagnostic for unterminated fence; got none")
	}
	gotMsg := ds[0].Message
	if !strings.Contains(gotMsg, "unterminated") {
		t.Errorf("expected 'unterminated' in diagnostic message; got %q", gotMsg)
	}
	if ds[0].Range.Start.Line != 2 {
		t.Errorf("expected diagnostic on opening fence line (2); got %d", ds[0].Range.Start.Line)
	}
}

func TestMarkdown_multipleBlocksIndependently(t *testing.T) {
	// First block is valid; second is malformed. The diagnostic must point
	// at the second block, not the first.
	body := strings.Join([]string{
		"```ipmt",           // 0
		"a ::e --> b ::e",   // 1
		"```",               // 2
		"",                  // 3
		"middle prose",      // 4
		"",                  // 5
		"```ipmt",           // 6
		"!!! malformed !!!", // 7
		"```",               // 8
	}, "\n")
	ds := runDiagnosticsOn(t, "markdown", body)
	if len(ds) != 1 {
		t.Fatalf("expected 1 diagnostic (only the 2nd block fails); got %d:\n%+v", len(ds), ds)
	}
	if ds[0].Range.Start.Line < 7 {
		t.Errorf("diagnostic should target the 2nd block (line >= 7); got line %d",
			ds[0].Range.Start.Line)
	}
}

func TestIpmt_directDocumentStillWorks(t *testing.T) {
	// Sanity: language=ipmt path still functions after the refactor.
	body := "a ::e --> b ::e\n"
	ds := runDiagnosticsOn(t, "ipmt", body)
	if len(ds) != 0 {
		t.Fatalf("valid .ipmt source should produce no diagnostics; got:\n%+v", ds)
	}
}
