package mdembed

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtmeta"
)

// BlockOutcome reports what the tool would do (or did) for one block.
type BlockOutcome string

const (
	OutcomeOK           BlockOutcome = "ok"            // marker present, hashes match, SVG fresh
	OutcomeInsertMarker BlockOutcome = "insert-marker" // no marker present
	OutcomeRehash       BlockOutcome = "rehash"        // marker hash differs from source hash
	OutcomeRerender     BlockOutcome = "rerender"      // marker hash matches but SVG file missing or stale
	OutcomeUnterminated BlockOutcome = "unterminated"  // visible: source fence is unterminated; report and skip
	OutcomeMissingSrc   BlockOutcome = "missing-src"   // include: the sibling .ipmt referenced by src= can't be read
	OutcomeMalformed    BlockOutcome = "malformed"     // structurally bad input (e.g. <!-- ipm-include --> without src=)
	OutcomeNoEmbed      BlockOutcome = "no-embed"      // `embed=false`: valid but illustrative — not rendered/embedded
	OutcomeBadMeta      BlockOutcome = "bad-meta"      // invalid ipmt metadata (misplaced/unknown `# ipmt:` pragma or fence token)
)

// BlockResult is one block's per-analysis verdict.
type BlockResult struct {
	Index      int       // 1-based positional index across all kinds in source order
	Kind       BlockKind // visible / include
	OpenLine   int       // first line of the block (opening fence or ipm-include line)
	AnchorLine int       // line after which an "after"-positioned marker is expected (closing fence, </details>, or ipm-include line)
	MarkerLine int       // line of existing marker comment; -1 if absent
	OldMarker  Marker
	NewMarker  Marker
	SVGPath    string // absolute path of the SVG the tool would (re)write
	// RenamedFromSVGPath is the absolute path of the SVG this block referenced
	// under a previous id, now orphaned because the block was re-keyed (e.g. a
	// duplicated id resolved to its own key). The writer removes it after the
	// new SVGPath is written. Empty when no rename occurred.
	RenamedFromSVGPath string
	SrcHash            string   // hash of the normalized source content
	Content            string   // ipmt source content (already loaded for all kinds)
	Meta               []string // fence metadata after "ipmt" (e.g. ["unresolved"]); visible blocks only
	Outcome            BlockOutcome
	SkipReason         string // populated when Outcome is Unterminated / MissingSrc / Malformed
	// Include-specific: absolute path of the sibling .ipmt file referenced
	// by the include line (resolved against the .md file's directory).
	IncludeSrcAbs string
}

// FileAnalysis is the per-file result of AnalyzeMarkdown.
type FileAnalysis struct {
	Path   string // absolute path of the source .md
	Lines  []string
	Blocks []BlockResult
}

// HasIPMT reports whether the file contains any ipmt blocks (of any kind).
func (a FileAnalysis) HasIPMT() bool { return len(a.Blocks) > 0 }

// SrcReader resolves an include `src=` path (already converted to absolute)
// to its content. Defaults to os.ReadFile; tests can swap it.
type SrcReader func(absPath string) ([]byte, error)

// AnalyzeOptions configure AnalyzeMarkdown.
type AnalyzeOptions struct {
	// Root is the absolute repo root. SVG paths are computed relative to it.
	Root string
	// SVGDir is the directory under Root that holds generated SVGs. Default: "_ipm".
	SVGDir string
	// SrcReader loads include source files. Default: os.ReadFile.
	SrcReader SrcReader
}

