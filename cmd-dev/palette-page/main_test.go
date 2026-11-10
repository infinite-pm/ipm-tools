package main

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
)

// The page is what a colour change is judged by, so it has to show every
// rule in both modes, the sample has to use every class, and the diagram
// has to be there to compare with.
func TestPageShowsEveryRuleBothWays(t *testing.T) {
	page, err := build()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range ipmtokens.Palette {
		for _, v := range []string{r.FG, r.FGLight} {
			if !strings.Contains(page, v+" Sample") {
				t.Errorf("%s: value %s is not in the table", r.Class, v)
			}
		}
		// arrow and string are fallbacks the tokenizer gives no plain sample
		// line; the table still shows them.
		if r.Class != "arrow" && r.Class != "string" && !strings.Contains(page, `class="ipm-`+r.Class+`"`) {
			t.Errorf("%s: the sample has no token of this class", r.Class)
		}
	}
	if !strings.Contains(page, "<svg") {
		t.Error("the page has no diagram")
	}
	// the diagram table is read from the renderer: an event's text is bold
	// there, a thing's is not
	if !strings.Contains(page, "<td>bold</td>") || !strings.Contains(page, "<td>normal</td>") {
		t.Error("the diagram table does not show the text weights")
	}
	if !strings.Contains(page, "color:#ff8000;font-weight:bold;") {
		t.Error("the event sample is not drawn bold in the diagram's orange")
	}
}
