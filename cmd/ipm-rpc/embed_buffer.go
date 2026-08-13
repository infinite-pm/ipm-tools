package main

// ipm.embedBuffer — live (pre-save) render of every ipmt block in an open
// document.
//
// Unlike ipm.embed, this command:
//   - reads the document content from the LSP server's in-memory state
//     (textDocument/didOpen + didChange) rather than from disk
//   - does NOT write SVGs to disk
//   - does NOT modify the .md
//   - returns the rendered SVG bytes as base64
//
// Consumed by the VS Code extension's live-refresh listener: on every
// debounced edit it asks the server for fresh SVGs and substitutes them
// into the preview as data URLs. The on-disk SVG + the .md's marker
// hash stay frozen at their last-saved state until the user actually
// saves and the existing ipm.embed flow kicks in.
//

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	lsp "go.lsp.dev/protocol"

	ipconfig "github.com/infinite-pm/ipm-tools/pkg/ipm/config"
	"github.com/infinite-pm/ipm-tools/pkg/ipmtmeta"
	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
	"github.com/infinite-pm/ipm-tools/pkg/mdembed"
)

// embedBufferResult is the response shape for ipm.embedBuffer. The client
// keys a per-URI cache by Uri + bufferBlock.Index.
type embedBufferResult struct {
	Uri        string               `json:"uri"`
	Blocks     []bufferBlock        `json:"blocks"`
	InlineIpmt []inlineIpmtFragment `json:"inlineIpmt,omitempty"`
	Warnings   []embedWarning       `json:"warnings,omitempty"`
}

// inlineIpmtFragment is one `<!--ipmt-->` + backtick code-span pair
// found in the markdown source. The client keys these by the `Source`
// string (== the inline code content) so its markdown-it rule can look
// up tokens for any code_inline whose source matches.
//
// Tokens are 0-based and relative to the start of Source (single line,
// since CommonMark inline code spans don't cross newlines).
type inlineIpmtFragment struct {
	Source string        `json:"source"`
	Tokens []bufferToken `json:"tokens,omitempty"`
}

type bufferBlock struct {
	Index     int           `json:"index"`               // 1-based positional index across all kinds in source order
	ID        string        `json:"id"`                  // marker id (positional 01, 02… or explicit)
	Hash      string        `json:"hash"`                // sha256 prefix of the normalized source
	Source    string        `json:"source,omitempty"`    // raw ipmt block content (no fences) — lets the client key tokens by source string
	SVGBase64 string        `json:"svgBase64,omitempty"` // base64 of rendered SVG bytes; empty on render error
	Outcome   string        `json:"outcome"`             // ok / insert-marker / rehash / rerender / …
	Error     string        `json:"error,omitempty"`     // populated when render failed (parse error, etc.)
	Tokens    []bufferToken `json:"tokens,omitempty"`    // syntax tokens, block-relative
}

// bufferToken is a block-relative semantic-token record so the markdown
// preview can apply the same coloring as the editor's LSP semantic
// tokens without duplicating the tokenizer logic in TypeScript.
//
// Coordinates are 0-based, relative to the start of the block's source
// text (NOT to the surrounding markdown document). Columns are UTF-16
// code units to match the LSP convention.
type bufferToken struct {
	Line     uint32 `json:"line"`
	StartCol uint32 `json:"col"`
	Length   uint32 `json:"len"`
	Type     string `json:"type"`           // one of semanticTokenTypes, e.g. "ipmEvent"
	Mods     uint32 `json:"mods,omitempty"` // bitmask matching semanticTokenModifiers
}

