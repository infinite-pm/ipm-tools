package mdembed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderAndWriteSVG_skipsMetaOnlyRewrite renders one block three times to
// confirm: a re-render that changes only the generated-by provenance leaves the
// file untouched (wrote=false), while --force-meta (forceMeta=true) rewrites it.
func TestRenderAndWriteSVG_skipsMetaOnlyRewrite(t *testing.T) {
	root := t.TempDir()
	mdAbs := filepath.Join(root, "doc.md")
	svgPath := filepath.Join(root, "_ipm", "doc", "100.ipm.svg")

	br := BlockResult{
		Content:   "Patrick wears black t-shirt ::e\n  --> Patrick wears white t-shirt ::e\n",
		SVGPath:   svgPath,
		SrcHash:   "abc12345",
		NewMarker: Marker{ID: "100", Hash: "abc12345"},
		Outcome:   OutcomeRerender,
	}

	// 1) First write, stamped as if produced by an older tool.
	wrote, err := RenderAndWriteSVG(root, mdAbs, br, "ipm-rpc@v0.0.0-old", false)
	if err != nil {
		t.Fatalf("initial write: %v", err)
	}
	if !wrote {
		t.Fatal("initial write should report wrote=true")
	}
	first, err := os.ReadFile(svgPath)
	if err != nil {
		t.Fatalf("read after initial write: %v", err)
	}
	if !strings.Contains(string(first), "generated-by=ipm-rpc@v0.0.0-old") {
		t.Fatalf("missing initial provenance stamp:\n%s", first)
	}

	// 2) Re-render with a different tool/version and forceMeta=false: the only
	// drift is generated-by, so the write must be skipped and the file untouched.
	wrote, err = RenderAndWriteSVG(root, mdAbs, br, "md-embed@dev", false)
	if err != nil {
		t.Fatalf("meta-only re-render: %v", err)
	}
	if wrote {
		t.Fatal("meta-only re-render should report wrote=false (skipped)")
	}
	again, err := os.ReadFile(svgPath)
	if err != nil {
		t.Fatalf("read after skip: %v", err)
	}
	if string(again) != string(first) {
		t.Fatalf("file changed despite meta-only skip:\nbefore:\n%s\nafter:\n%s", first, again)
	}

	// 3) Same re-render with forceMeta=true: the provenance stamp is rewritten.
	wrote, err = RenderAndWriteSVG(root, mdAbs, br, "md-embed@dev", true)
	if err != nil {
		t.Fatalf("forced re-render: %v", err)
	}
	if !wrote {
		t.Fatal("forced re-render should report wrote=true")
	}
	forced, err := os.ReadFile(svgPath)
	if err != nil {
		t.Fatalf("read after force: %v", err)
	}
	if !strings.Contains(string(forced), "generated-by=md-embed@dev") {
		t.Fatalf("force-meta did not restamp provenance:\n%s", forced)
	}
}
