package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	lsp "go.lsp.dev/protocol"
)

// embedBufferViaLSP is a small helper to call ipm.embedBuffer through the
// LSP test client and unmarshal the typed response. `extra` entries are
// merged into the argument object, for flags like {"tokensOnly": true}.
func embedBufferViaLSP(t *testing.T, cli interface {
	CallResult(context.Context, string, any, any) error
}, uri string, extra ...map[string]any) (*embedBufferResult, error) {
	t.Helper()
	var raw json.RawMessage
	args := map[string]any{"uri": uri}
	for _, m := range extra {
		for k, v := range m {
			args[k] = v
		}
	}
	err := cli.CallResult(t.Context(), "workspace/executeCommand", &lsp.ExecuteCommandParams{
		Command:   "ipm.embedBuffer",
		Arguments: []any{args},
	}, &raw)
	if err != nil {
		return nil, err
	}
	var out embedBufferResult
	if jerr := json.Unmarshal(raw, &out); jerr != nil {
		return nil, fmt.Errorf("unmarshal: %w", jerr)
	}
	return &out, nil
}

// makeMdInTmpRepo creates a tmp dir with a .git marker (so FindRepoRoot
// stops there) and writes the .md at <dir>/<rel>. Returns the file URI.
func makeMdInTmpRepo(t *testing.T, relPath, content string) string {
	t.Helper()
	root := t.TempDir()
	// .git/HEAD is enough to trigger FindRepoRoot.
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mdAbs := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(mdAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mdAbs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return "file://" + mdAbs
}

func TestEmbedBuffer_rendersOpenBuffer(t *testing.T) {
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{
		ClientInfo: &lsp.ClientInfo{Name: "embed-buffer-test"},
	}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	mdContent := "# t\n\n```ipmt\nA ::e --> B ::e\n```\n"
	uri := makeMdInTmpRepo(t, "README.md", mdContent)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "markdown", Version: 1, Text: mdContent,
		},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatalf("embedBuffer: %v", err)
	}
	if len(res.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(res.Blocks))
	}
	b := res.Blocks[0]
	if b.Hash == "" {
		t.Errorf("hash missing on block %+v", b)
	}
	if b.SVGBase64 == "" {
		t.Errorf("SVGBase64 missing on block %+v", b)
	}
	dec, err := base64.StdEncoding.DecodeString(b.SVGBase64)
	if err != nil {
		t.Fatalf("svg not valid base64: %v", err)
	}
	if !strings.Contains(string(dec), "<svg") {
		t.Fatalf("decoded SVG missing <svg root: %q", string(dec)[:80])
	}

	// Piggybacked semantic tokens: the block carries block-relative
	// tokens so the markdown preview can color each fence the same way
	// as the editor pane. We don't enumerate every token here — just
	// confirm the response is non-empty and that one of the event-kind
	// names is present (the block is "A ::e --> B ::e", which produces
	// at least two ipmEvent tokens).
	if len(b.Tokens) == 0 {
		t.Fatalf("expected tokens on block, got none: %+v", b)
	}
	var sawEvent bool
	for _, tk := range b.Tokens {
		if tk.Type == "ipmEvent" {
			sawEvent = true
			break
		}
	}
	if !sawEvent {
		t.Errorf("expected at least one ipmEvent token, got types: %v", tokenTypeSummary(b.Tokens))
	}
}

func tokenTypeSummary(tokens []bufferToken) []string {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, t.Type)
	}
	return out
}

