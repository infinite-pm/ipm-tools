package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	lsp "go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	iplog "github.com/infinite-pm/ipm-tools/pkg/ipm/log"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/lspos"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/validate"
	"github.com/infinite-pm/ipm-tools/pkg/ipmtmeta"
	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
	ipmd "github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// server is the LSP server's runtime state.
//
// Phase 1 surface is intentionally minimal: maintain the open-document set,
// parse + validate on every change, push diagnostics. Hover / completion /
// semantic tokens / workspace commands come in subsequent commits.
//
// Concurrency note: jrpc2 dispatches each request on its own goroutine. The
// document cache is protected with a mutex; per-document parse + validate
// work happens inside the request goroutine. A real "snapshot per change"
// model (gopls trick #3) would split that into "store snapshot under lock"
// and "parse off the lock" — that refinement lands when the simple version
// shows contention.
type server struct {
	log *slog.Logger

	mu   sync.Mutex
	docs map[lsp.DocumentURI]*document

	jrpcMu sync.Mutex
	jrpc   *jrpc2.Server

	// lifecycleMu guards the LSP shutdown/exit state below.
	lifecycleMu sync.Mutex
	// shutdownReceived records whether a `shutdown` request arrived before
	// `exit`. The LSP spec requires the process to exit 0 only when it did.
	shutdownReceived bool
	// exitCode is the process exit status the `exit` handler selected: 0 when
	// a prior `shutdown` was seen, otherwise 1. main() reads it after Wait()
	// returns and exits the process with that code.
	exitCode int

	// notifyOverride, when non-nil, replaces the jrpc2-backed notify path.
	// Tests use it to capture the notifications the server would have
	// pushed to a client without standing up a real transport.
	notifyOverride func(method string, params any)
}

// document holds the current state of one open file.
type document struct {
	uri      lsp.DocumentURI
	version  int32
	text     string
	language string
}

func newServer(log *slog.Logger) *server {
	return &server{
		log:  log,
		docs: map[lsp.DocumentURI]*document{},
	}
}

// attachJRPC gives the server the back-channel it needs to push notifications
// (publishDiagnostics) to the client.
func (s *server) attachJRPC(j *jrpc2.Server) {
	s.jrpcMu.Lock()
	defer s.jrpcMu.Unlock()
	s.jrpc = j
}

// assignedHandlers returns the jrpc2 method dispatch table. Each handler is a
// thin wrapper that decodes the params, calls the typed method below, and
// returns the result (or nil for notifications).
func (s *server) assignedHandlers() jrpc2.Assigner {
	m := handler.Map{
		"initialize":  handler.New(s.initialize),
		"initialized": handler.New(s.initialized),
		"shutdown":    handler.New(s.shutdown),
		"exit":        handler.New(s.exit),

		"textDocument/didOpen":             handler.New(s.didOpen),
		"textDocument/didChange":           handler.New(s.didChange),
		"textDocument/didClose":            handler.New(s.didClose),
		"textDocument/hover":               handler.New(s.hover),
		"textDocument/semanticTokens/full": handler.New(s.semanticTokensFull),

		"workspace/executeCommand": handler.New(s.executeCommand),
	}
	return m
}

