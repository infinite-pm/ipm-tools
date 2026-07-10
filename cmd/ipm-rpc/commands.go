package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	lsp "go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	ipconfig "github.com/infinite-pm/ipm-tools/pkg/ipm/config"
	iplog "github.com/infinite-pm/ipm-tools/pkg/ipm/log"
	"github.com/infinite-pm/ipm-tools/pkg/mdembed"
)

// embedResult is the shape we return for ipm.embed and ipm.check via
// workspace/executeCommand. JSON-tagged so the LSP client (the VS Code
// extension) can unmarshal it into a typed struct.
type embedResult struct {
	File         string         `json:"file"`         // absolute path of the processed .md
	Changed      bool           `json:"changed"`      // true if any markers/SVGs were (or would be) written
	Stats        embedStats     `json:"stats"`        // outcome counts
	Warnings     []embedWarning `json:"warnings"`     // non-fatal issues (unterminated fence, etc.)
	StaleSummary string         `json:"staleSummary"` // human-readable one-liner; useful for status bar
}

type embedStats struct {
	Blocks       int `json:"blocks"`
	InsertMarker int `json:"insertMarker"`
	Rehash       int `json:"rehash"`
	Rerender     int `json:"rerender"`
	Unterminated int `json:"unterminated"`
	Malformed    int `json:"malformed"`
	MissingSrc   int `json:"missingSrc"`
	NoEmbed      int `json:"noEmbed"`
	BadMeta      int `json:"badMeta"`
}

type embedWarning struct {
	BlockIndex int    `json:"blockIndex"`
	Outcome    string `json:"outcome"`
	Message    string `json:"message"`
	Line       int    `json:"line"` // 0-based; the LSP convention
}

// executeCommand dispatches workspace/executeCommand requests. The argument
// shape depends on the command name; we decode lazily inside each case.
func (s *server) executeCommand(ctx context.Context, params *lsp.ExecuteCommandParams) (any, error) {
	log := iplog.FromContext(iplog.With(ctx, s.log.With(slog.String("command", params.Command))))
	log.Debug("workspace/executeCommand")

	switch params.Command {
	case "ipm.embed":
		return s.cmdEmbedOrCheck(ctx, params.Arguments, false)
	case "ipm.check":
		return s.cmdEmbedOrCheck(ctx, params.Arguments, true)
	case "ipm.embedBuffer":
		return s.cmdEmbedBuffer(ctx, params.Arguments)
	default:
		return nil, fmt.Errorf("unknown command: %s", params.Command)
	}
}

