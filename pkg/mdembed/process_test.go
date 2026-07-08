package mdembed

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeMarkdown_insertMarker(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/docs/example.md"
	src := strings.Join([]string{
		"# title",
		"",
		"```ipmt",
		"A --> B",
		"```",
		"",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(got.Blocks))
	}
	b := got.Blocks[0]
	if b.Outcome != OutcomeInsertMarker {
		t.Fatalf("got outcome %s, want %s", b.Outcome, OutcomeInsertMarker)
	}
	// A fresh doc starts at the first base-36 key, "100".
	if b.NewMarker.ID != "100" || b.NewMarker.Hash == "" {
		t.Fatalf("new marker = %+v", b.NewMarker)
	}
	wantSVG := filepath.Join(root, "_ipm", "docs", "example", "100.ipm.svg")
	if b.SVGPath != wantSVG {
		t.Fatalf("svg path = %q, want %q", b.SVGPath, wantSVG)
	}
	if b.NewMarker.ImagePath != "../_ipm/docs/example/100.ipm.svg" {
		t.Fatalf("image path = %q", b.NewMarker.ImagePath)
	}
}

func TestAnalyzeMarkdown_okWhenHashMatches(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/docs/example.md"
	content := "A --> B"
	h := HashIPMT(content)

	src := strings.Join([]string{
		"```ipmt",
		content,
		"```",
		"",
		"<!-- ipm-svg id=01 hash=" + h + " -->",
		"![](../_ipm/docs/example/01.ipm.svg)",
		"",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].Outcome != OutcomeOK {
		t.Fatalf("got %s, want %s; block = %+v", got.Blocks[0].Outcome, OutcomeOK, got.Blocks[0])
	}
}

func TestAnalyzeMarkdown_rehashWhenSourceChanges(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/x.md"
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
		"<!-- ipm-svg id=01 hash=00000000 -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	b := got.Blocks[0]
	if b.Outcome != OutcomeRehash {
		t.Fatalf("got %s, want %s", b.Outcome, OutcomeRehash)
	}
	if b.NewMarker.Hash == "00000000" {
		t.Fatal("hash was not updated")
	}
}

func TestApplyMarkers_insert(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/x.md"
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "<!-- ipm-svg id=100 hash=") {
		t.Fatalf("apply did not insert marker; got:\n%s", joined)
	}
	// Marker should sit *immediately* after the closing fence — no blank
	// line between them.
	wantContains := "```\n<!-- ipm-svg id=100 hash="
	if !strings.Contains(joined, wantContains) {
		t.Fatalf("marker not placed tight against fence; got:\n%s", joined)
	}
	// Conversely, never `fence \n blank \n marker`.
	if strings.Contains(joined, "```\n\n<!-- ipm-svg ") {
		t.Fatalf("blank line snuck in between fence and marker; got:\n%s", joined)
	}
}

func TestApplyMarkers_rehash(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/x.md"
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
		"<!-- ipm-svg id=01 hash=00000000 -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	if strings.Contains(strings.Join(out, "\n"), "hash=00000000") {
		t.Fatal("stale hash not replaced")
	}
}

