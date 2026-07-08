package mdembed

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/ipmsvg"
	"github.com/infinite-pm/ipm-tools/pkg/nodekind"
)

// RenderSVGBytes renders br's ipmt content to an SVG byte slice with the
// provenance metadata that SVGIsFresh later checks. Does NOT touch the
// filesystem — used by ipm.embedBuffer (live preview) where the .md hasn't
// been saved yet and we don't want disk churn.
//
// rootAbs is the workspace root; used only to compute a stable
// `source-file` value for the embedded meta (forward-slash, repo-root-
// relative). generatedBy is a free-form identifier (typically
// "<tool-name>@<version>") that ends up in the meta as well.
func RenderSVGBytes(rootAbs, mdAbs string, br BlockResult, generatedBy string) ([]byte, error) {
	graph, err := parser.ParseIPMTBytes([]byte(br.Content), mdAbs)
	if err != nil {
		return nil, fmt.Errorf("parse ipmt: %w", err)
	}
	// `ipmt unresolved` fence: resolve undecided (::?…) node kinds with the
	// node-kind solver before rendering, so the SVG shows resolved kinds (and
	// grey/swatches for any that stay genuinely undecided). The `defaults`
	// directive additionally collapses each still-grey node to its role-preferred
	// kind (Expresses-target→concept, everything else→thing; event is never a
	// default), fully resolving the block.
	useDefaults := slices.Contains(br.Meta, "defaults")
	if useDefaults || slices.Contains(br.Meta, "unresolved") {
		var res *nodekind.Result
		if useDefaults {
			res = nodekind.SolveWithDefaults(graph)
		} else {
			res = nodekind.Solve(graph)
		}
		solved := nodekind.ToGraph(res)
		solved.Src = graph.Src // preserve ipmt provenance (ToGraph leaves Src empty)
		graph = solved
	}
	svg, err := ipmsvg.GenerateFromIPM(graph)
	if err != nil {
		return nil, fmt.Errorf("render svg: %w", err)
	}

	relMD, _ := filepath.Rel(rootAbs, mdAbs)
	relMD = filepath.ToSlash(relMD)

	return EmbedMeta(svg, Meta{
		Hash:        br.SrcHash,
		SourceID:    br.NewMarker.ID,
		SourceFile:  relMD,
		GeneratedBy: generatedBy,
	}), nil
}

// RenderAndWriteSVG renders br's ipmt content to an SVG file at br.SVGPath and
// reports whether it actually wrote the file.
//
// When forceMeta is false (the usual case) and the freshly rendered bytes
// differ from the existing file ONLY in the `generated-by` provenance field,
// the write is skipped and (false, nil) is returned — re-rendering with a
// different tool/version must not churn version control. Pass forceMeta=true to
// rewrite regardless (e.g. to deliberately restamp provenance across a tree).
//
// Lifted out of cmd/md-embed so cmd/ipm-rpc can call the same code path
// when serving workspace/executeCommand "ipm.embed".
func RenderAndWriteSVG(rootAbs, mdAbs string, br BlockResult, generatedBy string, forceMeta bool) (bool, error) {
	svg, err := RenderSVGBytes(rootAbs, mdAbs, br, generatedBy)
	if err != nil {
		return false, err
	}
	// Leave the file untouched when the only drift is the generated-by stamp —
	// but never short-circuit a migration rename, which must still land the SVG
	// at its new path (and drop the old one below).
	if !forceMeta && br.RenamedFromSVGPath == "" {
		if existing, err := os.ReadFile(br.SVGPath); err == nil && EqualExceptGeneratedBy(svg, existing) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(br.SVGPath), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(br.SVGPath, svg, 0o644); err != nil {
		return false, err
	}
	// Migration renamed this block (numeric id → base-36 key): now that the new
	// SVG is on disk, drop the old one. Done here so it happens for every writer
	// (the RPC ipm.embed path does not prune, and CLI --no-prune skips cleanup).
	if br.RenamedFromSVGPath != "" && br.RenamedFromSVGPath != br.SVGPath {
		if err := os.Remove(br.RenamedFromSVGPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return true, nil
}

// SVGIsFresh reports whether the on-disk SVG at absPath was rendered from
// content whose normalized-source hash matches srcHash. Returns
// (false, nil) when the file is missing; returns an error only for read
// failures other than ErrNotExist.
func SVGIsFresh(absPath, srcHash string) (bool, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	meta, ok := ExtractMeta(data)
	if !ok {
		return false, nil
	}
	return meta.Hash == srcHash, nil
}
