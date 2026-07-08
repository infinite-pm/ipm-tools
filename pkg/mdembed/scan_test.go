package mdembed

import (
	"strings"
	"testing"
)

func TestScanBlocks_visibleOnly(t *testing.T) {
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 || blocks[0].Kind != KindVisible {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[0].Content != "A --> B" {
		t.Fatalf("content = %q", blocks[0].Content)
	}
	if blocks[0].HasMarker {
		t.Fatal("marker should not be present")
	}
}

// The ```ipmt-invalid lane is a foreign fence: a mixed doc must yield only the
// valid ```ipmt block, never the negative example — no SourceBlock, so no
// marker/SVG is ever produced for it.
func TestScanBlocks_ipmtInvalidLaneSkipped(t *testing.T) {
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
		"```ipmt-invalid",
		"A --> B",
		"B --> C",
		"A --> B",
		"```",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("expected only the valid block, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Kind != KindVisible || blocks[0].Content != "A --> B" {
		t.Fatalf("unexpected block: %+v", blocks[0])
	}
}

func TestScanBlocks_visibleWithMarker(t *testing.T) {
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
		"<!-- ipm-svg id=01 hash=ab12cd34 -->",
		"![](./_ipm/x/01.ipm.svg)",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	if !blocks[0].HasMarker {
		t.Fatal("marker should be attached")
	}
	if blocks[0].Marker.ID != "01" {
		t.Fatalf("marker id = %q", blocks[0].Marker.ID)
	}
}

func TestScanBlocks_include(t *testing.T) {
	src := strings.Join([]string{
		"prose",
		"",
		"<!-- ipm-include src=./block.ipmt -->",
		"<!-- ipm-svg id=block hash=deadbeef -->",
		"![](./_ipm/x/block.ipm.svg)",
		"",
		"more prose",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Kind != KindInclude {
		t.Fatalf("kind = %s", b.Kind)
	}
	if b.SrcPathRel != "./block.ipmt" {
		t.Fatalf("src = %q", b.SrcPathRel)
	}
	if !b.HasMarker || b.Marker.ID != "block" || b.Marker.Hash != "deadbeef" {
		t.Fatalf("marker = %+v", b.Marker)
	}
}

func TestScanBlocks_include_explicitID(t *testing.T) {
	src := "<!-- ipm-include src=./block.ipmt id=swap -->\n"
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	if blocks[0].ExplicitID != "swap" {
		t.Fatalf("ExplicitID = %q, want swap", blocks[0].ExplicitID)
	}
}

func TestScanBlocks_include_missingSrcIsMalformed(t *testing.T) {
	src := "<!-- ipm-include id=foo -->\n"
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 || !blocks[0].Skip {
		t.Fatalf("want 1 skipped block, got %+v", blocks)
	}
}

func TestScanBlocks_visibleAndIncludeInOrder(t *testing.T) {
	src := strings.Join([]string{
		"# title",
		"",
		"```ipmt",
		"V --> isible",
		"```",
		"",
		"<!-- ipm-include src=./b.ipmt -->",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Kind != KindVisible || blocks[1].Kind != KindInclude {
		t.Fatalf("kinds = %s, %s", blocks[0].Kind, blocks[1].Kind)
	}
}

func TestScanBlocks_detailsWrapped_recognizesMarkerOutside(t *testing.T) {
	// The ```ipmt fence is wrapped in <details><summary>...</summary>...</details>
	// for foldable source on GitHub. The marker sits outside the </details>.
	// Scanner must extend AnchorLine to </details> so the marker is found.
	src := strings.Join([]string{
		"<details><summary>ipmt</summary>",
		"",
		"```ipmt",
		"A --> B",
		"```",
		"</details>",
		"<!-- ipm-svg id=01 hash=ab12cd34 -->",
		"![](_ipm/x/01.ipm.svg)",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if !b.HasMarker {
		t.Fatalf("marker outside </details> not found; block=%+v", b)
	}
	if b.Marker.Hash != "ab12cd34" {
		t.Fatalf("marker hash = %q, want ab12cd34", b.Marker.Hash)
	}
	// OpenLine should point at <details>; AnchorLine should point at </details>.
	if b.OpenLine != 0 {
		t.Fatalf("OpenLine = %d, want 0 (<details> line)", b.OpenLine)
	}
	if b.AnchorLine != 5 {
		t.Fatalf("AnchorLine = %d, want 5 (</details> line)", b.AnchorLine)
	}
}

func TestScanBlocks_detailsWrapped_noBlanksInside(t *testing.T) {
	// Same as above but the wrap is tight (no blanks between tags and fence).
	src := strings.Join([]string{
		"<details><summary>ipmt</summary>",
		"```ipmt",
		"A --> B",
		"```",
		"</details>",
		"<!-- ipm-svg id=01 hash=ab12cd34 -->",
		"![](_ipm/x/01.ipm.svg)",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 || !blocks[0].HasMarker {
		t.Fatalf("scanner didn't recognize tight details wrap: %+v", blocks)
	}
}

func TestScanBlocks_detailsWrapped_onlyOpenTagDoesNotShift(t *testing.T) {
	// A stray <details> above without a matching </details> below should NOT
	// extend OpenLine — the wrap is recognized only when both tags are present.
	src := strings.Join([]string{
		"<details><summary>unrelated</summary>",
		"```ipmt",
		"A --> B",
		"```",
		"<!-- ipm-svg id=01 hash=ab12cd34 -->",
		"![](_ipm/x/01.ipm.svg)",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	if blocks[0].OpenLine != 1 {
		t.Fatalf("stray <details> shouldn't extend OpenLine; got %d, want 1", blocks[0].OpenLine)
	}
}

func TestScanBlocks_visibleWithMarkerBefore(t *testing.T) {
	src := strings.Join([]string{
		"intro paragraph",
		"",
		"<!-- ipm-svg id=01 hash=ab12cd34 pos=before -->",
		"![](./_ipm/x/01.ipm.svg)",
		"",
		"```ipmt",
		"A --> B",
		"```",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if !b.HasMarker {
		t.Fatal("marker should be attached (before the block)")
	}
	if b.Marker.Pos != "before" {
		t.Fatalf("scanner should record Pos=before, got %q", b.Marker.Pos)
	}
	// MarkerLine should point at the comment line (line 2 in this fixture).
	if b.MarkerLine != 2 {
		t.Fatalf("MarkerLine = %d, want 2 (comment line above the block)", b.MarkerLine)
	}
}

func TestScanBlocks_preferAfterOverBefore(t *testing.T) {
	// If a marker sits both before AND after, "after" wins (matches the
	// current default; user intent for "before" is signaled by relocating).
	src := strings.Join([]string{
		"<!-- ipm-svg id=01 hash=before00 pos=before -->",
		"![](./_ipm/x/before.svg)",
		"",
		"```ipmt",
		"A --> B",
		"```",
		"",
		"<!-- ipm-svg id=01 hash=after000 -->",
		"![](./_ipm/x/after.svg)",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if !b.HasMarker {
		t.Fatal("expected the after-marker to be attached")
	}
	if b.Marker.Hash != "after000" {
		t.Fatalf("scanner picked the wrong marker; got hash=%q want after000", b.Marker.Hash)
	}
}

func TestScanBlocks_markerNotStolenByNextBlock(t *testing.T) {
	// Block A's "after" marker sits between A and a marker-less block B. B's
	// "before" probe would otherwise find (and steal) A's marker; the consumed
	// guard must leave B marker-less.
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
		"<!-- ipm-svg id=01 hash=aaaa1111 -->",
		"![](./_ipm/x/a.svg)",
		"",
		"```ipmt",
		"C --> D",
		"```",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if !blocks[0].HasMarker || blocks[0].Marker.Hash != "aaaa1111" {
		t.Fatalf("block A should keep its after-marker; got HasMarker=%v hash=%q",
			blocks[0].HasMarker, blocks[0].Marker.Hash)
	}
	if blocks[1].HasMarker {
		t.Fatalf("block B must NOT steal block A's marker; got hash=%q", blocks[1].Marker.Hash)
	}
}

func TestScanBlocks_markerNotStolenByTightNextFence(t *testing.T) {
	// Tight layout: block A's "after" marker (comment + image) sits directly
	// above block B's opening fence with NO blank line in between — the
	// canonical shape the tool itself writes. B's "before" probe re-discovers
	// A's marker on the adjacent image line; the consumed guard must keep B
	// marker-less and must NOT let it inherit A's MarkerLine.
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"<!-- ipm-svg id=01 hash=aaaa1111 -->",
		"![](./_ipm/x/a.svg)",
		"```ipmt",
		"C --> D",
		"```",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if !blocks[0].HasMarker || blocks[0].Marker.Hash != "aaaa1111" || blocks[0].MarkerLine != 3 {
		t.Fatalf("block A should keep its after-marker on line 3; got HasMarker=%v hash=%q MarkerLine=%d",
			blocks[0].HasMarker, blocks[0].Marker.Hash, blocks[0].MarkerLine)
	}
	if blocks[1].HasMarker {
		t.Fatalf("block B must NOT inherit block A's tight marker; got hash=%q", blocks[1].Marker.Hash)
	}
	if blocks[1].MarkerLine == blocks[0].MarkerLine {
		t.Fatalf("block B MarkerLine (%d) must not equal block A's (%d)", blocks[1].MarkerLine, blocks[0].MarkerLine)
	}
}

func TestScanBlocks_orphanMarkerWithoutBlock(t *testing.T) {
	// A standalone marker that doesn't follow any visible fence or
	// ipm-include line is ignored — no synthetic block is created.
	src := "<!-- ipm-svg id=01 hash=ab12cd34 -->\n![](./x.svg)\n"
	_, blocks := ScanBlocks(src)
	if len(blocks) != 0 {
		t.Fatalf("want 0 blocks (orphan marker), got %d: %+v", len(blocks), blocks)
	}
}

// TestScanBlocks_nestedExampleIsLiteral pins the fence-nesting rule
// (pkg/markdown/fence.go): a ```ipmt fence inside a ````md documentation
// example — the docs/ipmt-unresolved.md pattern — is literal text. The
// old flat scanner detected it, and embed would have inserted a marker
// pair INSIDE the example on the next save.
func TestScanBlocks_nestedExampleIsLiteral(t *testing.T) {
	src := strings.Join([]string{
		"````md",
		"```ipmt unresolved",
		"deploy ::?etc --::X--> safety ::?etc",
		"```",
		"````",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 0 {
		t.Fatalf("nested example must produce no blocks; got %+v", blocks)
	}
}

// The tilde-wrapper form of a nested example (docs/md-embed.md shows
// its own syntax this way): backtick fences inside ~~~~md are content.
func TestScanBlocks_nestedInTildeWrapperIsLiteral(t *testing.T) {
	src := strings.Join([]string{
		"~~~~md",
		"```ipmt unresolved",
		"deploy ::?etc --::X--> safety ::?etc",
		"```",
		"~~~~",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 0 {
		t.Fatalf("tilde-wrapped example must produce no blocks; got %+v", blocks)
	}
}

// ipmt blocks are backtick fences only: a ~~~ipmt fence is an ordinary
// fenced block, never rendered or marker-paired.
func TestScanBlocks_tildeIpmtIsNotABlock(t *testing.T) {
	src := strings.Join([]string{
		"~~~ipmt",
		"p ::e --> q ::e",
		"~~~",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 0 {
		t.Fatalf("~~~ipmt must not be an ipmt block; got %+v", blocks)
	}
}

// An <!-- ipm-include --> line shown inside a fenced example is literal
// text too, not an include directive.
func TestScanBlocks_includeInsideFenceIsLiteral(t *testing.T) {
	src := strings.Join([]string{
		"```md",
		"<!-- ipm-include src=flow.ipmt -->",
		"```",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 0 {
		t.Fatalf("include inside fence must be literal; got %+v", blocks)
	}
}

// A real block after a closed example is still found and its marker
// attaches as usual.
func TestScanBlocks_realBlockAfterExampleKeepsMarker(t *testing.T) {
	src := strings.Join([]string{
		"````md",
		"```ipmt",
		"X --> Y",
		"```",
		"````",
		"",
		"```ipmt",
		"A --> B",
		"```",
		"<!-- ipm-svg id=01 hash=abcd1234 -->",
		"![](_ipm/doc/01.ipm.svg)",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %+v", blocks)
	}
	b := blocks[0]
	if b.Kind != KindVisible || b.Content != "A --> B" || !b.HasMarker {
		t.Fatalf("block = %+v", b)
	}
	if b.OpenLine != 6 || b.AnchorLine != 8 || b.MarkerLine != 9 {
		t.Fatalf("lines = open %d anchor %d marker %d", b.OpenLine, b.AnchorLine, b.MarkerLine)
	}
}

// A ````ipmt block's closer must be >= the opener (CommonMark): shorter
// runs and info-carrying ``` lines stay content.
func TestScanBlocks_longFenceCloserRule(t *testing.T) {
	src := strings.Join([]string{
		"````ipmt",
		"A --> B",
		"```",
		"````",
	}, "\n")
	_, blocks := ScanBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %+v", blocks)
	}
	if want := "A --> B\n```"; blocks[0].Content != want {
		t.Fatalf("content = %q, want %q", blocks[0].Content, want)
	}
}