// AnalyzeMarkdown reads the .md file at absPath and produces a FileAnalysis
// describing every block (visible, include) and what action the processor
// needs to take. Does no rendering; touches the filesystem only via
// opts.SrcReader (for include sources).
func AnalyzeMarkdown(absPath string, mdText string, opts AnalyzeOptions) (FileAnalysis, error) {
	if opts.Root == "" {
		return FileAnalysis{}, errors.New("AnalyzeOptions.Root is required")
	}
	if opts.SVGDir == "" {
		opts.SVGDir = "_ipm"
	}
	if opts.SrcReader == nil {
		opts.SrcReader = os.ReadFile
	}
	relMD, err := filepath.Rel(opts.Root, absPath)
	if err != nil {
		return FileAnalysis{}, fmt.Errorf("rel md: %w", err)
	}
	relMD = filepath.ToSlash(relMD)
	if strings.HasPrefix(relMD, "../") {
		return FileAnalysis{}, fmt.Errorf("md path %s is outside root %s", absPath, opts.Root)
	}
	stem := strings.TrimSuffix(relMD, filepath.Ext(relMD))
	mdDirAbs := filepath.Dir(absPath)

	lines, raw := ScanBlocks(mdText)
	out := FileAnalysis{Path: absPath, Lines: lines}

	for i, blk := range raw {
		br := BlockResult{
			Index:      i + 1,
			Kind:       blk.Kind,
			OpenLine:   blk.OpenLine,
			AnchorLine: blk.AnchorLine,
			MarkerLine: -1,
		}
		if blk.HasMarker {
			br.MarkerLine = blk.MarkerLine
			br.OldMarker = blk.Marker
		}

		if blk.Skip {
			br.SkipReason = blk.SkipReason
			if blk.Kind == KindVisible {
				br.Outcome = OutcomeUnterminated
			} else {
				br.Outcome = OutcomeMalformed
			}
			out.Blocks = append(out.Blocks, br)
			continue
		}

		// Pick the ID:
		//   1. existing marker's id= (preserved across runs)
		//   2. include line's explicit id= (when given)
		//   3. include: basename of the src .ipmt file (without extension)
		// A visible block with no marker gets none here — the keyed pass below
		// assigns it a between-key.
		var id string
		switch {
		case blk.HasMarker && blk.Marker.ID != "":
			id = blk.Marker.ID
		case blk.Kind == KindInclude && blk.ExplicitID != "":
			id = blk.ExplicitID
		case blk.Kind == KindInclude:
			base := filepath.Base(filepath.FromSlash(blk.SrcPathRel))
			id = strings.TrimSuffix(base, filepath.Ext(base))
		}
		// A visible block with no marker yet has no id at this point; the keyed
		// pass below assigns it a between-key (and its SVG path with it).

		// Resolve / load source content.
		switch blk.Kind {
		case KindVisible:
			br.Content = blk.Content
		case KindInclude:
			srcAbs := filepath.Join(mdDirAbs, filepath.FromSlash(blk.SrcPathRel))
			br.IncludeSrcAbs = srcAbs
			data, err := opts.SrcReader(srcAbs)
			if err != nil {
				br.Outcome = OutcomeMissingSrc
				br.SkipReason = fmt.Sprintf("cannot read src=%s: %v", blk.SrcPathRel, err)
				out.Blocks = append(out.Blocks, br)
				continue
			}
			br.Content = string(data)
		}

		// Effective processing flags: the fence info-string tokens (visible
		// blocks) unioned with the `# ipmt:` pragma in the content — the only
		// flag channel for include blocks and standalone .ipmt files, which have
		// no fence. Strict: a misplaced pragma or unknown token is a real error.
		meta, err := ipmtmeta.EffectiveMeta(blk.Meta, br.Content)
		if err != nil {
			br.Outcome = OutcomeBadMeta
			br.SkipReason = err.Error()
			out.Blocks = append(out.Blocks, br)
			continue
		}
		br.Meta = meta

		// `embed=false`: a valid but illustrative block (a syntax snippet where
		// the text is the point) — never rendered or embedded.
		if ipmtmeta.Contains(br.Meta, ipmtmeta.FlagEmbedFalse) {
			br.Outcome = OutcomeNoEmbed
			out.Blocks = append(out.Blocks, br)
			continue
		}

		br.SrcHash = HashIPMT(br.Content)

		// Decide the SVG output path. Marker.Path overrides default location.
		defaultSVGAbs := filepath.Join(opts.Root, opts.SVGDir, filepath.FromSlash(stem), id+".ipm.svg")
		svgAbs := defaultSVGAbs
		if blk.HasMarker && blk.Marker.Path != "" {
			svgAbs = filepath.Join(mdDirAbs, filepath.FromSlash(blk.Marker.Path))
		}
		imgRel, err := filepath.Rel(mdDirAbs, svgAbs)
		if err != nil {
			return FileAnalysis{}, fmt.Errorf("rel svg: %w", err)
		}
		imgRel = filepath.ToSlash(imgRel)

		br.NewMarker = Marker{
			ID:        id,
			Hash:      br.SrcHash,
			Path:      blk.Marker.Path,
			Pos:       blk.Marker.Pos, // scanner set this from where the marker was found
			ImageAlt:  blk.Marker.ImageAlt,
			ImagePath: imgRel,
		}
		br.SVGPath = svgAbs

		switch {
		case !blk.HasMarker:
			br.Outcome = OutcomeInsertMarker
		case blk.Marker.Hash != br.SrcHash:
			br.Outcome = OutcomeRehash
		default:
			br.Outcome = OutcomeOK
		}

		out.Blocks = append(out.Blocks, br)
	}

	// Second pass — assign ids.
	//
	// Every existing marker id is preserved (see keepID); blocks that need a
	// fresh id — a new block, or one whose id duplicates an earlier block's — get
	// a key BETWEEN their document neighbours (see pkg/mdembed/key.go), so
	// inserting or reordering never renumbers existing blocks or renames their
	// SVGs, and a copy-pasted duplicate resolves to its own key instead of
	// silently pointing at the first block's SVG.
	skip := func(o BlockOutcome) bool {
		return o == OutcomeUnterminated || o == OutcomeMalformed || o == OutcomeMissingSrc ||
			o == OutcomeNoEmbed || o == OutcomeBadMeta
	}
	{
		// preserved[i] = block keeps its id (present + first to claim it);
		// everything else is assigned a between-key.
		preserved := make([]bool, len(out.Blocks))
		seen := map[string]bool{}
		for i := range out.Blocks {
			if skip(out.Blocks[i].Outcome) {
				continue
			}
			id := out.Blocks[i].NewMarker.ID
			if keepID(out.Blocks[i]) && !seen[id] {
				preserved[i] = true
				seen[id] = true
			}
		}
		// Marker lines owned by a preserved block. A reassigned block may rewrite
		// its OWN marker line, but must INSERT a fresh marker when its line is
		// shared (the scanner attributes one marker to an adjacent marker-less
		// block) — rewriting a shared line would clobber the owning block's marker.
		claimedLine := map[int]bool{}
		for i := range out.Blocks {
			if preserved[i] && out.Blocks[i].MarkerLine >= 0 {
				claimedLine[out.Blocks[i].MarkerLine] = true
			}
		}
		nextKey := make([]string, len(out.Blocks))
		nk := ""
		for i := len(out.Blocks) - 1; i >= 0; i-- {
			nextKey[i] = nk
			// Only a real key bounds the interval. A preserved non-key id (an
			// include's filename, a hand-written id=) has no position in the
			// keyspace, so it must not act as a bound — otherwise the neighbour
			// would be allocated from the bottom of the space and could collide
			// with an existing key.
			if preserved[i] && IsKey(out.Blocks[i].NewMarker.ID) {
				nk = out.Blocks[i].NewMarker.ID
			}
		}
		prevKey := ""
		for i := range out.Blocks {
			br := &out.Blocks[i]
			if skip(br.Outcome) {
				continue
			}
			if preserved[i] {
				// Keeps the id resolved in the first pass; only a real key
				// advances the interval bound.
				if IsKey(br.NewMarker.ID) {
					prevKey = br.NewMarker.ID
				}
				continue
			}
			var key string
			var ok bool
			if nextKey[i] == "" {
				key, ok = keyAfter(prevKey) // appending past the last key: small step
			} else {
				key, ok = keyBetween(prevKey, nextKey[i]) // inserting between two: midpoint
				if !ok {
					key, ok = keyAfter(prevKey)
				}
			}
			if !ok {
				continue // keyspace exhausted (extremely unlikely for a doc)
			}
			br.NewMarker.ID = key
			if br.NewMarker.Path == "" { // default location: filename follows the id
				svgAbs := filepath.Join(opts.Root, opts.SVGDir, filepath.FromSlash(stem), key+".ipm.svg")
				// A block that already had a marker referenced an SVG under its
				// old id; once the filename follows the new key, that file is
				// orphaned. Record it so the writer deletes it after rendering
				// the new one. (A brand-new block had no SVG, so nothing to drop.)
				if br.OldMarker.ID != "" && br.SVGPath != "" && br.SVGPath != svgAbs {
					br.RenamedFromSVGPath = br.SVGPath
				}
				br.SVGPath = svgAbs
				if rel, err := filepath.Rel(mdDirAbs, svgAbs); err == nil {
					br.NewMarker.ImagePath = filepath.ToSlash(rel)
				}
			}
			if br.MarkerLine >= 0 && !claimedLine[br.MarkerLine] {
				br.Outcome = OutcomeRehash // owns a distinct marker → rewrite it in place
				claimedLine[br.MarkerLine] = true
			} else {
				br.Outcome = OutcomeInsertMarker // no own/shared marker → insert a fresh one
			}
			prevKey = key
		}
	}

	return out, nil
}