// cmdEmbedOrCheck implements ipm.embed (when dryRun=false) and ipm.check
// (when dryRun=true). Args: {"file": "<uri or absolute path>", "force":
// bool?}. The file must point at a `.md` somewhere on disk. When
// `force` is true the freshness check is bypassed — every block that
// would normally be skipped because the on-disk SVG already matches
// the source hash is re-rendered and re-written. Used by the
// "Force re-embed" commands when the user wants to regenerate after a
// renderer / SVG style upgrade that doesn't change the source hash.
func (s *server) cmdEmbedOrCheck(ctx context.Context, args []any, dryRun bool) (*embedResult, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing argument: {file: ...}")
	}
	argMap, ok := args[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("argument 0 must be {file: ...}; got %T", args[0])
	}
	fileArg, ok := argMap["file"].(string)
	if !ok || fileArg == "" {
		return nil, fmt.Errorf("missing or empty 'file'")
	}
	force, _ := argMap["force"].(bool)
	mdAbs, err := resolveFilePath(fileArg)
	if err != nil {
		return nil, fmt.Errorf("resolve file: %w", err)
	}

	// Discover the workspace root for this file. Walks up looking for
	// .git or .ipm.conf; same logic md-embed CLI uses.
	rootAbs := ipconfig.FindRepoRoot(filepath.Dir(mdAbs))
	rootAbs, _ = filepath.Abs(rootAbs)

	// Honor the repo's .ipm.conf if present. CLI flags don't apply here —
	// the LSP server respects only the on-disk policy.
	svgDir := "_ipm"
	if fileCfg, present, err := ipconfig.Load(rootAbs); err == nil && present {
		if fileCfg.SVGDir != nil {
			svgDir = *fileCfg.SVGDir
		}
		// embed-ignore: never scan, render, or rewrite an ignored doc — even
		// on an explicit "Force Re-embed". Return a no-op so the editor's
		// embed-on-save and force commands leave it untouched.
		if rel, err := filepath.Rel(rootAbs, mdAbs); err == nil && fileCfg.IsIgnored(rel) {
			return &embedResult{File: mdAbs, StaleSummary: "ignored (embed-ignore)"}, nil
		}
	}

	mdText, err := os.ReadFile(mdAbs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", mdAbs, err)
	}

	analysis, err := mdembed.AnalyzeMarkdown(mdAbs, string(mdText), mdembed.AnalyzeOptions{
		Root:   rootAbs,
		SVGDir: svgDir,
	})
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}

	res := &embedResult{File: mdAbs}
	if !analysis.HasIPMT() {
		res.StaleSummary = "no ipmt blocks"
		return res, nil
	}

	// Per-block walk — same outcome dispatch as cmd/md-embed.
	for i, br := range analysis.Blocks {
		switch br.Outcome {
		case mdembed.OutcomeUnterminated:
			res.Stats.Unterminated++
			res.Warnings = append(res.Warnings, embedWarning{
				BlockIndex: br.Index, Outcome: string(br.Outcome),
				Message: "unterminated ```ipmt fence", Line: br.AnchorLine,
			})
			continue
		case mdembed.OutcomeMalformed:
			res.Stats.Malformed++
			res.Warnings = append(res.Warnings, embedWarning{
				BlockIndex: br.Index, Outcome: string(br.Outcome),
				Message: br.SkipReason, Line: br.AnchorLine,
			})
			continue
		case mdembed.OutcomeMissingSrc:
			res.Stats.MissingSrc++
			res.Warnings = append(res.Warnings, embedWarning{
				BlockIndex: br.Index, Outcome: string(br.Outcome),
				Message: br.SkipReason, Line: br.AnchorLine,
			})
			continue
		case mdembed.OutcomeNoEmbed:
			// embed=false: valid but illustrative — return the block with no SVG
			// and no warning; the preview shows it as plain source.
			res.Stats.NoEmbed++
			continue
		case mdembed.OutcomeBadMeta:
			res.Stats.BadMeta++
			res.Warnings = append(res.Warnings, embedWarning{
				BlockIndex: br.Index, Outcome: string(br.Outcome),
				Message: "invalid ipmt metadata: " + br.SkipReason, Line: br.AnchorLine,
			})
			continue
		}

		fresh, err := mdembed.SVGIsFresh(br.SVGPath, br.SrcHash)
		if err != nil {
			return res, fmt.Errorf("inspect svg %s: %w", br.SVGPath, err)
		}
		if br.Outcome == mdembed.OutcomeOK && (!fresh || force) {
			br.Outcome = mdembed.OutcomeRerender
			analysis.Blocks[i] = br
		}

		switch br.Outcome {
		case mdembed.OutcomeInsertMarker:
			res.Stats.InsertMarker++
		case mdembed.OutcomeRehash:
			res.Stats.Rehash++
		case mdembed.OutcomeRerender:
			res.Stats.Rerender++
		}
		res.Stats.Blocks++

		if dryRun || br.Outcome == mdembed.OutcomeOK {
			continue
		}
		generatedBy := fmt.Sprintf("%s@%s", toolName, versionString())
		if _, err := mdembed.RenderAndWriteSVG(rootAbs, mdAbs, br, generatedBy, false); err != nil {
			return res, fmt.Errorf("render block %d: %w", br.Index, err)
		}
	}

	stale := res.Stats.InsertMarker + res.Stats.Rehash + res.Stats.Rerender +
		res.Stats.MissingSrc + res.Stats.BadMeta

	// In write-mode, persist the .md if ApplyMarkers would change anything.
	if !dryRun && stale > 0 {
		newLines := mdembed.ApplyMarkers(analysis.Lines, analysis.Blocks)
		newText := strings.Join(newLines, "\n")
		if newText != string(mdText) {
			if err := os.WriteFile(mdAbs, []byte(newText), 0o644); err != nil {
				return res, fmt.Errorf("write %s: %w", mdAbs, err)
			}
			res.Changed = true
		}
	}

	res.StaleSummary = formatStaleSummary(res.Stats)
	return res, nil
}

func formatStaleSummary(s embedStats) string {
	parts := []string{fmt.Sprintf("blocks=%d", s.Blocks)}
	if s.InsertMarker > 0 {
		parts = append(parts, fmt.Sprintf("insert=%d", s.InsertMarker))
	}
	if s.Rehash > 0 {
		parts = append(parts, fmt.Sprintf("rehash=%d", s.Rehash))
	}
	if s.Rerender > 0 {
		parts = append(parts, fmt.Sprintf("rerender=%d", s.Rerender))
	}
	if s.MissingSrc > 0 {
		parts = append(parts, fmt.Sprintf("missing-src=%d", s.MissingSrc))
	}
	if s.Unterminated > 0 {
		parts = append(parts, fmt.Sprintf("unterminated=%d", s.Unterminated))
	}
	if s.Malformed > 0 {
		parts = append(parts, fmt.Sprintf("malformed=%d", s.Malformed))
	}
	if s.NoEmbed > 0 {
		parts = append(parts, fmt.Sprintf("no-embed=%d", s.NoEmbed))
	}
	if s.BadMeta > 0 {
		parts = append(parts, fmt.Sprintf("bad-meta=%d", s.BadMeta))
	}
	return strings.Join(parts, " ")
}

// resolveFilePath accepts either a file:// URI or an absolute filesystem
// path. The VS Code extension uses URIs; CLI testing prefers plain paths.
func resolveFilePath(fileArg string) (string, error) {
	if strings.HasPrefix(fileArg, "file://") {
		u, err := uri.Parse(fileArg)
		if err != nil {
			return "", err
		}
		return u.Filename(), nil
	}
	abs, err := filepath.Abs(fileArg)
	if err != nil {
		return "", err
	}
	return abs, nil
}