func TestApplyMarkers_multipleBlocksRetainOrder(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/x.md"
	src := strings.Join([]string{
		"```ipmt",
		"X",
		"```",
		"middle",
		"```ipmt",
		"Y",
		"```",
		"",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	if strings.Count(joined, "<!-- ipm-svg ") != 2 {
		t.Fatalf("want 2 markers, got %d in:\n%s", strings.Count(joined, "<!-- ipm-svg "), joined)
	}
	// Block 1 gets the first key, block 2 the next one, after "middle".
	idx1 := strings.Index(joined, "id=100")
	idxMid := strings.Index(joined, "middle")
	idx2 := strings.Index(joined, "id=110")
	if !(idx1 < idxMid && idxMid < idx2) {
		t.Fatalf("markers out of order; idx1=%d mid=%d idx2=%d\n%s", idx1, idxMid, idx2, joined)
	}
}

func TestAnalyzeMarkdown_posBeforeIsPreserved(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/x.md"
	content := "A --> B"
	h := HashIPMT(content)
	src := strings.Join([]string{
		"intro",
		"",
		"<!-- ipm-svg id=01 hash=" + h + " pos=before -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
		"```ipmt",
		content,
		"```",
		"",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(got.Blocks))
	}
	b := got.Blocks[0]
	if b.Outcome != OutcomeOK {
		t.Fatalf("outcome = %s, want OK; block=%+v", b.Outcome, b)
	}
	if b.NewMarker.Pos != "before" {
		t.Fatalf("NewMarker.Pos = %q, want before", b.NewMarker.Pos)
	}
}

func TestAnalyzeMarkdown_posBeforeRehashRewritesInPlace(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/x.md"
	src := strings.Join([]string{
		"<!-- ipm-svg id=01 hash=00000000 pos=before -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
		"```ipmt",
		"A --> B",
		"```",
		"",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if a.Blocks[0].Outcome != OutcomeRehash {
		t.Fatalf("outcome = %s, want Rehash", a.Blocks[0].Outcome)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "hash=00000000") {
		t.Fatalf("stale hash not replaced:\n%s", joined)
	}
	if !strings.Contains(joined, "pos=before") {
		t.Fatalf("pos=before should be preserved on rewrite:\n%s", joined)
	}
	// Comment line still precedes the fence — the marker stayed above.
	idxComment := strings.Index(joined, "<!-- ipm-svg")
	idxFence := strings.Index(joined, "```ipmt")
	if idxComment >= idxFence {
		t.Fatalf("marker should still sit before the fence:\n%s", joined)
	}
}

func TestApplyMarkers_CapsExcessTrailingBlanks(t *testing.T) {
	// Five blank lines after the image — accumulated from hand-editing or
	// older tool versions. ApplyMarkers should cap at MaxBlanksAroundMarker.
	root := "/repo"
	mdAbs := "/repo/x.md"
	content := "A --> B"
	h := HashIPMT(content)
	src := strings.Join([]string{
		"```ipmt",
		content,
		"```",
		"<!-- ipm-svg id=01 hash=" + h + " -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
		"",
		"",
		"",
		"",
		"trailing",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	// At most 2 consecutive blank lines between the image and "trailing".
	if strings.Contains(joined, ".svg)\n\n\n\ntrailing") {
		t.Fatalf("expected ≤2 blanks after image; got:\n%s", joined)
	}
	// And at least 1 blank — we shouldn't *grow* tight layouts. The fixture
	// had 5, so 2 is the result.
	want := ".svg)\n\n\ntrailing"
	if !strings.Contains(joined, want) {
		t.Fatalf("expected exactly 2 blanks; got:\n%s", joined)
	}
}

func TestApplyMarkers_CapsExcessLeadingBlanks_posBefore(t *testing.T) {
	root := "/repo"
	mdAbs := "/repo/x.md"
	content := "A --> B"
	h := HashIPMT(content)
	src := strings.Join([]string{
		"intro",
		"",
		"",
		"",
		"",
		"",
		"<!-- ipm-svg id=01 hash=" + h + " pos=before -->",
		"![](_ipm/x/01.ipm.svg)",
		"```ipmt",
		content,
		"```",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "intro\n\n\n\n<!-- ipm-svg") {
		t.Fatalf("expected ≤2 blanks before marker; got:\n%s", joined)
	}
	if !strings.Contains(joined, "intro\n\n\n<!-- ipm-svg") {
		t.Fatalf("expected exactly 2 blanks before marker; got:\n%s", joined)
	}
}

func TestApplyMarkers_TwoBlanksUntouched(t *testing.T) {
	// Exactly MaxBlanksAroundMarker blanks already present — leave alone.
	root := "/repo"
	mdAbs := "/repo/x.md"
	content := "A --> B"
	h := HashIPMT(content)
	src := strings.Join([]string{
		"```ipmt",
		content,
		"```",
		"<!-- ipm-svg id=01 hash=" + h + " -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
		"",
		"trailing",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	if strings.Join(out, "\n") != src {
		t.Fatalf("file shouldn't change when blanks are within the cap; got:\n%s", strings.Join(out, "\n"))
	}
}

func TestApplyMarkers_OKTrimsStaleSeparator(t *testing.T) {
	// File written by an older version of the tool: a blank line sits between
	// the closing fence and the marker. Marker hash matches the source, so
	// outcome is OK — but ApplyMarkers should still remove the stale blank.
	root := "/repo"
	mdAbs := "/repo/x.md"
	content := "A --> B"
	h := HashIPMT(content)
	src := strings.Join([]string{
		"```ipmt",
		content,
		"```",
		"",
		"<!-- ipm-svg id=01 hash=" + h + " -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if a.Blocks[0].Outcome != OutcomeOK {
		t.Fatalf("precondition: expected OK, got %s", a.Blocks[0].Outcome)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "```\n\n<!-- ipm-svg ") {
		t.Fatalf("stale blank between fence and marker was not trimmed; got:\n%s", joined)
	}
	if !strings.Contains(joined, "```\n<!-- ipm-svg ") {
		t.Fatalf("marker should sit immediately after fence; got:\n%s", joined)
	}
}

func TestApplyMarkers_OKTrimsStaleSeparator_posBefore(t *testing.T) {
	// Symmetric case: pos=before with a stale blank between the image and the
	// opening fence.
	root := "/repo"
	mdAbs := "/repo/x.md"
	content := "A --> B"
	h := HashIPMT(content)
	src := strings.Join([]string{
		"intro",
		"",
		"<!-- ipm-svg id=01 hash=" + h + " pos=before -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
		"```ipmt",
		content,
		"```",
		"",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if a.Blocks[0].Outcome != OutcomeOK {
		t.Fatalf("precondition: expected OK, got %s", a.Blocks[0].Outcome)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, ".svg)\n\n```ipmt") {
		t.Fatalf("stale blank between image and opening fence was not trimmed; got:\n%s", joined)
	}
	if !strings.Contains(joined, ".svg)\n```ipmt") {
		t.Fatalf("marker should sit immediately above fence; got:\n%s", joined)
	}
}

func TestApplyMarkers_insertNewDefaultsToAfter(t *testing.T) {
	// No existing marker → tool inserts AFTER, never before.
	root := "/repo"
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
	}, "\n")
	a, err := AnalyzeMarkdown(filepath.Join(root, "x.md"), src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	idxFence := strings.Index(joined, "```ipmt")
	idxCloseFence := strings.LastIndex(joined, "```")
	idxComment := strings.Index(joined, "<!-- ipm-svg")
	if !(idxFence < idxCloseFence && idxCloseFence < idxComment) {
		t.Fatalf("first-time insert should default to after-fence; got:\n%s", joined)
	}
	if strings.Contains(joined, "pos=") {
		t.Fatalf("default after-marker should not carry pos= attribute:\n%s", joined)
	}
}

func TestAnalyzeMarkdown_unterminatedFence(t *testing.T) {
	root := "/repo"
	src := "```ipmt\nA --> B\n"
	got, err := AnalyzeMarkdown(filepath.Join(root, "x.md"), src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].Outcome != OutcomeUnterminated {
		t.Fatalf("got %s, want %s", got.Blocks[0].Outcome, OutcomeUnterminated)
	}
}

func TestAnalyzeMarkdown_include_insertMarker(t *testing.T) {
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	content := "A --> B"
	reader := func(_ string) ([]byte, error) { return []byte(content), nil }

	src := "<!-- ipm-include src=./block.ipmt -->\n"
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root, SrcReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(got.Blocks))
	}
	b := got.Blocks[0]
	if b.Kind != KindInclude {
		t.Fatalf("kind = %s", b.Kind)
	}
	if b.Outcome != OutcomeInsertMarker {
		t.Fatalf("outcome = %s", b.Outcome)
	}
	// Default id should be derived from the .ipmt filename.
	if b.NewMarker.ID != "block" {
		t.Fatalf("expected id=block (from src filename), got %q", b.NewMarker.ID)
	}
}

func TestAnalyzeMarkdown_include_explicitIDOverridesFilename(t *testing.T) {
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	reader := func(_ string) ([]byte, error) { return []byte("A --> B"), nil }

	src := "<!-- ipm-include src=./block.ipmt id=swap -->\n"
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root, SrcReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].NewMarker.ID != "swap" {
		t.Fatalf("explicit id should win; got %q", got.Blocks[0].NewMarker.ID)
	}
}

func TestAnalyzeMarkdown_include_okWhenHashMatches(t *testing.T) {
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	content := "A --> B"
	h := HashIPMT(content)
	reader := func(_ string) ([]byte, error) { return []byte(content), nil }

	src := strings.Join([]string{
		"<!-- ipm-include src=./block.ipmt -->",
		"<!-- ipm-svg id=block hash=" + h + " -->",
		"![](./_ipm/x/block.ipm.svg)",
		"",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root, SrcReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].Outcome != OutcomeOK {
		t.Fatalf("outcome = %s; block=%+v", got.Blocks[0].Outcome, got.Blocks[0])
	}
}

func TestAnalyzeMarkdown_include_rehashWhenSourceFileEdited(t *testing.T) {
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	reader := func(_ string) ([]byte, error) { return []byte("A --> NEW"), nil }

	src := strings.Join([]string{
		"<!-- ipm-include src=./block.ipmt -->",
		"<!-- ipm-svg id=block hash=00000000 -->",
		"![](./_ipm/x/block.ipm.svg)",
		"",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root, SrcReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].Outcome != OutcomeRehash {
		t.Fatalf("outcome = %s", got.Blocks[0].Outcome)
	}
}

func TestAnalyzeMarkdown_include_missingSrc(t *testing.T) {
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	src := "<!-- ipm-include src=./does-not-exist.ipmt -->\n"
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{
		Root:      root,
		SrcReader: func(_ string) ([]byte, error) { return nil, errFakeNotExist },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].Outcome != OutcomeMissingSrc {
		t.Fatalf("outcome = %s", got.Blocks[0].Outcome)
	}
	if got.Blocks[0].SkipReason == "" {
		t.Fatal("missing src outcome should carry a SkipReason")
	}
}

func TestAnalyzeMarkdown_duplicateIDReassigned(t *testing.T) {
	// id= must be unique within a single .md file. When two blocks claim the
	// same id the FIRST keeps it; the later one is reassigned a fresh
	// between-key (rather than being flagged and left colliding), so the two
	// blocks land on distinct SVGs.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	contentA := "A --> B"
	contentB := "C --> D"
	hA := HashIPMT(contentA)
	hB := HashIPMT(contentB)
	src := strings.Join([]string{
		"```ipmt",
		contentA,
		"```",
		"<!-- ipm-svg id=01 hash=" + hA + " -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
		"```ipmt",
		contentB,
		"```",
		"<!-- ipm-svg id=01 hash=" + hB + " -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(got.Blocks))
	}
	// First block keeps the contested id.
	if got.Blocks[0].NewMarker.ID != "01" {
		t.Fatalf("first block should keep id=01; got %q", got.Blocks[0].NewMarker.ID)
	}
	// Second block is re-keyed to something distinct, and its SVG follows.
	b1 := got.Blocks[1]
	if b1.NewMarker.ID == "01" || b1.NewMarker.ID == "" {
		t.Fatalf("second block should be reassigned a fresh id; got %q", b1.NewMarker.ID)
	}
	if !IsKey(b1.NewMarker.ID) {
		t.Fatalf("reassigned id %q should be a base-36 key", b1.NewMarker.ID)
	}
	if got.Blocks[0].SVGPath == b1.SVGPath {
		t.Fatalf("blocks must land on distinct SVGs; both %q", b1.SVGPath)
	}
}

func TestAnalyzeMarkdown_tightMarkerlessSecondBlockGetsFreshID(t *testing.T) {
	// Tight layout: block A's marker sits directly above block B's opening
	// fence (no blank line). The scanner must leave B marker-less, so
	// AnalyzeMarkdown assigns B a fresh positional id and OutcomeInsertMarker —
	// NOT OutcomeDuplicateID from inheriting block A's id.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	hA := HashIPMT("A --> B")
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"<!-- ipm-svg id=01 hash=" + hA + " -->",
		"![](_ipm/x/01.ipm.svg)",
		"```ipmt",
		"C --> D",
		"```",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(got.Blocks))
	}
	if got.Blocks[0].Outcome != OutcomeOK || got.Blocks[0].NewMarker.ID != "01" {
		t.Fatalf("block A should stay OK with id=01; got outcome=%s id=%q",
			got.Blocks[0].Outcome, got.Blocks[0].NewMarker.ID)
	}
	b := got.Blocks[1]
	if b.Outcome != OutcomeInsertMarker {
		t.Fatalf("block B outcome = %s, want %s (must not be DuplicateID)", b.Outcome, OutcomeInsertMarker)
	}
	if b.NewMarker.ID == "01" || b.NewMarker.ID == "" {
		t.Fatalf("block B should get a fresh id distinct from block A's 01; got %q", b.NewMarker.ID)
	}
	if b.MarkerLine != -1 {
		t.Fatalf("block B should carry no existing marker (MarkerLine -1); got %d", b.MarkerLine)
	}
}

func TestAnalyzeMarkdown_splitBlockFirstHalfGetsFreeID(t *testing.T) {
	// Splitting one ```ipmt block in two leaves the original marker on the
	// SECOND half; the first half becomes a new marker-less block at the old
	// block's document position. The positional fallback must not hand the
	// first half an id another block still owns — that would steal the owner's
	// SVG path (overwriting its rendered file) and leave the owner flagged
	// duplicate-id and skipped on every subsequent run, its marker hash stale
	// forever.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	cFirst, cSecond, cThird := "A --> B", "C --> D", "E --> F"
	src := strings.Join([]string{
		"```ipmt", cFirst, "```", // first half of the split: no marker yet
		"```ipmt", cSecond, "```", // second half: kept the pre-split marker
		"<!-- ipm-svg id=01 hash=00000000 -->", // stale pre-split hash
		"![](_ipm/x/01.ipm.svg)", "",
		"```ipmt", cThird, "```", // unrelated block further down
		"<!-- ipm-svg id=02 hash=" + HashIPMT(cThird) + " -->",
		"![](_ipm/x/02.ipm.svg)", "",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(got.Blocks))
	}
	// First half: fresh insert under a free id — not 01 (owned by the second
	// half) and not 02 (owned by the third block).
	b0 := got.Blocks[0]
	if b0.Outcome != OutcomeInsertMarker {
		t.Fatalf("first half outcome = %s, want %s", b0.Outcome, OutcomeInsertMarker)
	}
	if b0.NewMarker.ID == "01" || b0.NewMarker.ID == "02" || b0.NewMarker.ID == "" {
		t.Fatalf("first half id = %q, want a free id distinct from existing 01/02", b0.NewMarker.ID)
	}
	if !IsKey(b0.NewMarker.ID) {
		t.Fatalf("first half id = %q, want a base-36 key", b0.NewMarker.ID)
	}
	if !strings.HasSuffix(filepath.ToSlash(b0.SVGPath), b0.NewMarker.ID+".ipm.svg") ||
		!strings.HasSuffix(b0.NewMarker.ImagePath, b0.NewMarker.ID+".ipm.svg") {
		t.Fatalf("first half paths must follow its id %q: img=%q svg=%q",
			b0.NewMarker.ID, b0.NewMarker.ImagePath, b0.SVGPath)
	}
	// Second half: keeps its id and simply rehashes in place — it must NOT be
	// flagged duplicate-id (the collision would be of the tool's own making).
	b1 := got.Blocks[1]
	if b1.Outcome != OutcomeRehash {
		t.Fatalf("second half outcome = %s, want %s", b1.Outcome, OutcomeRehash)
	}
	if b1.NewMarker.ID != "01" || b1.NewMarker.Hash != HashIPMT(cSecond) {
		t.Fatalf("second half must keep id=01 and rehash; got id=%q hash=%q", b1.NewMarker.ID, b1.NewMarker.Hash)
	}
	// Third block: untouched.
	b2 := got.Blocks[2]
	if b2.Outcome != OutcomeOK || b2.NewMarker.ID != "02" {
		t.Fatalf("third block outcome=%s id=%q, want ok id=02", b2.Outcome, b2.NewMarker.ID)
	}
	// And the three blocks must land on three distinct SVGs.
	if b0.SVGPath == b1.SVGPath || b0.SVGPath == b2.SVGPath || b1.SVGPath == b2.SVGPath {
		t.Fatalf("svg paths must be distinct; got %q / %q / %q", b0.SVGPath, b1.SVGPath, b2.SVGPath)
	}
}

func TestAnalyzeMarkdown_uniqueNonKeyIDsPreserved(t *testing.T) {
	// Distinct hand-written (non-key) ids are intentional: both are preserved
	// as-is, so their SVG filenames never churn.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	hA := HashIPMT("A --> B")
	hB := HashIPMT("C --> D")
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"<!-- ipm-svg id=a hash=" + hA + " -->",
		"![](_ipm/x/a.ipm.svg)",
		"",
		"```ipmt",
		"C --> D",
		"```",
		"<!-- ipm-svg id=b hash=" + hB + " -->",
		"![](_ipm/x/b.ipm.svg)",
		"",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(got.Blocks))
	}
	if got.Blocks[0].NewMarker.ID != "a" || got.Blocks[1].NewMarker.ID != "b" {
		t.Fatalf("unique non-key ids must be preserved; got %q and %q",
			got.Blocks[0].NewMarker.ID, got.Blocks[1].NewMarker.ID)
	}
}

func TestApplyMarkers_duplicateIDRewrittenWithFreshKey(t *testing.T) {
	// A duplicated id is resolved rather than refused: the later block is
	// re-keyed, so ApplyMarkers rewrites its marker onto the fresh id (and its
	// real hash) instead of leaving two blocks pointing at one SVG.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	contentA := "A --> B"
	contentB := "C --> D"
	hA := HashIPMT(contentA)
	src := strings.Join([]string{
		"```ipmt",
		contentA,
		"```",
		"<!-- ipm-svg id=01 hash=" + hA + " -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
		"```ipmt",
		contentB,
		"```",
		"<!-- ipm-svg id=01 hash=00000000 -->",
		"![](_ipm/x/01.ipm.svg)",
		"",
	}, "\n")
	a, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyMarkers(a.Lines, a.Blocks)
	joined := strings.Join(out, "\n")
	// The stale placeholder hash is gone: the re-keyed block now carries its
	// own id and the real hash of contentB.
	if strings.Contains(joined, "hash=00000000") {
		t.Fatalf("duplicate block should have been rewritten; output:\n%s", joined)
	}
	if !strings.Contains(joined, "hash="+HashIPMT(contentB)) {
		t.Fatalf("re-keyed block should carry contentB's hash; output:\n%s", joined)
	}
	newID := a.Blocks[1].NewMarker.ID
	if newID == "01" || !IsKey(newID) {
		t.Fatalf("second block should hold a fresh key; got %q", newID)
	}
	if !strings.Contains(joined, "id="+newID) {
		t.Fatalf("marker for the fresh key %q not written; output:\n%s", newID, joined)
	}
}

var errFakeNotExist = stubErr("file does not exist (fake)")

type stubErr string

func (e stubErr) Error() string { return string(e) }

func TestAnalyzeMarkdown_base36_duplicateReassigned(t *testing.T) {
	// In a base-36 keyed file (all marker ids are valid 3-char keys) a duplicate
	// is REASSIGNED a fresh key (not flagged/skipped), so it stops pointing at the
	// first block's SVG — the bug that motivated the scheme.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	cA, cB := "A --> B", "C --> D"
	src := strings.Join([]string{
		"```ipmt", cA, "```",
		"<!-- ipm-svg id=100 hash=" + HashIPMT(cA) + " -->",
		"![](_ipm/x/100.ipm.svg)", "",
		"```ipmt", cB, "```",
		"<!-- ipm-svg id=100 hash=" + HashIPMT(cB) + " -->",
		"![](_ipm/x/100.ipm.svg)", "",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].NewMarker.ID != "100" {
		t.Fatalf("first block id = %q, want 100 (preserved)", got.Blocks[0].NewMarker.ID)
	}
	b1 := got.Blocks[1]
	if b1.NewMarker.ID == "100" || !IsKey(b1.NewMarker.ID) {
		t.Fatalf("second block id = %q, want a fresh valid key != 100", b1.NewMarker.ID)
	}
	if !strings.HasSuffix(b1.NewMarker.ImagePath, b1.NewMarker.ID+".ipm.svg") {
		t.Fatalf("image path %q must follow the new id %q", b1.NewMarker.ImagePath, b1.NewMarker.ID)
	}
}

func TestAnalyzeMarkdown_base36_newBlockGetsBetweenKey(t *testing.T) {
	// A new (marker-less) block inserted between two base-36 keys gets a key
	// strictly between them, so document order == key order with no renumber.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	cA, cMid, cB := "A --> B", "E --> F", "C --> D"
	src := strings.Join([]string{
		"```ipmt", cA, "```",
		"<!-- ipm-svg id=100 hash=" + HashIPMT(cA) + " -->",
		"![](_ipm/x/100.ipm.svg)", "",
		"```ipmt", cMid, "```", "", // new block, no marker, between 100 and 130
		"```ipmt", cB, "```",
		"<!-- ipm-svg id=130 hash=" + HashIPMT(cB) + " -->",
		"![](_ipm/x/130.ipm.svg)", "",
	}, "\n")
	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	mid := got.Blocks[1].NewMarker.ID
	if !IsKey(mid) || mid <= "100" || mid >= "130" {
		t.Fatalf("inserted block id = %q, want a key strictly between 100 and 130", mid)
	}
	if got.Blocks[1].Outcome != OutcomeInsertMarker {
		t.Fatalf("inserted block outcome = %s, want insert-marker", got.Blocks[1].Outcome)
	}
}

// legacyDupSrc is a legacy positional-numeric doc whose third block duplicates
// the first block's id — the exact shape the migration must repair.
func legacyDupSrc() (cA, cB, cC, src string) {
	cA, cB, cC = "A --> B", "C --> D", "E --> F"
	src = strings.Join([]string{
		"```ipmt", cA, "```",
		"<!-- ipm-svg id=01 hash=" + HashIPMT(cA) + " -->",
		"![](_ipm/x/01.ipm.svg)", "",
		"```ipmt", cB, "```",
		"<!-- ipm-svg id=02 hash=" + HashIPMT(cB) + " -->",
		"![](_ipm/x/02.ipm.svg)", "",
		"```ipmt", cC, "```",
		"<!-- ipm-svg id=01 hash=" + HashIPMT(cC) + " -->",
		"![](_ipm/x/01.ipm.svg)", "",
	}, "\n")
	return
}

func TestAnalyzeMarkdown_existingIDsPreservedOnlyDuplicateRekeyed(t *testing.T) {
	// An id already written into a marker is intentional and is preserved
	// verbatim — even a short numeric one — so its SVG never churns. Only the
	// block whose id duplicates an earlier block's is re-keyed, and only that
	// block records the SVG it renamed away from.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	_, _, _, src := legacyDupSrc() // ids 01, 02, 01

	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(got.Blocks))
	}
	if got.Blocks[0].NewMarker.ID != "01" || got.Blocks[1].NewMarker.ID != "02" {
		t.Fatalf("existing ids must be preserved; got %q,%q",
			got.Blocks[0].NewMarker.ID, got.Blocks[1].NewMarker.ID)
	}
	b2 := got.Blocks[2]
	if b2.NewMarker.ID == "01" || !IsKey(b2.NewMarker.ID) {
		t.Fatalf("the duplicate must be re-keyed to a fresh key; got %q", b2.NewMarker.ID)
	}
	if !strings.HasSuffix(b2.NewMarker.ImagePath, b2.NewMarker.ID+".ipm.svg") ||
		!strings.HasSuffix(filepath.ToSlash(b2.SVGPath), b2.NewMarker.ID+".ipm.svg") {
		t.Fatalf("re-keyed paths must follow the new id %q: img=%q svg=%q",
			b2.NewMarker.ID, b2.NewMarker.ImagePath, b2.SVGPath)
	}
	if got.Blocks[0].RenamedFromSVGPath != "" || got.Blocks[1].RenamedFromSVGPath != "" {
		t.Fatal("preserved blocks must not record a rename")
	}
	wantOld := filepath.Join(root, "_ipm", "x", "01.ipm.svg")
	if b2.RenamedFromSVGPath != wantOld {
		t.Fatalf("re-keyed block RenamedFromSVGPath = %q, want %q", b2.RenamedFromSVGPath, wantOld)
	}
}

func TestAnalyzeMarkdown_preservesIncludeIDs(t *testing.T) {
	// An include's meaningful filename id (swap.ipm.svg) is intentional and is
	// preserved — a rename would clobber a human-chosen name. It also must not
	// act as a keyspace bound for its neighbours.
	root := "/repo"
	mdAbs := filepath.Join(root, "x.md")
	incContent, visContent := "P --> Q", "A --> B"
	reader := func(_ string) ([]byte, error) { return []byte(incContent), nil }
	src := strings.Join([]string{
		"<!-- ipm-include src=./swap.ipmt -->",
		"<!-- ipm-svg id=swap hash=" + HashIPMT(incContent) + " -->",
		"![](_ipm/x/swap.ipm.svg)", "",
		"```ipmt", visContent, "```",
		"<!-- ipm-svg id=01 hash=" + HashIPMT(visContent) + " -->",
		"![](_ipm/x/01.ipm.svg)", "",
	}, "\n")

	got, err := AnalyzeMarkdown(mdAbs, src, AnalyzeOptions{Root: root, SrcReader: reader})
	if err != nil {
		t.Fatal(err)
	}
	inc, vis := got.Blocks[0], got.Blocks[1]
	if inc.Kind != KindInclude || inc.NewMarker.ID != "swap" {
		t.Fatalf("include id = %q (kind %s), want preserved id=swap", inc.NewMarker.ID, inc.Kind)
	}
	if inc.RenamedFromSVGPath != "" {
		t.Fatalf("include must not be renamed; RenamedFromSVGPath = %q", inc.RenamedFromSVGPath)
	}
	// The visible block's own id is likewise preserved, and neither block is
	// renamed.
	if vis.NewMarker.ID != "01" {
		t.Fatalf("visible id = %q, want preserved id=01", vis.NewMarker.ID)
	}
	if vis.RenamedFromSVGPath != "" {
		t.Fatalf("visible must not be renamed; RenamedFromSVGPath = %q", vis.RenamedFromSVGPath)
	}
}
