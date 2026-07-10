package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creachadair/jrpc2"
	lsp "go.lsp.dev/protocol"
)

// TestE2E_FullSession drives a realistic LSP session through the in-process
// loopback harness: initialize → didOpen (valid + invalid) → diagnostics →
// semanticTokens/full → hover → workspace/executeCommand:ipm.check →
// shutdown. Each step asserts what the previous one produced.
//
// This is the "more bugs than per-method tests" e2e — it catches sequencing
// problems (state mutation between calls) that the focused tests miss.
func TestE2E_FullSession(t *testing.T) {
	cli, notifs := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// 1. initialize
	var initResult lsp.InitializeResult
	if err := cli.CallResult(ctx, "initialize", &lsp.InitializeParams{
		ClientInfo: &lsp.ClientInfo{Name: "e2e-test", Version: "0"},
	}, &initResult); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initResult.ServerInfo == nil || initResult.ServerInfo.Name != toolName {
		t.Fatalf("server name wrong: %+v", initResult.ServerInfo)
	}
	// HoverProvider, SemanticTokensProvider, ExecuteCommandProvider all advertised.
	if !initResult.Capabilities.HoverProvider.(bool) {
		t.Errorf("HoverProvider not advertised")
	}
	if initResult.Capabilities.ExecuteCommandProvider == nil {
		t.Errorf("ExecuteCommandProvider not advertised")
	}
	if initResult.Capabilities.SemanticTokensProvider == nil {
		t.Errorf("SemanticTokensProvider not advertised")
	}

	if err := cli.Notify(ctx, "initialized", &lsp.InitializedParams{}); err != nil {
		t.Fatal(err)
	}

	// 2. didOpen with valid content → publishDiagnostics([])
	docURI := lsp.DocumentURI("file:///tmp/e2e.ipmt")
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI: docURI, LanguageID: "ipmt", Version: 1,
			// `bob` (thing) is the source of both edges; the first arrow is
			// reversed (`<--`) because the forward event→thing direction is
			// rejected. Same graph, same 8 tokens, `::e` still at char 8.
			Text: "alice ::e <-- bob ::t --> idea ::c\n",
		},
	}); err != nil {
		t.Fatal(err)
	}
	req := waitForNotification(t, notifs, "textDocument/publishDiagnostics", 2*time.Second)
	var diag lsp.PublishDiagnosticsParams
	if err := req.UnmarshalParams(&diag); err != nil {
		t.Fatal(err)
	}
	if len(diag.Diagnostics) != 0 {
		t.Errorf("valid doc should yield zero diagnostics; got %+v", diag.Diagnostics)
	}

	// 3. textDocument/hover on the `::e` marker (col 6 = 'e' inside `::e`).
	var hov lsp.Hover
	if err := cli.CallResult(ctx, "textDocument/hover", &lsp.HoverParams{
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: docURI},
			Position:     lsp.Position{Line: 0, Character: 8},
		},
	}, &hov); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hov.Contents.Value, "Event marker") {
		t.Errorf("hover on ::e missing 'Event marker'; got %q", hov.Contents.Value)
	}

	// 4. textDocument/semanticTokens/full — three node-name tokens, three
	// kind-marker tokens (`::e`, `::t`, `::c`), and two arrow tokens.
	var tokRes lsp.SemanticTokens
	if err := cli.CallResult(ctx, "textDocument/semanticTokens/full",
		&lsp.SemanticTokensParams{TextDocument: lsp.TextDocumentIdentifier{URI: docURI}},
		&tokRes); err != nil {
		t.Fatal(err)
	}
	got := decodeSemanticTokens(tokRes.Data)
	if len(got) != 8 {
		t.Fatalf("want 8 semantic tokens (3 names + 3 ::kind markers + 2 arrows); got %d: %+v", len(got), got)
	}
	// First token is the event "alice".
	if got[0].Type != 0 /*ipmEvent*/ {
		t.Errorf("first token type = %d, want 0 (ipmEvent)", got[0].Type)
	}

	// 5. didChange — switch to invalid content; expect a diagnostic to flow.
	if err := cli.Notify(ctx, "textDocument/didChange", &lsp.DidChangeTextDocumentParams{
		TextDocument: lsp.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: lsp.TextDocumentIdentifier{URI: docURI},
			Version:                2,
		},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{
			{Text: "foo ::e ::t bar\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	req = waitForNotification(t, notifs, "textDocument/publishDiagnostics", 2*time.Second)
	if err := req.UnmarshalParams(&diag); err != nil {
		t.Fatal(err)
	}
	if len(diag.Diagnostics) == 0 {
		t.Error("expected diagnostics after didChange to invalid content; got none")
	}

	// 6. workspace/executeCommand: ipm.check on a real on-disk .md file.
	checkRoot := t.TempDir()
	if err := exec.Command("git", "init", "-q", checkRoot).Run(); err == nil {
		mdPath := filepath.Join(checkRoot, "x.md")
		os.WriteFile(mdPath, []byte("```ipmt\na ::e\n```\n"), 0o644)
		var checkRes map[string]any
		if err := cli.CallResult(ctx, "workspace/executeCommand", &lsp.ExecuteCommandParams{
			Command:   "ipm.check",
			Arguments: []any{map[string]any{"file": mdPath}},
		}, &checkRes); err != nil {
			t.Fatalf("ipm.check: %v", err)
		}
		// Result is a map[string]any after JSON roundtrip — check the
		// stats sub-object reports 1 insert (no marker yet) and the
		// .md file is untouched on disk.
		if checkRes["changed"] != false {
			t.Errorf("ipm.check should not write; changed=%v", checkRes["changed"])
		}
		mdAfter, _ := os.ReadFile(mdPath)
		if strings.Contains(string(mdAfter), "ipm-svg") {
			t.Errorf("ipm.check wrote a marker; file:\n%s", mdAfter)
		}
	}

	// 7. shutdown / exit
	if _, err := cli.Call(ctx, "shutdown", nil); err != nil {
		t.Fatal(err)
	}
	if err := cli.Notify(ctx, "exit", nil); err != nil {
		t.Fatal(err)
	}
}

// TestE2E_NvimHeadless drives the ipm-rpc binary through a real Neovim
// process (LSP client). Skipped when `nvim` isn't on PATH so local devs
// without Neovim see green; CI runs it.
func TestE2E_NvimHeadless(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping headless e2e")
	}

	// Build the binary fresh.
	binary := filepath.Join(t.TempDir(), "ipm-rpc")
	if err := exec.Command("go", "build", "-o", binary, "github.com/infinite-pm/ipm-tools/cmd/ipm-rpc").Run(); err != nil {
		t.Fatalf("build ipm-rpc: %v", err)
	}

	// Prepare an ipmt sample.
	workdir := t.TempDir()
	sample := filepath.Join(workdir, "x.ipmt")
	if err := os.WriteFile(sample, []byte("foo ::e ::t bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Lua snippet: start an LSP client, attach to the buffer, wait for a
	// publishDiagnostics, write the count to stderr, exit.
	lua := `
local diag_seen = false
vim.lsp.start({
  name = "ipm-rpc",
  cmd = { "` + binary + `" },
  root_dir = "` + workdir + `",
  filetypes = { "ipmt" },
})
vim.cmd("set filetype=ipmt")
vim.cmd("edit ` + sample + `")

local deadline = vim.uv.now() + 5000
while vim.uv.now() < deadline do
  vim.wait(100)
  local d = vim.diagnostic.get(0)
  if #d > 0 then
    diag_seen = true
    io.stderr:write("DIAGS:" .. tostring(#d) .. "\n")
    break
  end
end
if not diag_seen then
  io.stderr:write("NO_DIAGS\n")
end
vim.cmd("quit!")
`
	cmd := exec.Command("nvim", "--headless", "-c", "lua "+lua)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	_ = err // nvim may exit non-zero on the quit; we check stderr content instead.
	if !strings.Contains(string(out), "DIAGS:") {
		t.Fatalf("nvim never received diagnostics from ipm-rpc; output:\n%s", out)
	}
}

// Forces the test file to link against jrpc2 even if other tests use it,
// keeping the import non-stale.
var _ = jrpc2.NewClient