// initialize advertises the server's capabilities. We pick a deliberately
// conservative set: only what we actually implement, per gopls trick #13.
func (s *server) initialize(ctx context.Context, params *lsp.InitializeParams) (*lsp.InitializeResult, error) {
	s.log.Info("client connected",
		slog.String("client", clientName(params)),
		slog.Int("root_uris", len(params.WorkspaceFolders)),
	)
	syncFull := lsp.TextDocumentSyncKindFull // prereq B: full text sync.
	return &lsp.InitializeResult{
		Capabilities: lsp.ServerCapabilities{
			TextDocumentSync: lsp.TextDocumentSyncOptions{
				OpenClose: true,
				Change:    syncFull,
			},
			ExecuteCommandProvider: &lsp.ExecuteCommandOptions{
				Commands: []string{"ipm.embed", "ipm.check", "ipm.embedBuffer"},
			},
			HoverProvider: true,
			// go.lsp.dev/protocol types SemanticTokensProvider as interface{}
			// with a TODO; build the JSON shape directly to match the LSP
			// spec (SemanticTokensOptions with Legend + full=true).
			SemanticTokensProvider: map[string]any{
				"legend": map[string]any{
					"tokenTypes":     ipmtokens.SemanticTokenTypes,
					"tokenModifiers": ipmtokens.SemanticTokenModifiers,
				},
				"full": true,
			},
		},
		ServerInfo: &lsp.ServerInfo{
			Name:    toolName,
			Version: versionString(),
		},
	}, nil
}

func (s *server) initialized(ctx context.Context, params *lsp.InitializedParams) error {
	s.log.Debug("client initialized")
	return nil
}

func (s *server) shutdown(ctx context.Context) error {
	s.log.Debug("client requested shutdown")
	s.lifecycleMu.Lock()
	s.shutdownReceived = true
	s.lifecycleMu.Unlock()
	return nil
}

func (s *server) exit(ctx context.Context) error {
	// Per the LSP spec, the server exits with success (0) only if a `shutdown`
	// request was received first; otherwise it exits with an error (1). Record
	// the selected code here and let main() apply it after Wait() returns.
	s.lifecycleMu.Lock()
	if s.shutdownReceived {
		s.exitCode = 0
	} else {
		s.exitCode = 1
	}
	code := s.exitCode
	s.lifecycleMu.Unlock()
	s.log.Debug("client requested exit", slog.Int("code", code))

	// Stopping the jrpc2 server triggers the Wait() in main to return,
	// which is how we exit cleanly.
	s.jrpcMu.Lock()
	defer s.jrpcMu.Unlock()
	if s.jrpc != nil {
		go s.jrpc.Stop()
	}
	return nil
}

// exitStatus reports the process exit code selected by the `exit` handler.
// It returns 0 when no `exit` was processed (the server wound down for another
// reason, e.g. transport close or signal), matching the previous clean-exit
// behavior.
func (s *server) exitStatus() int {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	return s.exitCode
}

// didOpen stores the document and immediately publishes diagnostics. The
// editor will use the diagnostics to mark up the source in its UI.
func (s *server) didOpen(ctx context.Context, params *lsp.DidOpenTextDocumentParams) error {
	doc := &document{
		uri:      params.TextDocument.URI,
		version:  params.TextDocument.Version,
		text:     params.TextDocument.Text,
		language: string(params.TextDocument.LanguageID),
	}
	s.mu.Lock()
	s.docs[doc.uri] = doc
	s.mu.Unlock()

	log := iplog.With(ctx, s.log.With(slog.String("uri", string(doc.uri)), slog.String("lang", doc.language)))
	iplog.FromContext(log).Debug("textDocument/didOpen")

	s.publishDiagnostics(log, doc)
	return nil
}

// didChange applies the full-text update (we advertised sync=Full). Any
// incremental support would land here behind a switch on the change type.
func (s *server) didChange(ctx context.Context, params *lsp.DidChangeTextDocumentParams) error {
	s.mu.Lock()
	// Build a fresh document value and atomically swap it into the cache
	// rather than mutating the existing one in place. A concurrent request
	// (hover/semanticTokens) may have captured the previous *document pointer
	// under the lock and read its fields after releasing the lock; mutating
	// that struct here would race with those reads. Treating each version as
	// an immutable snapshot (the gopls "snapshot per change" model) removes
	// the race without holding the lock across parse/validate.
	prev, ok := s.docs[params.TextDocument.URI]
	doc := &document{uri: params.TextDocument.URI}
	if ok {
		// Carry forward the prior fields; the change below overrides those it
		// touches.
		*doc = *prev
	}
	// When the client sends didChange for a file we never saw didOpen for, doc
	// is left with an empty language field and URI-suffix detection applies.
	doc.version = params.TextDocument.Version
	if len(params.ContentChanges) > 0 {
		// In full-sync mode every change carries the entire new text.
		doc.text = params.ContentChanges[len(params.ContentChanges)-1].Text
	}
	s.docs[params.TextDocument.URI] = doc
	s.mu.Unlock()

	log := iplog.With(ctx, s.log.With(slog.String("uri", string(doc.uri))))
	iplog.FromContext(log).Debug("textDocument/didChange",
		slog.Int("version", int(doc.version)),
		slog.Int("size", len(doc.text)),
	)
	s.publishDiagnostics(log, doc)
	return nil
}

