package markdown

import (
	"strings"
	"testing"
)

func TestScanIPMTBlocks_empty(t *testing.T) {
	lines, blocks := ScanIPMTBlocks("")
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("lines = %#v", lines)
	}
	if len(blocks) != 0 {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestScanIPMTBlocks_noBlocks(t *testing.T) {
	_, blocks := ScanIPMTBlocks("# heading\n\nbody\n")
	if len(blocks) != 0 {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestScanIPMTBlocks_singleBlock(t *testing.T) {
	src := strings.Join([]string{
		"# title",
		"",
		"```ipmt",
		"A --> B",
		"B --> C",
		"```",
		"",
		"trailing",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Index != 1 || b.StartLine != 2 || b.EndLine != 5 {
		t.Fatalf("got block %+v", b)
	}
	if b.Content != "A --> B\nB --> C" {
		t.Fatalf("content = %q", b.Content)
	}
}

func TestScanIPMTBlocks_multiple(t *testing.T) {
	src := strings.Join([]string{
		"```ipmt",
		"X",
		"```",
		"middle",
		"```ipmt",
		"Y",
		"Z",
		"```",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 2 {
		t.Fatalf("want 2, got %d", len(blocks))
	}
	if blocks[0].Index != 1 || blocks[0].Content != "X" {
		t.Fatalf("block0 = %+v", blocks[0])
	}
	if blocks[1].Index != 2 || blocks[1].Content != "Y\nZ" || blocks[1].StartLine != 4 {
		t.Fatalf("block1 = %+v", blocks[1])
	}
}

func TestScanIPMTBlocks_skipsNonIPMTFence(t *testing.T) {
	src := strings.Join([]string{
		"```go",
		"func main() {}",
		"```",
		"",
		"```ipmt",
		"A --> B",
		"```",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1, got %d", len(blocks))
	}
	if blocks[0].StartLine != 4 || blocks[0].Content != "A --> B" {
		t.Fatalf("block = %+v", blocks[0])
	}
}

func TestScanIPMTBlocks_unterminated(t *testing.T) {
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"oops no fence",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1, got %d", len(blocks))
	}
	b := blocks[0]
	if b.EndLine != -1 {
		t.Fatalf("want unterminated (EndLine=-1), got %+v", b)
	}
	if b.Content != "A --> B\noops no fence" {
		t.Fatalf("content = %q", b.Content)
	}
}

func TestScanIPMTBlocks_crlfNormalized(t *testing.T) {
	src := "```ipmt\r\nA --> B\r\n```\r\n"
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 1 || blocks[0].Content != "A --> B" {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestNormalizeLF(t *testing.T) {
	cases := map[string]string{
		"a\r\nb":   "a\nb",
		"a\rb":     "a\nb",
		"a\nb":     "a\nb",
		"":         "",
		"a\r\n\rb": "a\n\nb",
	}
	for in, want := range cases {
		if got := NormalizeLF(in); got != want {
			t.Errorf("NormalizeLF(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScanIPMTBlocks_nestedExampleIsLiteral pins the CommonMark nesting
// rule that motivated fence.go: docs/ipmt-unresolved.md SHOWS the fence
// syntax inside a ````md wrapper — the inner ```ipmt line is literal
// text, not a block. A flat scanner used to detect it (and md-embed
// would then have inserted a marker inside the example).
func TestScanIPMTBlocks_nestedExampleIsLiteral(t *testing.T) {
	src := strings.Join([]string{
		"````md",
		"```ipmt unresolved",
		"deploy ::?etc --::X--> safety ::?etc",
		"```",
		"````",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 0 {
		t.Fatalf("nested example must be literal; got %+v", blocks)
	}
}

func TestScanIPMTBlocks_nestedInTildeFence(t *testing.T) {
	src := strings.Join([]string{
		"~~~md",
		"```ipmt",
		"A --> B",
		"```",
		"~~~",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 0 {
		t.Fatalf("example inside ~~~md must be literal; got %+v", blocks)
	}
}

// ipmt blocks are BACKTICK fences only: ~~~ipmt opens an ordinary
// (non-ipmt) fenced block whose content is literal. A later real
// ```ipmt block is still found.
func TestScanIPMTBlocks_tildeIpmtIsNotABlock(t *testing.T) {
	if IsIPMTFenceStart("~~~ipmt") {
		t.Fatal("IsIPMTFenceStart(~~~ipmt) must be false: ipmt fences are backtick-only")
	}
	src := strings.Join([]string{
		"~~~ipmt",
		"p ::e --> q ::e",
		"~~~",
		"```ipmt",
		"A --> B",
		"```",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want only the backtick block, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].StartLine != 3 {
		t.Fatalf("backtick block starts at line %d, want 3", blocks[0].StartLine)
	}
}

// A real block after a closed example is still found, with correct lines.
func TestScanIPMTBlocks_realBlockAfterExample(t *testing.T) {
	src := strings.Join([]string{
		"````md",  // 0
		"```ipmt", // 1  literal
		"X --> Y", // 2
		"```",     // 3
		"````",    // 4
		"",        // 5
		"```ipmt", // 6  real
		"A --> B", // 7
		"```",     // 8
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %+v", blocks)
	}
	b := blocks[0]
	if b.Index != 1 || b.StartLine != 6 || b.EndLine != 8 || b.Content != "A --> B" {
		t.Fatalf("block = %+v", b)
	}
}

// An unterminated wrapper fence swallows the rest of the document.
func TestScanIPMTBlocks_unterminatedWrapperSwallowsRest(t *testing.T) {
	src := strings.Join([]string{
		"````md",
		"```ipmt",
		"A --> B",
		"```",
		// no ```` closer
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 0 {
		t.Fatalf("everything after unterminated ````md is literal; got %+v", blocks)
	}
}

// A ````ipmt block may CONTAIN ``` lines: the closer must be >= the
// opener's length (CommonMark), so shorter runs and info-carrying lines
// stay content.
func TestScanIPMTBlocks_longFenceKeepsShortRunsAsContent(t *testing.T) {
	src := strings.Join([]string{
		"````ipmt",
		"A --> B",
		"```",     // shorter run: content
		"```text", // info string: content (and could never close anyway)
		"````",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %+v", blocks)
	}
	if want := "A --> B\n```\n```text"; blocks[0].Content != want {
		t.Fatalf("content = %q, want %q", blocks[0].Content, want)
	}
	if blocks[0].EndLine != 4 {
		t.Fatalf("EndLine = %d, want 4", blocks[0].EndLine)
	}
}

// List-indented ipmt fences keep working (deliberate indent leniency).
func TestScanIPMTBlocks_listIndentedFence(t *testing.T) {
	src := strings.Join([]string{
		"- item:",
		"  ```ipmt",
		"  A --> B",
		"  ```",
	}, "\n")
	_, blocks := ScanIPMTBlocks(src)
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %+v", blocks)
	}
}
