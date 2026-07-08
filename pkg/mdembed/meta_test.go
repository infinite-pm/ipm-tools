package mdembed

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtmeta"
)

// embed=false: a valid but illustrative block is skipped (no marker/SVG), while
// a sibling plain block still gets a marker.
func TestAnalyzeMarkdown_embedFalseSkipped(t *testing.T) {
	src := strings.Join([]string{
		"```ipmt",
		"A --> B",
		"```",
		"",
		"```ipmt embed=false",
		"X --> Y",
		"```",
	}, "\n")
	got, err := AnalyzeMarkdown("/repo/d.md", src, AnalyzeOptions{Root: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(got.Blocks))
	}
	if got.Blocks[0].Outcome != OutcomeInsertMarker {
		t.Fatalf("block 0 outcome = %s", got.Blocks[0].Outcome)
	}
	if got.Blocks[1].Outcome != OutcomeNoEmbed {
		t.Fatalf("block 1 outcome = %s, want %s", got.Blocks[1].Outcome, OutcomeNoEmbed)
	}
	if got.Blocks[1].NewMarker.ID != "" || got.Blocks[1].SVGPath != "" {
		t.Fatalf("embed=false block should have no marker/SVG: %+v", got.Blocks[1])
	}
}

// A `# ipmt:` pragma in a visible block's content contributes flags, unioned
// with the fence info-string tokens.
func TestAnalyzeMarkdown_pragmaInVisibleBlock(t *testing.T) {
	src := strings.Join([]string{
		"```ipmt",
		"# ipmt: unresolved",
		"deploy ::?etc --::X--> safety ::?etc",
		"```",
	}, "\n")
	got, err := AnalyzeMarkdown("/repo/d.md", src, AnalyzeOptions{Root: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !ipmtmeta.Contains(got.Blocks[0].Meta, ipmtmeta.FlagUnresolved) {
		t.Fatalf("meta = %v, want to contain unresolved", got.Blocks[0].Meta)
	}
}

// An include block carries no fence, so its flags come solely from the `# ipmt:`
// pragma inside the included .ipmt content.
func TestAnalyzeMarkdown_pragmaInInclude(t *testing.T) {
	src := "<!-- ipm-include src=frag.ipmt -->\n"
	opts := AnalyzeOptions{
		Root: "/repo",
		SrcReader: func(string) ([]byte, error) {
			return []byte("# ipmt: unresolved\ndeploy ::?etc --::X--> safety ::?etc"), nil
		},
	}
	got, err := AnalyzeMarkdown("/repo/d.md", src, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Blocks) != 1 || got.Blocks[0].Kind != KindInclude {
		t.Fatalf("blocks = %+v", got.Blocks)
	}
	if !ipmtmeta.Contains(got.Blocks[0].Meta, ipmtmeta.FlagUnresolved) {
		t.Fatalf("include meta = %v, want to contain unresolved", got.Blocks[0].Meta)
	}
}

// A misplaced `# ipmt:` pragma (not the first non-empty line) is bad metadata.
func TestAnalyzeMarkdown_badPragma(t *testing.T) {
	src := strings.Join([]string{
		"```ipmt",
		"# a comment first",
		"# ipmt: unresolved",
		"A --> B",
		"```",
	}, "\n")
	got, err := AnalyzeMarkdown("/repo/d.md", src, AnalyzeOptions{Root: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks[0].Outcome != OutcomeBadMeta {
		t.Fatalf("outcome = %s, want %s", got.Blocks[0].Outcome, OutcomeBadMeta)
	}
	if got.Blocks[0].SkipReason == "" {
		t.Fatal("want a SkipReason describing the bad metadata")
	}
}