// cmdEmbedBuffer implements workspace/executeCommand "ipm.embedBuffer".
// Argument: {"uri": "file://…", "tokensOnly": false}. The URI must be for a
// document the server is currently tracking (didOpen received); otherwise the
// call errors so the client can fall back to disk-based rendering.
//
// tokensOnly skips the SVG render (the expensive half — layout runs per
// block) and returns just source + tokens. Colors and diagrams have very
// different freshness needs: the preview's fence text must match the buffer
// on the keystroke, while a diagram can lag until the typist pauses. A
// tokens-only call is roughly an order of magnitude cheaper, which is what
// makes per-keystroke coloring affordable on a large document.
func (s *server) cmdEmbedBuffer(ctx context.Context, args []any) (*embedBufferResult, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing argument: {uri: ...}")
	}
	argMap, ok := args[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("argument 0 must be {uri: ...}; got %T", args[0])
	}
	uriArg, _ := argMap["uri"].(string)
	if uriArg == "" {
		return nil, fmt.Errorf("missing or empty 'uri'")
	}
	tokensOnly, _ := argMap["tokensOnly"].(bool)

	docURI := lsp.DocumentURI(uriArg)
	s.mu.Lock()
	doc, ok := s.docs[docURI]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("document not open in server: %s (run textDocument/didOpen first)", uriArg)
	}
	bufferText := doc.text

	mdAbs, err := resolveFilePath(uriArg)
	if err != nil {
		return nil, fmt.Errorf("resolve uri: %w", err)
	}

	rootAbs := ipconfig.FindRepoRoot(filepath.Dir(mdAbs))
	rootAbs, _ = filepath.Abs(rootAbs)
	svgDir := "_ipm"
	if fileCfg, present, err := ipconfig.Load(rootAbs); err == nil && present && fileCfg.SVGDir != nil {
		svgDir = *fileCfg.SVGDir
	}

	// `.ipmt` files are a single ipmt block — no markdown fence scanning.
	// Render the whole buffer as one block and return the SVG in-memory
	// so the preview pane can show it without touching disk.
	if strings.HasSuffix(strings.ToLower(mdAbs), ".ipmt") {
		return renderIpmtBuffer(uriArg, mdAbs, rootAbs, bufferText, tokensOnly)
	}

	analysis, err := mdembed.AnalyzeMarkdown(mdAbs, bufferText, mdembed.AnalyzeOptions{
		Root:   rootAbs,
		SVGDir: svgDir,
	})
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}

	res := &embedBufferResult{Uri: uriArg}
	// Inline ipmt: scan the buffer for `<!--ipmt-->` + backtick pairs and
	// return per-fragment tokens. Independent of fenced blocks — a .md
	// can have inline ipmt without any ```ipmt fences.
	res.InlineIpmt = collectInlineIpmt(bufferText)
	if !analysis.HasIPMT() {
		return res, nil
	}

	generatedBy := fmt.Sprintf("%s@%s", toolName, versionString())

	for _, br := range analysis.Blocks {
		bb := bufferBlock{
			Index:   br.Index,
			ID:      br.NewMarker.ID,
			Hash:    br.SrcHash,
			Source:  br.Content,
			Outcome: string(br.Outcome),
			Tokens:  blockTokensFor(br.Content),
		}
		switch br.Outcome {
		case mdembed.OutcomeUnterminated, mdembed.OutcomeMalformed,
			mdembed.OutcomeMissingSrc, mdembed.OutcomeBadMeta:
			// Surface as a warning AND emit a block entry with no SVG so the
			// client knows that block exists but couldn't be rendered.
			res.Warnings = append(res.Warnings, embedWarning{
				BlockIndex: br.Index,
				Outcome:    string(br.Outcome),
				Message:    br.SkipReason,
				Line:       br.AnchorLine,
			})
			res.Blocks = append(res.Blocks, bb)
			continue
		case mdembed.OutcomeNoEmbed:
			// embed=false: valid but illustrative — emit the block with no SVG
			// and no warning; the preview shows it as plain source.
			res.Blocks = append(res.Blocks, bb)
			continue
		}

		if tokensOnly {
			// Caller wants coloring only; the SVG stays whatever the last
			// full call produced (the client keys SVGs by block id, tokens
			// by source, so the two caches never contradict each other).
			res.Blocks = append(res.Blocks, bb)
			continue
		}

		svgBytes, err := mdembed.RenderSVGBytes(rootAbs, mdAbs, br, generatedBy)
		if err != nil {
			// Common during typing — partial fences, transient parse errors.
			// Don't fail the whole call; mark this block, keep going.
			bb.Error = err.Error()
			res.Blocks = append(res.Blocks, bb)
			continue
		}
		bb.SVGBase64 = base64.StdEncoding.EncodeToString(svgBytes)
		res.Blocks = append(res.Blocks, bb)
	}
	return res, nil
}