func (s *server) didClose(ctx context.Context, params *lsp.DidCloseTextDocumentParams) error {
	s.mu.Lock()
	delete(s.docs, params.TextDocument.URI)
	s.mu.Unlock()

	log := iplog.With(ctx, s.log.With(slog.String("uri", string(params.TextDocument.URI))))
	iplog.FromContext(log).Debug("textDocument/didClose")

	// Clear stale diagnostics by publishing an empty list.
	s.notify(log, "textDocument/publishDiagnostics", &lsp.PublishDiagnosticsParams{
		URI:         params.TextDocument.URI,
		Diagnostics: []lsp.Diagnostic{},
	})
	return nil
}

// publishDiagnostics parses + validates the document and pushes findings
// back to the client. Two paths:
//
//   - Pure ipmt (.ipmt files, or LanguageID="ipmt"): parse the whole text.
//   - Markdown (.md): scan for ```ipmt fences and process each block
//     individually, offsetting block-local positions to whole-file
//     coordinates so the editor highlights the right lines.
//
// Other documents (plain markdown with no ipmt content, anything else)
// publish an empty list to clear any stale state.
func (s *server) publishDiagnostics(ctx context.Context, doc *document) {
	log := iplog.FromContext(ctx)
	out := &lsp.PublishDiagnosticsParams{
		URI:         doc.uri,
		Diagnostics: []lsp.Diagnostic{},
	}

	switch {
	case looksLikeIPMT(doc):
		s.diagnosePureIPMT(ctx, doc, out)
	case looksLikeMarkdown(doc):
		s.diagnoseMarkdownIPMT(ctx, doc, out)
	}
	log.Debug("publishing diagnostics", slog.Int("count", len(out.Diagnostics)))
	s.notify(ctx, "textDocument/publishDiagnostics", out)
}

func (s *server) diagnosePureIPMT(ctx context.Context, doc *document, out *lsp.PublishDiagnosticsParams) {
	log := iplog.FromContext(ctx)
	mapper := lspos.NewMapper(doc.text)
	// A misplaced/unknown `# ipmt:` pragma is surfaced as a diagnostic on the
	// first line; parse+validate still run (the pragma is an ipmt comment).
	if _, merr := ipmtmeta.MetaFromContent(doc.text); merr != nil {
		out.Diagnostics = append(out.Diagnostics, lsp.Diagnostic{
			Severity: lsp.DiagnosticSeverityError,
			Source:   toolName,
			Message:  "invalid ipmt metadata: " + merr.Error(),
			Range:    lineRange(0),
		})
	}
	graph, err := parser.ParseIPMTBytes([]byte(doc.text), string(doc.uri))
	if err != nil {
		out.Diagnostics = append(out.Diagnostics, parseErrorToDiagnostic(err, mapper))
		log.Debug("parse error", slog.String("err", err.Error()))
		return
	}
	for _, f := range validate.Run(graph) {
		out.Diagnostics = append(out.Diagnostics, findingToDiagnostic(f, mapper))
	}
}