// MaxBlanksAroundMarker is the upper bound on consecutive blank lines on the
// "away" side of a marker pair (after the image for pos=after, before the
// comment for pos=before). Excess blanks accumulate from earlier tool
// behavior or hand-editing; the cleanup pass shrinks them back to this cap.
const MaxBlanksAroundMarker = 2

// ApplyMarkers rewrites lines so each block has its computed two-line marker,
// and returns the new line slice. Iterates back-to-front so insertions don't
// shift earlier indices.
//
// Placement honors br.NewMarker.Pos:
//   - "" or "after" (default): marker goes immediately below AnchorLine
//     (no blank between the closing fence and the comment line).
//   - "before": marker goes immediately above OpenLine (no blank between
//     the image line and the opening fence).
//
// On every run the tool also (a) strips one stale blank line between the
// fence and the marker (legacy from earlier versions of the code) and
// (b) caps consecutive blank lines on the away-side of the marker at
// MaxBlanksAroundMarker. Both cleanups keep refresh runs converging on a
// canonical layout without requiring users to hand-edit existing files.
//
// Behavior per outcome:
//   - Unterminated / Malformed / MissingSrc: line slice unchanged.
//   - OK: marker text unchanged, but stale adjacent blank lines are stripped.
//   - Rehash / Rerender (marker present): rewrite both marker lines in place;
//     strip stale adjacent blanks.
//   - InsertMarker / Rerender (marker absent): insert "comment + image"
//     adjacent to the chosen edge of the block. No leading or trailing blank.
func ApplyMarkers(lines []string, blocks []BlockResult) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	for i := len(blocks) - 1; i >= 0; i-- {
		br := blocks[i]
		switch br.Outcome {
		case OutcomeUnterminated, OutcomeMalformed, OutcomeMissingSrc,
			OutcomeNoEmbed, OutcomeBadMeta:
			continue
		case OutcomeRehash:
			rewriteMarker(out, br.MarkerLine, br.NewMarker)
			out = trimStaleSeparator(out, br)
		case OutcomeInsertMarker:
			out = insertMarker(out, br, br.NewMarker)
		case OutcomeRerender:
			if br.MarkerLine >= 0 {
				rewriteMarker(out, br.MarkerLine, br.NewMarker)
				out = trimStaleSeparator(out, br)
			} else {
				out = insertMarker(out, br, br.NewMarker)
			}
		case OutcomeOK:
			out = trimStaleSeparator(out, br)
		}
	}
	// Single pass to cap excess blank lines on the away-side of every marker
	// pair. Runs back-to-front so trims don't shift indices we haven't yet
	// inspected.
	out = capAwaySideBlanks(out, MaxBlanksAroundMarker)
	return out
}