func TestEmbedBuffer_reflectsLiveEdit(t *testing.T) {
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	initial := "```ipmt\nA ::e --> B ::e\n```\n"
	uri := makeMdInTmpRepo(t, "README.md", initial)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "markdown", Version: 1, Text: initial,
		},
	}); err != nil {
		t.Fatal(err)
	}
	first, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(first.Blocks))
	}
	hash1 := first.Blocks[0].Hash

	// Edit the buffer (NOT the disk file) and re-call.
	edited := "```ipmt\nA ::e --> C ::e\n```\n"
	if err := cli.Notify(ctx, "textDocument/didChange", &lsp.DidChangeTextDocumentParams{
		TextDocument: lsp.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI(uri)},
			Version:                2,
		},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{{Text: edited}},
	}); err != nil {
		t.Fatal(err)
	}
	second, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Blocks) != 1 {
		t.Fatalf("want 1 block after edit, got %d", len(second.Blocks))
	}
	hash2 := second.Blocks[0].Hash
	if hash1 == hash2 {
		t.Fatalf("hash should change after edit; both = %s", hash1)
	}
	if second.Blocks[0].SVGBase64 == first.Blocks[0].SVGBase64 {
		t.Fatalf("SVGBase64 should change after edit")
	}

	// Critically: the disk file is NOT updated by ipm.embedBuffer.
	mdAbs := strings.TrimPrefix(uri, "file://")
	diskBytes, _ := os.ReadFile(mdAbs)
	if string(diskBytes) != initial {
		t.Fatalf("ipm.embedBuffer must NOT touch disk; got:\n%s", string(diskBytes))
	}
}

func TestEmbedBuffer_documentNotOpenIsError(t *testing.T) {
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	uri := "file:///tmp/never-opened.md"
	_, err := embedBufferViaLSP(t, cli, uri)
	if err == nil {
		t.Fatal("expected error for unopened document, got nil")
	}
	if !strings.Contains(err.Error(), "not open") {
		t.Fatalf("error should mention not-open; got %v", err)
	}
}

func TestEmbedBuffer_parseErrorReportedPerBlock(t *testing.T) {
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	// Two blocks: one valid, one definitely malformed (unterminated quoted
	// string — the parser rejects this with a tokenizer error rather than
	// recovering into a stray-text node).
	bogus := "```ipmt\nA --\"unterminated\n```\n\n```ipmt\nA ::e --> B ::e\n```\n"
	uri := makeMdInTmpRepo(t, "README.md", bogus)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "markdown", Version: 1, Text: bogus,
		},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatalf("embedBuffer should not fail wholesale on per-block parse error: %v", err)
	}
	if len(res.Blocks) != 2 {
		t.Fatalf("want 2 blocks (good + bad), got %d", len(res.Blocks))
	}
	// Find the bad block — whichever one has an Error string.
	var bad *bufferBlock
	var good *bufferBlock
	for i := range res.Blocks {
		if res.Blocks[i].Error != "" {
			bad = &res.Blocks[i]
		} else {
			good = &res.Blocks[i]
		}
	}
	if bad == nil {
		t.Fatalf("expected one block with Error; got %+v", res.Blocks)
	}
	if good == nil {
		t.Fatalf("expected one block without Error; got %+v", res.Blocks)
	}
	if bad.SVGBase64 != "" {
		t.Errorf("bad block must have empty SVGBase64; got %q", bad.SVGBase64[:20])
	}
	if good.SVGBase64 == "" {
		t.Error("good block must still render even when a sibling block failed")
	}
}

func TestEmbedBuffer_ipmtURIRendersWholeBufferAsOneBlock(t *testing.T) {
	// `.ipmt` URI: cmdEmbedBuffer must skip the markdown fence scanner
	// and render the whole buffer as a single block. This is the path
	// the `.ipmt` preview pane uses.
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	// thing→event direction (`bob ::t --> Alice ::e`); the reverse
	// event→thing form is rejected by the parser.
	ipmtSource := "bob ::t --> Alice ::e\n"
	uri := makeMdInTmpRepo(t, "diagram.ipmt", ipmtSource)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "ipmt", Version: 1, Text: ipmtSource,
		},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatalf("embedBuffer: %v", err)
	}
	if len(res.Blocks) != 1 {
		t.Fatalf("want exactly 1 block for .ipmt URI, got %d: %+v", len(res.Blocks), res.Blocks)
	}
	b := res.Blocks[0]
	if b.ID != "preview" {
		t.Errorf("want block.ID=preview, got %q", b.ID)
	}
	if b.SVGBase64 == "" {
		t.Errorf("want SVGBase64 populated; got error=%q", b.Error)
	}
	if b.Source != ipmtSource {
		t.Errorf("want Source to be the whole buffer text; got %q", b.Source)
	}
	if len(b.Tokens) == 0 {
		t.Errorf("want non-empty tokens for the preview block; got %+v", b)
	}
}

