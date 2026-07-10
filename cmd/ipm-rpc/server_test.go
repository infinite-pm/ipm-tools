package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	lsp "go.lsp.dev/protocol"

	iplog "github.com/infinite-pm/ipm-tools/pkg/ipm/log"
)

// startTestServer wires the server against an in-process direct channel and
// returns a jrpc2.Client connected to it. The caller is responsible for
// closing the client; the server stops automatically when the client closes.
//
// The returned `notifs` channel receives every notification the server pushes
// to the client (e.g. textDocument/publishDiagnostics). Tests can drain it
// to assert on what the server published.
func startTestServer(t *testing.T) (cli *jrpc2.Client, notifs <-chan *jrpc2.Request) {
	t.Helper()

	iplog.SetupDiscard()
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)))

	serverCh, clientCh := channel.Direct()
	s := jrpc2.NewServer(srv.assignedHandlers(), &jrpc2.ServerOptions{
		AllowPush: true,
	}).Start(serverCh)
	srv.attachJRPC(s)

	notifCh := make(chan *jrpc2.Request, 16)
	cli = jrpc2.NewClient(clientCh, &jrpc2.ClientOptions{
		OnNotify: func(r *jrpc2.Request) {
			// Non-blocking send so a slow consumer doesn't deadlock the test.
			select {
			case notifCh <- r:
			default:
			}
		},
	})
	t.Cleanup(func() {
		// Closing the client also closes the underlying channel; that's what
		// signals the server to wind down.
		_ = cli.Close()
		_ = s.Wait()
		close(notifCh)
	})
	return cli, notifCh
}

func TestInitialize_capabilitiesAdvertised(t *testing.T) {
	cli, _ := startTestServer(t)
	var resp lsp.InitializeResult
	if err := cli.CallResult(t.Context(), "initialize", &lsp.InitializeParams{
		ProcessID: 0,
		ClientInfo: &lsp.ClientInfo{
			Name:    "test-client",
			Version: "0.0.0",
		},
	}, &resp); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Phase 1 only advertises text-document sync. Anything else would be
	// over-claiming.
	syncOpts, ok := resp.Capabilities.TextDocumentSync.(map[string]any)
	if !ok {
		// The server may return the typed struct directly; jrpc2's JSON
		// roundtrip turns it into a map. Try unmarshaling the raw form.
		raw, _ := json.Marshal(resp.Capabilities.TextDocumentSync)
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		syncOpts = parsed
	}
	if syncOpts["openClose"] != true {
		t.Fatalf("expected openClose=true; got %v", syncOpts)
	}
	if change, ok := syncOpts["change"].(float64); !ok || int(change) != int(lsp.TextDocumentSyncKindFull) {
		t.Fatalf("expected change=Full(%d); got %v", int(lsp.TextDocumentSyncKindFull), syncOpts["change"])
	}
	if resp.ServerInfo == nil || resp.ServerInfo.Name != toolName {
		t.Fatalf("ServerInfo wrong: %+v", resp.ServerInfo)
	}
}

func TestDidOpen_storesDocument(t *testing.T) {
	cli, _ := startTestServer(t)
	ctx := t.Context()
	if _, err := cli.Call(ctx, "initialize", &lsp.InitializeParams{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := cli.Notify(ctx, "initialized", &lsp.InitializedParams{}); err != nil {
		t.Fatalf("initialized: %v", err)
	}

	// The client just needs to not error. Diagnostics are pushed as a
	// notification; we don't capture them here (that's the next test).
	err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI:        lsp.DocumentURI("file:///tmp/x.ipmt"),
			LanguageID: "ipmt",
			Version:    1,
			Text:       "a ::e --> b ::e\n",
		},
	})
	if err != nil {
		t.Fatalf("didOpen notify: %v", err)
	}
}

func TestDidOpen_publishesParseErrorOnGarbage(t *testing.T) {
	cli, notifs := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if _, err := cli.Call(ctx, "initialize", &lsp.InitializeParams{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// Open an ipmt document with input that the parser will reject. A
	// truly invalid construct is needed since the parser is lenient with
	// bare identifiers — use a malformed type marker.
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI:        lsp.DocumentURI("file:///tmp/bad.ipmt"),
			LanguageID: "ipmt",
			Version:    1,
			Text:       "foo ::e ::t bar\n", // multiple types on one node — invalid
		},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	// Wait for the server to push publishDiagnostics back to us.
	req := waitForNotification(t, notifs, "textDocument/publishDiagnostics", 2*time.Second)
	var p lsp.PublishDiagnosticsParams
	if err := req.UnmarshalParams(&p); err != nil {
		t.Fatalf("unmarshal publishDiagnostics: %v", err)
	}
	if string(p.URI) != "file:///tmp/bad.ipmt" {
		t.Errorf("diagnostic URI = %q, want bad.ipmt", p.URI)
	}
	if len(p.Diagnostics) == 0 {
		t.Fatal("expected at least one diagnostic for invalid input")
	}
}

func TestDidOpen_publishesEmptyDiagnosticsOnValid(t *testing.T) {
	cli, notifs := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := cli.Call(ctx, "initialize", &lsp.InitializeParams{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := cli.Notify(ctx, "textDocument/didOpen", &lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI:        lsp.DocumentURI("file:///tmp/good.ipmt"),
			LanguageID: "ipmt",
			Version:    1,
			Text:       "a ::e --> b ::e\n",
		},
	}); err != nil {
		t.Fatal(err)
	}
	req := waitForNotification(t, notifs, "textDocument/publishDiagnostics", 2*time.Second)
	var p lsp.PublishDiagnosticsParams
	if err := req.UnmarshalParams(&p); err != nil {
		t.Fatal(err)
	}
	if len(p.Diagnostics) != 0 {
		t.Errorf("valid input should yield zero diagnostics; got %+v", p.Diagnostics)
	}
}

// waitForNotification drains notifs until it sees a request for the given
// method (or times out). Lets a test wait for the *specific* notification
// it cares about without choking on unrelated traffic.
func waitForNotification(t *testing.T, notifs <-chan *jrpc2.Request, method string, timeout time.Duration) *jrpc2.Request {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case req := <-notifs:
			if req == nil {
				t.Fatalf("notifications channel closed before %q", method)
			}
			if req.Method() == method {
				return req
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %q notification", method)
			return nil
		}
	}
}