// diagnoseMarkdownIPMT scans the markdown body for ```ipmt fenced blocks,
// runs parse + validate against each, and emits whole-file LSP diagnostics.
//
// Block-local line numbers are offset by (blockStartLine + 1) — the block
// content begins on the line right after the opening fence. Column numbers
// pass through unchanged because each block starts at column 0 (the markdown
// fence requires it).
func (s *server) diagnoseMarkdownIPMT(ctx context.Context, doc *document, out *lsp.PublishDiagnosticsParams) {
	log := iplog.FromContext(ctx)
	_, blocks := ipmd.ScanIPMTBlocks(doc.text)
	if len(blocks) == 0 {
		return
	}
	for _, blk := range blocks {
		// Skip unterminated fences cleanly — the parser would emit a noisy
		// "unexpected EOF" error against partial content. Surface as a
		// diagnostic on the opening fence line instead.
		if blk.EndLine < 0 {
			out.Diagnostics = append(out.Diagnostics, lsp.Diagnostic{
				Severity: lsp.DiagnosticSeverityError,
				Source:   toolName,
				Message:  "unterminated ```ipmt fence",
				Range:    lineRange(uint32(blk.StartLine)),
			})
			continue
		}
		blkMapper := lspos.NewMapper(blk.Content)
		// First content line of this block in the WHOLE-FILE coordinate
		// system. blocks count from the opening fence (StartLine), so the
		// first content line is StartLine+1.
		fileLineOffset := uint32(blk.StartLine + 1)

		// A misplaced/unknown flag (fence token or `# ipmt:` pragma) is a
		// diagnostic on the opening fence line.
		if _, merr := ipmtmeta.EffectiveMeta(blk.Meta, blk.Content); merr != nil {
			out.Diagnostics = append(out.Diagnostics, lsp.Diagnostic{
				Severity: lsp.DiagnosticSeverityError,
				Source:   toolName,
				Message:  "invalid ipmt metadata: " + merr.Error(),
				Range:    lineRange(uint32(blk.StartLine)),
			})
			continue
		}

		graph, err := parser.ParseIPMTBytes([]byte(blk.Content), string(doc.uri))
		if err != nil {
			d := parseErrorToDiagnostic(err, blkMapper)
			d.Range = shiftRange(d.Range, fileLineOffset)
			out.Diagnostics = append(out.Diagnostics, d)
			log.Debug("md block parse error",
				slog.Int("block", blk.Index),
				slog.String("err", err.Error()))
			continue
		}
		for _, f := range validate.Run(graph) {
			d := findingToDiagnostic(f, blkMapper)
			d.Range = shiftRange(d.Range, fileLineOffset)
			out.Diagnostics = append(out.Diagnostics, d)
		}
	}
}

// lineRange returns a zero-character range that covers just the start of
// the given (whole-file) line. Used for whole-line diagnostics where we
// don't have a precise column.
func lineRange(line uint32) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: line, Character: 0},
		End:   lsp.Position{Line: line, Character: 0},
	}
}

// shiftRange offsets a block-local LSP range so its line numbers become
// whole-file line numbers.
func shiftRange(r lsp.Range, lineOffset uint32) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: r.Start.Line + lineOffset, Character: r.Start.Character},
		End:   lsp.Position{Line: r.End.Line + lineOffset, Character: r.End.Character},
	}
}

// looksLikeMarkdown returns true for documents the LSP server should treat
// as markdown-with-possibly-ipmt: the LanguageID is "markdown" or the URI
// ends in .md / .markdown.
func looksLikeMarkdown(doc *document) bool {
	if doc.language == "markdown" {
		return true
	}
	u, err := uri.Parse(string(doc.uri))
	if err != nil {
		return false
	}
	name := strings.ToLower(u.Filename())
	return strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown")
}