// capAwaySideBlanks finds every (comment, image) marker pair in lines and
// trims consecutive blank lines on the side opposite the block (after the
// image for pos=after, before the comment for pos=before) to at most
// maxBlanks.
func capAwaySideBlanks(lines []string, maxBlanks int) []string {
	out := lines
	for i := len(out) - 2; i >= 0; i-- {
		mk, ok := ParseMarker(out[i], out[i+1])
		if !ok {
			continue
		}
		if mk.Pos == "before" {
			j := i - 1
			for j >= 0 && strings.TrimSpace(out[j]) == "" {
				j--
			}
			blankCount := i - 1 - j
			if blankCount > maxBlanks {
				removeFrom := j + 1 + maxBlanks
				removeCount := blankCount - maxBlanks
				out = removeRange(out, removeFrom, removeCount)
			}
		} else {
			j := i + 2
			for j < len(out) && strings.TrimSpace(out[j]) == "" {
				j++
			}
			blankCount := j - (i + 2)
			if blankCount > maxBlanks {
				removeFrom := i + 2 + maxBlanks
				removeCount := blankCount - maxBlanks
				out = removeRange(out, removeFrom, removeCount)
			}
		}
		// Step past the image line so we don't try to parse it as a comment.
		i--
	}
	return out
}

func removeRange(lines []string, start, count int) []string {
	if count <= 0 {
		return lines
	}
	out := make([]string, 0, len(lines)-count)
	out = append(out, lines[:start]...)
	out = append(out, lines[start+count:]...)
	return out
}