func TestEmbedBuffer_tokensOnlySkipsSVGRender(t *testing.T) {
	// tokensOnly is what makes per-keystroke coloring affordable in the
	// markdown preview: same scan, same tokens, no layout. The client
	// runs it on every edit and keeps the full render on a debounce, so
	// the two MUST agree on `source` and `tokens` — only `svgBase64`
	// may differ.
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	mdContent := "# t\n\n```ipmt\nA ::e --> B ::e\n```\n\n```ipmt\nbob ::t --> Alice ::e\n```\n"
	uri := makeMdInTmpRepo(t, "README.md", mdContent)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "markdown", Version: 1, Text: mdContent,
		},
	}); err != nil {
		t.Fatal(err)
	}

	full, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatalf("embedBuffer: %v", err)
	}
	lean, err := embedBufferViaLSP(t, cli, uri, map[string]any{"tokensOnly": true})
	if err != nil {
		t.Fatalf("embedBuffer tokensOnly: %v", err)
	}

	if len(lean.Blocks) != len(full.Blocks) || len(lean.Blocks) != 2 {
		t.Fatalf("want the same 2 blocks either way; full=%d lean=%d", len(full.Blocks), len(lean.Blocks))
	}
	for i, lb := range lean.Blocks {
		fb := full.Blocks[i]
		if lb.SVGBase64 != "" {
			t.Errorf("block %d: tokensOnly must not render an SVG, got %d bytes of base64", i, len(lb.SVGBase64))
		}
		if fb.SVGBase64 == "" {
			t.Errorf("block %d: the full call must still render an SVG", i)
		}
		if lb.Source != fb.Source || lb.Hash != fb.Hash || lb.ID != fb.ID {
			t.Errorf("block %d: identity must not depend on tokensOnly\n full=%+v\n lean=%+v", i, fb, lb)
		}
		if len(lb.Tokens) == 0 {
			t.Errorf("block %d: tokensOnly must still return tokens", i)
		}
		if !reflect.DeepEqual(lb.Tokens, fb.Tokens) {
			t.Errorf("block %d: tokens must be identical either way\n full=%v\n lean=%v",
				i, tokenTypeSummary(fb.Tokens), tokenTypeSummary(lb.Tokens))
		}
	}
}

func TestEmbedBuffer_tokensOnlyIpmtURI(t *testing.T) {
	// The `.ipmt` branch takes a different code path (whole buffer as one
	// block) and must honour the flag too — the preview pane's coloring
	// runs through the same cache.
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	ipmtSource := "bob ::t --> Alice ::e\n"
	uri := makeMdInTmpRepo(t, "diagram.ipmt", ipmtSource)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "ipmt", Version: 1, Text: ipmtSource,
		},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := embedBufferViaLSP(t, cli, uri, map[string]any{"tokensOnly": true})
	if err != nil {
		t.Fatalf("embedBuffer tokensOnly: %v", err)
	}
	if len(res.Blocks) != 1 {
		t.Fatalf("want exactly 1 block for .ipmt URI, got %d", len(res.Blocks))
	}
	b := res.Blocks[0]
	if b.SVGBase64 != "" {
		t.Errorf("tokensOnly must not render an SVG for a .ipmt URI")
	}
	if b.Source != ipmtSource || len(b.Tokens) == 0 {
		t.Errorf("tokensOnly must still return source + tokens; got %+v", b)
	}
}