// notify sends a notification on the jrpc2 server back-channel. Silent if
// the server isn't attached yet. Tests intercept via notifyOverride.
func (s *server) notify(ctx context.Context, method string, params any) {
	if s.notifyOverride != nil {
		s.notifyOverride(method, params)
		return
	}
	s.jrpcMu.Lock()
	j := s.jrpc
	s.jrpcMu.Unlock()
	if j == nil {
		return
	}
	if err := j.Notify(ctx, method, params); err != nil {
		iplog.FromContext(ctx).Warn("notify failed", slog.String("method", method), slog.String("err", err.Error()))
	}
}

func looksLikeIPMT(doc *document) bool {
	switch doc.language {
	case "ipmt":
		return true
	case "markdown":
		// Phase 2: scan for ipmt fences. For now: no diagnostics from
		// markdown documents.
		return false
	}
	u, err := uri.Parse(string(doc.uri))
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Filename()), ".ipmt")
}

// parseErrorToDiagnostic converts a parser.ParseError (or generic error) into
// an LSP Diagnostic. Byte offsets from the parser are converted to LSP
// (line, character) UTF-16 positions using the document's mapper.
func parseErrorToDiagnostic(err error, mapper *lspos.Mapper) lsp.Diagnostic {
	d := lsp.Diagnostic{
		Severity: lsp.DiagnosticSeverityError,
		Source:   toolName,
		Message:  err.Error(),
	}
	var pe *parser.ParseError
	if errors.As(err, &pe) {
		sl, sc, el, ec := mapper.OffsetRangeToUTF16(pe.Start, pe.End)
		d.Range = lsp.Range{
			Start: lsp.Position{Line: sl, Character: sc},
			End:   lsp.Position{Line: el, Character: ec},
		}
	}
	return d
}

func findingToDiagnostic(f validate.Finding, mapper *lspos.Mapper) lsp.Diagnostic {
	// Validator's Line/Column are 1-based UTF-8. LSP wants 0-based UTF-16.
	// We use the mapper to convert via the byte offset of the column on the
	// validator's line, which gets us UTF-16 correctness for non-ASCII.
	var line, col uint32
	if f.Line > 0 {
		// Compute byte offset of (line, col) in the source text by walking
		// the mapper's line table and adding (col-1) bytes. This is still
		// approximate for non-ASCII *columns* (validator uses UTF-8 byte
		// columns), but the conversion through Mapper.OffsetToUTF16 gives
		// the right LSP-side coordinate.
		byteOff := mapper.LineColUTF16ToOffset(uint32(f.Line-1), 0) + max(0, f.Column-1)
		line, col = mapper.OffsetToUTF16(byteOff)
	}
	return lsp.Diagnostic{
		Severity: severityToLSP(f.Severity),
		Source:   toolName,
		Code:     f.Code,
		Message:  f.Message,
		Range: lsp.Range{
			Start: lsp.Position{Line: line, Character: col},
			// Without an explicit end position from the validator, render
			// a zero-width diagnostic at the anchor. The editor highlights
			// the token at that point.
			End: lsp.Position{Line: line, Character: col},
		},
	}
}

func severityToLSP(s validate.Severity) lsp.DiagnosticSeverity {
	switch s {
	case validate.SeverityError:
		return lsp.DiagnosticSeverityError
	case validate.SeverityWarning:
		return lsp.DiagnosticSeverityWarning
	case validate.SeverityInfo:
		return lsp.DiagnosticSeverityInformation
	default:
		return lsp.DiagnosticSeverityHint
	}
}

// clientName extracts a printable identifier from the initialize params for
// logging. Falls back to "(unknown)" so logs stay structured even when the
// client doesn't supply a ClientInfo block.
func clientName(p *lsp.InitializeParams) string {
	if p != nil && p.ClientInfo != nil {
		if p.ClientInfo.Version != "" {
			return fmt.Sprintf("%s/%s", p.ClientInfo.Name, p.ClientInfo.Version)
		}
		return p.ClientInfo.Name
	}
	return "(unknown)"
}
