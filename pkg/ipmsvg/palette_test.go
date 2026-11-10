package ipmsvg

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
)

// The renderer's node colours live in this package's code. palette.json lists
// them too, so that every colour of the toolset can be read, reviewed and
// changed in one file. Nothing reads that list at run time: this test is what
// keeps the two equal, both ways.
func TestKindStyleMatchesThePalette(t *testing.T) {
	if len(ipmtokens.PaletteDiagram) == 0 {
		t.Fatal("palette.json has no diagram section")
	}
	seen := map[string]bool{}
	for _, d := range ipmtokens.PaletteDiagram {
		seen[d.Kind] = true
		fill, stroke, text, bold := KindStyle(d.Kind)
		if fill != d.Fill || stroke != d.Stroke || text != d.Text || bold != d.Bold {
			t.Errorf("%s: the renderer draws fill %s stroke %s text %s bold %v, palette.json says fill %s stroke %s text %s bold %v",
				d.Kind, fill, stroke, text, bold, d.Fill, d.Stroke, d.Text, d.Bold)
		}
	}
	for _, kind := range []string{"event", "thing", "concept", "unresolved"} {
		if !seen[kind] {
			t.Errorf("palette.json has no diagram entry for %s", kind)
		}
	}
}
