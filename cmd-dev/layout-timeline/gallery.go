package main

// One engine, every diagram — no comparison at all.
//
// The rest of the report answers "what changed"; this answers "what does the
// whole corpus look like under THIS engine". Both are needed: a column that
// moved four diagrams says nothing about the three hundred it left alone, and
// those are where a reader notices that something has been wrong for months.
//
// It costs almost nothing, because a diagram that did not move was not
// re-rendered — it IS the previous column's picture. So a gallery is built by
// carrying the previous column forward and overwriting only what moved, and
// the pictures are the pool's existing files.

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
)

// galleryItem is one diagram as one engine drew it.
type galleryItem struct {
	ID    string
	Where string
	Src   string // "" when this engine could not lay it out
	Tier  string // set when the diagram MOVED in this column
	Note  string
	// Anchor identifies the item on the page, so a copied link lands on it.
	Anchor string
	// IssueMD is a ready-to-paste report about THIS diagram under THIS engine.
	// A gallery has no comparison, so there is no regression to describe —
	// only "here is what it draws, and something about it is wrong".
	IssueMD string
	// Moves is how often this diagram has EVER moved, across the whole
	// history. A gallery is a still photograph; this is the one number that
	// says whether the thing being looked at is settled or restless.
	Moves int
	// HistHref opens this diagram's own page, where those moves are.
	HistHref string
	// Canvas is the size this engine gave it, carried forward exactly like
	// the picture: a diagram that did not move did not change size either.
	Canvas string
	// MovedHref points at this diagram's row on the column page, set only
	// when it moved HERE — that row is the before/after this page has not got.
	MovedHref string
}

// galleries is one complete picture set per column, oldest column first.
//
// A diagram enters the set when some engine first draws it, is replaced when
// it moves, and LEAVES it when an engine cannot lay it out — carrying the old
// picture forward there would show a diagram the engine cannot actually draw.
func galleries(in timelineInput) map[string][]galleryItem {
	out := map[string][]galleryItem{}
	// How often each diagram moved across the WHOLE history, counted once.
	moves := map[string]int{}
	for _, w := range in.Weeks {
		for _, c := range w.Changes {
			moves[c.ID]++
		}
	}
	carry := map[string]string{}  // id -> pane href
	canvas := map[string]string{} // id -> its size under the carried engine
	tier := map[string]string{}   // id -> why it changed, this column only

	for _, w := range in.Weeks {
		for id := range tier {
			delete(tier, id)
		}
		// The first column arrives as a whole corpus; later ones as changes.
		for id := range w.Base {
			if ref := in.pane(w.Label, id, "after"); ref != "" {
				carry[id] = ref
			}
		}
		for _, c := range w.Changes {
			nb := c.Report.NewBounds
			if nb.Width > 0 || nb.Height > 0 {
				canvas[c.ID] = fmt.Sprintf("%d×%d", nb.Width, nb.Height)
			}
		}
		for _, c := range w.Changes {
			ref := in.pane(w.Label, c.ID, "after")
			if ref == "" {
				// Broken here, or not drawn. Either way this engine has no
				// picture of it, and the previous engine's is not one.
				delete(carry, c.ID)
				tier[c.ID] = c.Status
				continue
			}
			carry[c.ID] = ref
			tier[c.ID] = c.Status
			if c.Status == "changed" {
				tier[c.ID] = c.Report.Tier.String()
			}
		}
		if !hasPage(w) && len(w.Base) == 0 {
			continue // a column with nothing of its own shows nothing new
		}

		// Everything this engine draws, PLUS anything it failed on here. A
		// diagram it cannot lay out is not simply absent — that reads as "not
		// in the corpus" when the truth is "this engine cannot draw it".
		seen := map[string]bool{}
		ids := make([]string, 0, len(carry)+len(tier))
		for id := range carry {
			seen[id] = true
			ids = append(ids, id)
		}
		for id := range tier {
			if !seen[id] {
				ids = append(ids, id)
			}
		}
		// Source order, the same as the index grid: a catalogue is looked
		// things up in.
		sort.Slice(ids, func(a, b int) bool {
			ra, oka := in.Order[ids[a]]
			rb, okb := in.Order[ids[b]]
			if oka && okb && ra != rb {
				return ra < rb
			}
			if oka != okb {
				return oka
			}
			return ids[a] < ids[b]
		})

		items := make([]galleryItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, galleryItem{
				ID: id, Where: in.Where[id], Src: carry[id], Tier: tier[id],
				Anchor: "g-" + layoutaudit.Sanitize(id),
				Moves:  moves[id], HistHref: "../../" + diagramDir(id) + "/index.html",
				Canvas: canvas[id], MovedHref: movedHere(tier[id], id),
			})
		}
		out[w.Label] = items
	}
	return out
}

// movedHere links an item to its row on the column page, which holds the
// before/after a gallery deliberately does not.
func movedHere(tier, id string) string {
	if tier == "" {
		return ""
	}
	return "index.html#" + anchorOf(id)
}