// TestDidChange_withoutPriorDidOpen exercises the synthesis path in
// didChange: a client sends didChange for a URI it never opened (e.g. it
// crashed mid-stream and replays). The server should synthesize a fresh
// document and still publish diagnostics, relying on URI-suffix language
// detection since the synthesized doc has an empty language field.
func TestDidChange_withoutPriorDidOpen(t *testing.T) {
	srv := newCmdTestServer(t)

	var published []*lsp.PublishDiagnosticsParams
	srv.notifyOverride = func(method string, params any) {
		if method != "textDocument/publishDiagnostics" {
			return
		}
		if p, ok := params.(*lsp.PublishDiagnosticsParams); ok {
			published = append(published, p)
		}
	}

	uri := lsp.DocumentURI("file:///tmp/never-opened.ipmt")
	// Invalid input (two type markers on one node) so suffix-based detection
	// must kick in and produce at least one diagnostic.
	err := srv.didChange(t.Context(), &lsp.DidChangeTextDocumentParams{
		TextDocument: lsp.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: lsp.TextDocumentIdentifier{URI: uri},
			Version:                1,
		},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{
			{Text: "foo ::e ::t bar\n"},
		},
	})
	if err != nil {
		t.Fatalf("didChange: %v", err)
	}

	// The doc must have been synthesized and cached.
	srv.mu.Lock()
	doc, ok := srv.docs[uri]
	srv.mu.Unlock()
	if !ok {
		t.Fatal("expected didChange to synthesize a document for the never-opened URI")
	}
	if doc.language != "" {
		t.Errorf("synthesized doc language = %q, want empty (suffix detection)", doc.language)
	}

	if len(published) == 0 {
		t.Fatal("expected publishDiagnostics to fire for synthesized doc")
	}
	last := published[len(published)-1]
	if string(last.URI) != string(uri) {
		t.Errorf("diagnostic URI = %q, want %q", last.URI, uri)
	}
	if len(last.Diagnostics) == 0 {
		t.Fatal("expected at least one diagnostic for invalid input via suffix detection")
	}
}

// TestServer_concurrentDidChangeWithReads interleaves didChange with
// concurrent hover and semanticTokens requests on a single URI. Run with
// `go test -race` to surface unsynchronized access to the document cache.
func TestServer_concurrentDidChangeWithReads(t *testing.T) {
	srv := newCmdTestServer(t)
	uri := lsp.DocumentURI("file:///tmp/race.ipmt")
	srv.docs[uri] = &document{
		uri:      uri,
		language: "ipmt",
		text:     "alice ::e --> bob ::e\n",
	}

	ctx := t.Context()
	const iters = 200
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = srv.didChange(ctx, &lsp.DidChangeTextDocumentParams{
				TextDocument: lsp.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: lsp.TextDocumentIdentifier{URI: uri},
					Version:                int32(i + 2),
				},
				ContentChanges: []lsp.TextDocumentContentChangeEvent{
					{Text: "alice ::e --> bob ::e\n"},
				},
			})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_, _ = srv.hover(ctx, &lsp.HoverParams{
				TextDocumentPositionParams: lsp.TextDocumentPositionParams{
					TextDocument: lsp.TextDocumentIdentifier{URI: uri},
					Position:     lsp.Position{Line: 0, Character: 7},
				},
			})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_, _ = srv.semanticTokensFull(ctx, &lsp.SemanticTokensParams{
				TextDocument: lsp.TextDocumentIdentifier{URI: uri},
			})
		}
	}()

	wg.Wait()
}

func TestShutdown_thenExit(t *testing.T) {
	cli, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if _, err := cli.Call(ctx, "initialize", &lsp.InitializeParams{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, err := cli.Call(ctx, "shutdown", nil); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	// exit is a notification — server stops itself.
	if err := cli.Notify(ctx, "exit", nil); err != nil {
		t.Fatalf("exit: %v", err)
	}
}

// TestExitCode verifies the LSP-spec exit-status selection in the exit
// handler: 0 when a shutdown request preceded exit, 1 otherwise. We drive the
// handlers directly (rather than through the jrpc2 client) so we can assert on
// exitStatus() without the process actually exiting — main() is the only place
// that calls os.Exit.
func TestExitCode(t *testing.T) {
	ctx := t.Context()

	t.Run("shutdown then exit -> 0", func(t *testing.T) {
		srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err := srv.shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
		if err := srv.exit(ctx); err != nil {
			t.Fatalf("exit: %v", err)
		}
		if got := srv.exitStatus(); got != 0 {
			t.Fatalf("exitStatus after shutdown+exit = %d, want 0", got)
		}
	})

	t.Run("exit without shutdown -> 1", func(t *testing.T) {
		srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err := srv.exit(ctx); err != nil {
			t.Fatalf("exit: %v", err)
		}
		if got := srv.exitStatus(); got != 1 {
			t.Fatalf("exitStatus after bare exit = %d, want 1", got)
		}
	})

	t.Run("no exit -> 0", func(t *testing.T) {
		srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
		if got := srv.exitStatus(); got != 0 {
			t.Fatalf("exitStatus with no exit processed = %d, want 0", got)
		}
	})
}