func TestEmbedBuffer_inlineIpmtPiggybacksTokens(t *testing.T) {
	// `<!--ipmt-->` + backtick pairs in prose MUST surface as
	// per-fragment entries with semantic tokens, even when no fenced
	// ```ipmt``` block is present. The client keys these by source
	// string and applies them to matching code_inline tokens.
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	content := "# colors\n\nA small color taxonomy (<!--ipmt-->`t-shirt ::c`, <!--ipmt-->`black ::c`).\n"
	uri := makeMdInTmpRepo(t, "README.md", content)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "markdown", Version: 1, Text: content,
		},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatalf("embedBuffer: %v", err)
	}
	if len(res.Blocks) != 0 {
		t.Fatalf("want 0 fenced blocks, got %d", len(res.Blocks))
	}
	if len(res.InlineIpmt) != 2 {
		t.Fatalf("want 2 inline fragments, got %d: %+v", len(res.InlineIpmt), res.InlineIpmt)
	}
	if res.InlineIpmt[0].Source != "t-shirt ::c" {
		t.Errorf("frag 0 source = %q, want %q", res.InlineIpmt[0].Source, "t-shirt ::c")
	}
	if res.InlineIpmt[1].Source != "black ::c" {
		t.Errorf("frag 1 source = %q, want %q", res.InlineIpmt[1].Source, "black ::c")
	}
	for i, f := range res.InlineIpmt {
		if len(f.Tokens) == 0 {
			t.Errorf("frag %d (%q) has no tokens", i, f.Source)
			continue
		}
		// Each fragment is "<ident> ::c" — expect ipmConcept token for
		// the identifier and an ipmConcept-with-marker for `::c`.
		var sawConcept bool
		for _, tk := range f.Tokens {
			if tk.Type == "ipmConcept" {
				sawConcept = true
				break
			}
		}
		if !sawConcept {
			t.Errorf("frag %d (%q) missing ipmConcept token; types: %v",
				i, f.Source, tokenTypeSummary(f.Tokens))
		}
	}
}

func TestEmbedBuffer_inlineIpmtAsToken(t *testing.T) {
	// `<!--ipmt:as-token:e-marker-->`(orange)`` — the server returns a
	// single token spanning the whole visible "(orange)" with the
	// (type, mods) that `e-marker` resolves to (ipmEvent + marker),
	// keyed by the visible source.
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}

	content := "# m\n\nEvent color is <!--ipmt:as-token:e-marker-->`(orange)` here.\n"
	uri := makeMdInTmpRepo(t, "README.md", content)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "markdown", Version: 1, Text: content,
		},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatalf("embedBuffer: %v", err)
	}
	if len(res.InlineIpmt) != 1 {
		t.Fatalf("want 1 inline fragment, got %d: %+v", len(res.InlineIpmt), res.InlineIpmt)
	}
	f := res.InlineIpmt[0]
	if f.Source != "(orange)" {
		t.Errorf("Source = %q, want %q", f.Source, "(orange)")
	}
	if len(f.Tokens) != 1 {
		t.Fatalf("want exactly 1 token, got %d: %+v", len(f.Tokens), f.Tokens)
	}
	tk := f.Tokens[0]
	const modMarker = uint32(1 << 6)
	if tk.StartCol != 0 || tk.Length != 8 {
		t.Errorf("token should span whole visible text [0,8); got col=%d len=%d", tk.StartCol, tk.Length)
	}
	if tk.Type != "ipmEvent" || tk.Mods&modMarker == 0 {
		t.Errorf("e-marker should resolve to ipmEvent+marker; got type=%s mods=%d", tk.Type, tk.Mods)
	}
}

func TestEmbedBuffer_noIpmtBlocksReturnsEmpty(t *testing.T) {
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{}, &lsp.InitializeResult{}); err != nil {
		t.Fatal(err)
	}
	content := "# just markdown\n\nno ipmt here.\n"
	uri := makeMdInTmpRepo(t, "README.md", content)
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: lsp.DocumentURI(uri), LanguageID: "markdown", Version: 1, Text: content,
		},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := embedBufferViaLSP(t, cli, uri)
	if err != nil {
		t.Fatalf("embedBuffer: %v", err)
	}
	if len(res.Blocks) != 0 {
		t.Fatalf("want 0 blocks for ipmt-free doc, got %d", len(res.Blocks))
	}
}