// renderIpmtBuffer handles the `.ipmt` URI branch of cmdEmbedBuffer:
// the whole buffer is treated as one ipmt block (no markdown fence
// scanning), rendered to SVG in-memory, and returned alongside its
// block-relative semantic tokens. Used by the `.ipmt` preview pane —
// no disk writes; the SVG never persists.
//
// tokensOnly returns the tokens without rendering, same contract as the
// markdown path.
func renderIpmtBuffer(uriArg, mdAbs, rootAbs, bufferText string, tokensOnly bool) (*embedBufferResult, error) {
	res := &embedBufferResult{Uri: uriArg}
	if strings.TrimSpace(bufferText) == "" {
		return res, nil
	}

	// A standalone .ipmt file carries its processing flags via the `# ipmt:`
	// first-line pragma (it has no fence). Honor unresolved/defaults in the
	// preview; a bad pragma surfaces as a block error.
	meta, metaErr := ipmtmeta.EffectiveMeta(nil, bufferText)
	br := mdembed.BlockResult{
		Index:   1,
		Kind:    mdembed.KindVisible,
		Content: bufferText,
		SrcHash: mdembed.HashIPMT(bufferText),
		Meta:    meta,
		Outcome: mdembed.OutcomeOK,
	}
	bb := bufferBlock{
		Index:   1,
		ID:      "preview",
		Hash:    br.SrcHash,
		Source:  bufferText,
		Outcome: string(br.Outcome),
		Tokens:  blockTokensFor(bufferText),
	}
	if metaErr != nil {
		bb.Outcome = string(mdembed.OutcomeBadMeta)
		bb.Error = metaErr.Error()
		res.Blocks = append(res.Blocks, bb)
		return res, nil
	}

	if tokensOnly {
		res.Blocks = append(res.Blocks, bb)
		return res, nil
	}

	generatedBy := fmt.Sprintf("%s@%s", toolName, versionString())
	svgBytes, err := mdembed.RenderSVGBytes(rootAbs, mdAbs, br, generatedBy)
	if err != nil {
		// Common while typing: partial / unparseable buffer. Surface
		// the error so the webview can show it but keep the previous
		// SVG visible until a good render arrives.
		bb.Error = err.Error()
		res.Blocks = append(res.Blocks, bb)
		return res, nil
	}
	bb.SVGBase64 = base64.StdEncoding.EncodeToString(svgBytes)
	res.Blocks = append(res.Blocks, bb)
	return res, nil
}

// collectInlineIpmt scans the buffer for `<!--ipmt-->` + backtick code-span
// pairs and returns one fragment per match, with semantic tokens. The
// client keys these by `Source` (== the inline code content) so its
// markdown-it rule can look up tokens for any matching code_inline.
//
// Returns nil when no inline ipmt is present.
func collectInlineIpmt(bufferText string) []inlineIpmtFragment {
	matches := markdown.FindInlineIpmt(bufferText)
	if len(matches) == 0 {
		return nil
	}
	out := make([]inlineIpmtFragment, 0, len(matches))
	for _, m := range matches {
		var tokens []bufferToken
		if m.AsToken != "" {
			// as-token:NAME — paint the whole visible code in the named
			// style (one synthetic token; unknown NAME → uncolored).
			if styled, ok := ipmtokens.TokensForClass(m.AsToken, m.Code); ok {
				tokens = tokensToBuffer(styled)
			}
		} else {
			// bare marker — tokenize the code as a valid ipmt fragment.
			tokens = blockTokensFor(m.Code)
		}
		out = append(out, inlineIpmtFragment{
			Source: m.Code,
			Tokens: tokens,
		})
	}
	return out
}

// blockTokensFor runs the LSP semantic tokenizer over a single ipmt block's
// raw source (without the surrounding fences) and returns the tokens in
// block-relative coordinates. The markdown preview consumes these so its
// per-block coloring stays in lockstep with the editor pane.
//
// On parse error or empty input it returns nil — the caller leaves the
// `tokens` field omitted and the preview falls back to its built-in regex
// tokenizer.
func blockTokensFor(blockText string) []bufferToken {
	if blockText == "" {
		return nil
	}
	return tokensToBuffer(ipmtokens.Collect(blockText, blockText, 0))
}

// tokensToBuffer converts semantic tokens into the wire-format
// bufferToken records (type index → name string). Returns nil for an
// empty slice so the caller omits the `tokens` field.
func tokensToBuffer(sem []ipmtokens.Token) []bufferToken {
	if len(sem) == 0 {
		return nil
	}
	out := make([]bufferToken, 0, len(sem))
	for _, t := range sem {
		out = append(out, bufferToken{
			Line:     t.Line,
			StartCol: t.StartCol,
			Length:   t.Length,
			Type:     ipmtokens.TypeName(t.Type),
			Mods:     t.Mods,
		})
	}
	return out
}
