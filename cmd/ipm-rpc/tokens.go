package main

import (
	"context"
	"sort"

	lsp "go.lsp.dev/protocol"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
	ipmd "github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// semanticTokensFull handles textDocument/semanticTokens/full.
//
// Returns a flat uint32 array following the LSP relative encoding:
//
//	[deltaLine, deltaStart, length, tokenType, tokenModifiers, ...]
//
// where deltaLine is relative to the previous token's line (or 0 for the
// first) and deltaStart is relative to the previous token's start column
// (or absolute when deltaLine > 0).
//
// The tokenizer lives in pkg/ipmtokens so cmd/md-html can call it too;
// this file is only responsible for the LSP wire encoding.
func (s *server) semanticTokensFull(ctx context.Context, params *lsp.SemanticTokensParams) (*lsp.SemanticTokens, error) {
	s.mu.Lock()
	doc, ok := s.docs[params.TextDocument.URI]
	s.mu.Unlock()
	if !ok {
		return &lsp.SemanticTokens{Data: []uint32{}}, nil
	}

	var tokens []ipmtokens.Token
	switch {
	case looksLikeIPMT(doc):
		tokens = ipmtokens.Collect(doc.text, doc.text, 0)
	case looksLikeMarkdown(doc):
		_, blocks := ipmd.ScanIPMTBlocks(doc.text)
		for _, blk := range blocks {
			if blk.EndLine < 0 {
				continue
			}
			tokens = append(tokens, ipmtokens.Collect(doc.text, blk.Content, uint32(blk.StartLine+1))...)
		}
	}
	return &lsp.SemanticTokens{Data: encodeSemanticTokens(tokens)}, nil
}

// encodeSemanticTokens delta-encodes the tokens into the flat uint32 array
// the LSP protocol expects. [ipmtokens.Collect] returns a flattened,
// non-overlapping, sorted stream (see ipmtokens.Flatten), so the encoder
// no longer relies on client "last-wins" to resolve overlaps — every
// character is covered by at most one token. The re-sort here is a cheap
// safety net for callers that concatenate tokens from several blocks.
func encodeSemanticTokens(tokens []ipmtokens.Token) []uint32 {
	if len(tokens) == 0 {
		return []uint32{}
	}
	sort.SliceStable(tokens, func(i, j int) bool {
		if tokens[i].Line != tokens[j].Line {
			return tokens[i].Line < tokens[j].Line
		}
		return tokens[i].StartCol < tokens[j].StartCol
	})
	out := make([]uint32, 0, len(tokens)*5)
	var prevLine, prevCol uint32
	for i, t := range tokens {
		deltaLine := t.Line - prevLine
		var deltaStart uint32
		if i == 0 || deltaLine != 0 {
			deltaStart = t.StartCol
		} else {
			deltaStart = t.StartCol - prevCol
		}
		out = append(out, deltaLine, deltaStart, t.Length, t.Type, t.Mods)
		prevLine = t.Line
		prevCol = t.StartCol
	}
	return out
}