// rewriteMarker replaces the two-line marker starting at commentLine.
func rewriteMarker(lines []string, commentLine int, m Marker) {
	ml := m.FormatLines()
	lines[commentLine] = ml[0]
	if commentLine+1 < len(lines) {
		lines[commentLine+1] = ml[1]
	}
}

// insertMarker inserts the (comment, image) pair adjacent to the block at the
// position indicated by m.Pos. For "before", inserts (comment, image) right
// above OpenLine (the opening fence shifts down). For "after", inserts
// (comment, image) right below AnchorLine. No blank lines inserted on either
// side — the marker pair binds tightly to its fence.
func insertMarker(lines []string, br BlockResult, m Marker) []string {
	ml := m.FormatLines()
	if m.Pos == "before" && br.OpenLine > 0 {
		return spliceAt(lines, br.OpenLine, []string{ml[0], ml[1]})
	}
	return insertAfter(lines, br.AnchorLine, []string{ml[0], ml[1]})
}

// trimStaleSeparator removes exactly one blank line between the block and the
// adjacent marker pair, on whichever side the marker sits. This cleans up
// files written by earlier versions of the tool that always inserted a
// leading blank.
func trimStaleSeparator(lines []string, br BlockResult) []string {
	if br.MarkerLine < 0 {
		return lines
	}
	pos := br.NewMarker.Pos
	if pos == "before" {
		// Pattern: comment / image / blank / open-fence. Drop the blank.
		blankIdx := br.MarkerLine + 2
		fenceIdx := br.MarkerLine + 3
		if fenceIdx == br.OpenLine && blankIdx < len(lines) && strings.TrimSpace(lines[blankIdx]) == "" {
			return removeAt(lines, blankIdx)
		}
		return lines
	}
	// "after" (default). Pattern: close-fence / blank / comment / image.
	blankIdx := br.AnchorLine + 1
	if br.MarkerLine == br.AnchorLine+2 && blankIdx < len(lines) && strings.TrimSpace(lines[blankIdx]) == "" {
		return removeAt(lines, blankIdx)
	}
	return lines
}

func removeAt(lines []string, i int) []string {
	out := make([]string, 0, len(lines)-1)
	out = append(out, lines[:i]...)
	out = append(out, lines[i+1:]...)
	return out
}

// spliceAt inserts items at index i, so items become lines[i..i+len(items)-1]
// and the original lines[i:] shifts down.
func spliceAt(lines []string, i int, items []string) []string {
	out := make([]string, 0, len(lines)+len(items))
	out = append(out, lines[:i]...)
	out = append(out, items...)
	out = append(out, lines[i:]...)
	return out
}

func insertAfter(lines []string, i int, items []string) []string {
	out := make([]string, 0, len(lines)+len(items))
	out = append(out, lines[:i+1]...)
	out = append(out, items...)
	out = append(out, lines[i+1:]...)
	return out
}
