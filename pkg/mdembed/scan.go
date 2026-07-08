package mdembed

import (
	"regexp"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// BlockKind tells where a block's ipmt source was declared.
type BlockKind string

const (
	KindVisible BlockKind = "visible" // ```ipmt fence in the .md
	KindInclude BlockKind = "include" // <!-- ipm-include src=... --> line referencing a sibling .ipmt
)

// SourceBlock is what the scanner produces. It carries enough information for
// AnalyzeMarkdown to locate the marker line that should sit just after it
// (default) or just before it (when the author has placed the marker pair
// above the block and set `pos=before`).
type SourceBlock struct {
	Kind BlockKind

	// OpenLine is the first line of the block — the opening ```ipmt fence
	// for visible blocks, the `<!-- ipm-include` line for include blocks.
	// A "before"-positioned marker pair is searched for immediately above
	// this line.
	OpenLine int

	// AnchorLine is the line right after which an "after"-positioned marker
	// is expected. For visible blocks it is the closing fence (or </details>
	// when the fence is wrapped). For include blocks it equals OpenLine —
	// the include line itself is the anchor.
	AnchorLine int

	// Content is the ipmt source text (LF-joined, no trailing newline). The
	// scanner sets it for KindVisible from the lines between the fences. For
	// KindInclude the scanner leaves it empty and AnalyzeMarkdown reads the
	// referenced file.
	Content string

	// Meta holds the space-separated tokens after "ipmt" in the opening fence
	// info-string (e.g. ```ipmt unresolved -> ["unresolved"]). Visible blocks
	// only; empty for a bare ```ipmt fence and for include blocks.
	Meta []string

	// Existing marker location and parsed contents, if a marker is already present.
	HasMarker  bool
	MarkerLine int
	Marker     Marker

	// Include-specific: the path written on the `<!-- ipm-include src=... -->`
	// line (relative to the .md file's directory), plus an optional explicit
	// id= attribute. Empty for visible blocks.
	SrcPathRel string
	ExplicitID string

	// Skip is true for blocks the scanner found but cannot process (e.g.
	// unterminated visible fence). AnalyzeMarkdown turns these into
	// OutcomeUnterminated.
	Skip       bool
	SkipReason string
}

var (
	includeLineRE  = regexp.MustCompile(`^\s*<!--\s+ipm-include\s+(.+?)\s+-->\s*$`)
	detailsOpenRE  = regexp.MustCompile(`^\s*<details\b`)
	detailsCloseRE = regexp.MustCompile(`^\s*</details>\s*$`)
)

// ScanBlocks walks lines (LF-normalized) once and finds every visible fence
// and every <!-- ipm-include --> line, in source order. It returns the line
// slice (the same one ApplyMarkers operates on) plus the blocks.
//
// Fence-aware (see pkg/markdown/fence.go): a non-ipmt fenced block — e.g.
// a ````md documentation example — is skipped wholesale, so a ```ipmt
// fence or an <!-- ipm-include --> line INSIDE it is literal text and is
// never embedded. This is what CommonMark renderers (GitHub, the VS Code
// preview) already do; embedding would insert a marker into the example.
func ScanBlocks(text string) (lines []string, blocks []SourceBlock) {
	text = markdown.NormalizeLF(text)
	lines = strings.Split(text, "\n")

	consumedMarkerLines := map[int]bool{}

	for i := 0; i < len(lines); {
		// <!-- ipm-include src=... --> line?
		if m := includeLineRE.FindStringSubmatch(lines[i]); m != nil {
			blk := scanInclude(i, m[1])
			attachMarker(&blk, lines, consumedMarkerLines)
			blocks = append(blocks, blk)
			i++
			continue
		}
		f, info, ok := markdown.ParseFenceOpen(lines[i])
		if !ok {
			i++
			continue
		}
		// Visible ```ipmt fence?
		if f.Char == '`' && markdown.IsIPMTInfo(info) {
			blk, next := scanVisible(lines, i, f)
			attachMarker(&blk, lines, consumedMarkerLines)
			blocks = append(blocks, blk)
			i = next
			continue
		}
		// Some other fenced block: skip its literal content entirely.
		i = markdown.SkipFence(lines, i, f)
	}
	return lines, blocks
}

// scanInclude parses the attribute list on an <!-- ipm-include src=... --> line.
func scanInclude(lineIdx int, attrs string) SourceBlock {
	blk := SourceBlock{
		Kind:       KindInclude,
		OpenLine:   lineIdx,
		AnchorLine: lineIdx,
	}
	for kv := range strings.FieldsSeq(attrs) {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key, val := kv[:eq], kv[eq+1:]
		switch key {
		case "src":
			blk.SrcPathRel = val
		case "id":
			blk.ExplicitID = val
		}
	}
	if blk.SrcPathRel == "" {
		blk.Skip = true
		blk.SkipReason = "ipm-include missing src= attribute"
	}
	return blk
}

// scanVisible expects lines[start] to be a ```ipmt opener with fence
// delimiter f. It returns the block and the index just past the closing
// fence — which must be at least as long as the opener (CommonMark), so a
// ```anything line inside a ````ipmt block stays content. If the fence is
// wrapped in a `<details><summary>...</summary>` ... `</details>` pair
// (for foldable source on GitHub), the returned OpenLine and AnchorLine
// are extended to those tags so the marker placement code targets the
// outside of the wrapper.
func scanVisible(lines []string, start int, f markdown.Fence) (SourceBlock, int) {
	end := -1
	for j := start + 1; j < len(lines); j++ {
		if f.ClosedBy(lines[j]) {
			end = j
			break
		}
	}
	if end == -1 {
		return SourceBlock{
			Kind:       KindVisible,
			OpenLine:   start,
			AnchorLine: start,
			Skip:       true,
			SkipReason: "unterminated ```ipmt fence",
		}, len(lines)
	}
	openLine, anchorLine, nextAfter := detailsWrapBounds(lines, start, end)
	return SourceBlock{
		Kind:       KindVisible,
		OpenLine:   openLine,
		AnchorLine: anchorLine,
		Content:    strings.Join(lines[start+1:end], "\n"),
		Meta:       markdown.FenceMeta(lines[start]), // shared with pkg/markdown
	}, nextAfter
}

// detailsWrapBounds checks whether the ```ipmt fence at [fenceStart, fenceEnd]
// is wrapped in a `<details>...</details>` pair (with at most one blank line
// between either tag and the fence). When wrapped, it returns expanded
// OpenLine/AnchorLine that point at the wrapper tags themselves, plus the
// index right after the </details> line so the outer scanner advances past
// the whole wrapped construct.
//
// When not wrapped, it returns (fenceStart, fenceEnd, fenceEnd+1) unchanged.
func detailsWrapBounds(lines []string, fenceStart, fenceEnd int) (openLine, anchorLine, nextAfter int) {
	openLine = fenceStart
	anchorLine = fenceEnd
	nextAfter = fenceEnd + 1

	probeUp := fenceStart - 1
	if probeUp >= 0 && strings.TrimSpace(lines[probeUp]) == "" {
		probeUp--
	}
	upMatches := probeUp >= 0 && detailsOpenRE.MatchString(lines[probeUp])

	probeDown := fenceEnd + 1
	if probeDown < len(lines) && strings.TrimSpace(lines[probeDown]) == "" {
		probeDown++
	}
	downMatches := probeDown < len(lines) && detailsCloseRE.MatchString(lines[probeDown])

	if upMatches && downMatches {
		openLine = probeUp
		anchorLine = probeDown
		nextAfter = probeDown + 1
	}
	return
}

// attachMarker searches for an existing marker pair attached to blk in either
// position. "after" (immediately below AnchorLine, one optional blank in
// between) is preferred — if no marker is found there, the search falls back
// to "before" (immediately above OpenLine, one optional blank in between).
func attachMarker(blk *SourceBlock, lines []string, consumed map[int]bool) {
	if blk.Skip {
		return
	}
	// Skip a marker pair already claimed by an earlier block: otherwise a
	// marker-less block whose neighbour's marker sits between them would steal
	// it (consumed was written but never read before this guard).
	if commentLine, m, ok := findMarkerAfter(lines, blk.AnchorLine); ok && !consumed[commentLine] {
		blk.HasMarker = true
		blk.MarkerLine = commentLine
		blk.Marker = m
		blk.Marker.Pos = "" // physical position wins; emitter omits pos=after
		consumed[commentLine] = true
		consumed[commentLine+1] = true
		return
	}
	if commentLine, m, ok := findMarkerBefore(lines, blk.OpenLine); ok && !consumed[commentLine] {
		blk.HasMarker = true
		blk.MarkerLine = commentLine
		blk.Marker = m
		blk.Marker.Pos = "before"
		consumed[commentLine] = true
		consumed[commentLine+1] = true
	}
}

// findMarkerAfter probes for a (comment, image) pair immediately after
// anchor, allowing one optional blank line in between.
func findMarkerAfter(lines []string, anchor int) (commentLine int, m Marker, ok bool) {
	if anchor < 0 || anchor+1 >= len(lines) {
		return -1, Marker{}, false
	}
	probe := anchor + 1
	if probe < len(lines) && strings.TrimSpace(lines[probe]) == "" {
		probe++
	}
	if probe+1 >= len(lines) {
		return -1, Marker{}, false
	}
	mk, ok := ParseMarker(lines[probe], lines[probe+1])
	if !ok {
		return -1, Marker{}, false
	}
	return probe, mk, true
}

// findMarkerBefore probes for a (comment, image) pair immediately above
// openLine, allowing one optional blank line in between.
func findMarkerBefore(lines []string, openLine int) (commentLine int, m Marker, ok bool) {
	if openLine <= 1 {
		return -1, Marker{}, false
	}
	imageIdx := openLine - 1
	if imageIdx >= 0 && strings.TrimSpace(lines[imageIdx]) == "" {
		imageIdx--
	}
	if imageIdx < 1 {
		return -1, Marker{}, false
	}
	mk, ok := ParseMarker(lines[imageIdx-1], lines[imageIdx])
	if !ok {
		return -1, Marker{}, false
	}
	return imageIdx - 1, mk, true
}
