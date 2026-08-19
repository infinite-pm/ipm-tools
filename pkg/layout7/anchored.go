package layout7

import (
	"math"
	"sort"
	"strconv"
)

// Anchored assembly (Options.Anchor): the cross-component arrangement of a
// REFERENCE layout is kept, at component granularity, while every component's
// inside is this layout's own. The zoom canvas passes its all-open layout as
// the anchor; a state then places each component where the anchor had it and
// separates the components that grew (an opened shell) or shrank by pushing
// along the direction the anchor already had them in — right or down in
// reading order — so left stays left and above stays above. Components the
// anchor does not know (nodes new to this state) wrap after the anchored
// group with the count ladder. Nothing is stamped onto nodes: a component is
// moved as a whole, its ports and routes are the engine's for this state.
//
// This is the "soft anchor" of the shells-in-the-core design (the Options
// table in docs/dev/layout-gen/layout7-engine.md) in its robust form: the
// free-choice tiebreaks (ring flank, tile order) were tried first and did
// not hold — the jumps came from the seed and the hub choices, which move
// with centrality on every click.

// assembleAnchored returns false when the anchor covers no component; the
// caller then assembles by centrality as always.
func (g *graph) assembleAnchored() bool {
	if g.opts.Anchor == nil || len(g.comps) == 0 {
		return false
	}
	type acomp struct {
		ci       int
		ax0, ay0 int // anchor bbox of the component's node centres
		ax1, ay1 int
		acx, acy int // anchor centre of mass of the KNOWN nodes
		lcx, lcy int // this layout's centre of mass of the same nodes (local)
		w, h     int // local bbox size
		x, y     int // placed top-left (absolute)
		known    bool
	}
	acs := make([]*acomp, len(g.comps))
	known := 0
	for ci, c := range g.comps {
		a := &acomp{ci: ci, w: c.maxX - c.minX, h: c.maxY - c.minY}
		first := true
		sax, say, slx, sly, cnt := 0, 0, 0, 0, 0
		for _, n := range g.nodes {
			if n.comp != ci || n.shell || n.boundary || !n.placed {
				continue
			}
			p, has := g.opts.Anchor[strconv.Itoa(n.id)]
			if !has {
				continue
			}
			if first {
				a.ax0, a.ay0, a.ax1, a.ay1, first = p[0], p[1], p[0], p[1], false
			}
			a.ax0, a.ay0 = minInt(a.ax0, p[0]), minInt(a.ay0, p[1])
			a.ax1, a.ay1 = maxInt(a.ax1, p[0]), maxInt(a.ay1, p[1])
			sax, say = sax+p[0], say+p[1]
			slx, sly = slx+n.x+n.w/2, sly+n.y+n.h/2
			cnt++
		}
		if cnt > 0 {
			a.known = true
			a.acx, a.acy = sax/cnt, say/cnt
			a.lcx, a.lcy = slx/cnt, sly/cnt
			// ... the centre of mass over the component's EVENTS when it has
			// any (the spine is what a click never adds to; a thing or
			// concept expanded into the component would otherwise pull the
			// whole component toward itself), and, for a component of
			// things and concepts alone, its HUB — the known node with the
			// most edges (declaration order breaks ties) — lands exactly
			// where the anchor had it and everything a click adds grows
			// around it (FriendsAndFiends: Mike's stack slid 560px left
			// when Adam and his friends joined; the centre of mass moved
			// with them).
			esx, esy, elx, ely, ecnt := 0, 0, 0, 0, 0
			hub, hubDeg := -1, -1
			for _, n := range g.nodes {
				if n.comp != ci || n.shell || n.boundary || !n.placed {
					continue
				}
				p, has := g.opts.Anchor[strconv.Itoa(n.id)]
				if !has {
					continue
				}
				if n.kind == KindEvent {
					esx, esy = esx+p[0], esy+p[1]
					elx, ely = elx+n.x+n.w/2, ely+n.y+n.h/2
					ecnt++
				}
				if deg := len(g.in[n.idx]) + len(g.out[n.idx]); deg > hubDeg {
					hub, hubDeg = n.idx, deg
				}
			}
			if ecnt > 0 {
				a.acx, a.acy = esx/ecnt, esy/ecnt
				a.lcx, a.lcy = elx/ecnt, ely/ecnt
			} else if hub >= 0 {
				hn := g.nodes[hub]
				p := g.opts.Anchor[strconv.Itoa(hn.id)]
				a.acx, a.acy = p[0], p[1]
				a.lcx, a.lcy = hn.x+hn.w/2, hn.y+hn.h/2
			}
			known++
		}
		acs[ci] = a
	}
	if known == 0 {
		return false
	}
	// anchored components: the KNOWN nodes' centre of mass lands where the
	// anchor had it (not the bbox centre: a component that grew — a T/C
	// expanded into it — would otherwise carry every old node along with
	// the growth), grid-snapped
	var placed []*acomp
	for _, a := range acs {
		if !a.known {
			continue
		}
		c := g.comps[a.ci]
		a.x = c.minX + gridSnap(a.acx-a.lcx)
		a.y = c.minY + gridSnap(a.acy-a.lcy)
		placed = append(placed, a)
	}
	// reading order of the anchor: rows by anchor top, then left to right
	sort.SliceStable(placed, func(i, j int) bool {
		if absInt(placed[i].ay0-placed[j].ay0) > 2*RowGap {
			return placed[i].ay0 < placed[j].ay0
		}
		return placed[i].ax0 < placed[j].ax0
	})
	// make room: a later component that overlaps an earlier one (with the
	// component gap) is pushed the way the anchor already had it — right
	// when the anchor had it right of the other, down when below, else the
	// shorter push. Fixpoint over pairs; bounded.
	overlaps := func(a, b *acomp) (int, int) {
		dx := a.x + a.w + CompGap - b.x // push right needed
		dy := a.y + a.h + CompGap - b.y // push down needed
		if a.x >= b.x+b.w+CompGap || b.x >= a.x+a.w+CompGap ||
			a.y >= b.y+b.h+CompGap || b.y >= a.y+a.h+CompGap {
			return 0, 0
		}
		return dx, dy
	}
	for iter := 0; iter < 8*len(placed)+8; iter++ {
		moved := false
		for i := 0; i < len(placed); i++ {
			for j := i + 1; j < len(placed); j++ {
				a, b := placed[i], placed[j]
				dx, dy := overlaps(a, b)
				if dx <= 0 && dy <= 0 {
					continue
				}
				// which way did the anchor have b relative to a?
				right := b.acx-a.acx >= absInt(b.acy-a.acy)
				below := b.acy-a.acy > absInt(b.acx-a.acx)
				switch {
				case right && dx > 0:
					b.x += gridUp(dx)
				case below && dy > 0:
					b.y += gridUp(dy)
				case dx > 0 && (dy <= 0 || dx <= dy):
					b.x += gridUp(dx)
				default:
					b.y += gridUp(dy)
				}
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	offsets := make(map[int][2]int, len(g.comps))
	gx0, gy0, gx1, gy1 := math.MaxInt32, math.MaxInt32, math.MinInt32, math.MinInt32
	for _, a := range placed {
		c := g.comps[a.ci]
		offsets[a.ci] = [2]int{a.x - c.minX, a.y - c.minY}
		gx0, gy0 = minInt(gx0, a.x), minInt(gy0, a.y)
		gx1, gy1 = maxInt(gx1, a.x+a.w), maxInt(gy1, a.y+a.h)
	}
	// unknown components: the count ladder to the right of the anchored
	// group, in centrality (declaration) order
	var rest []*acomp
	for _, a := range acs {
		if !a.known {
			rest = append(rest, a)
		}
	}
	if len(rest) > 0 {
		lc, lr := 3, 1
		for lc*lr < len(rest) {
			if lc-lr >= 2 {
				lr++
			} else {
				lc++
			}
		}
		perRow := (len(rest) + lr - 1) / lr
		x, y, rowH := gx1+CompGap, gy0, 0
		for i, a := range rest {
			if i > 0 && i%perRow == 0 {
				x, y = gx1+CompGap, y+rowH+CompGap
				rowH = 0
			}
			c := g.comps[a.ci]
			offsets[a.ci] = [2]int{x - c.minX, y - c.minY}
			x += a.w + CompGap
			if a.h > rowH {
				rowH = a.h
			}
		}
	}
	for _, n := range g.nodes {
		if n.comp >= 0 && n.placed {
			off := offsets[n.comp]
			n.x += off[0]
			n.y += off[1]
		}
	}
	g.normalizeToMargins()
	return true
}

func gridSnap(v int) int {
	if v >= 0 {
		return (v + GridStep/2) / GridStep * GridStep
	}
	return -((-v + GridStep/2) / GridStep * GridStep)
}

// normalizeToMargins translates every placed node so the layout starts at
// (Margin, Margin) — assemble's tail, shared.
func (g *graph) normalizeToMargins() {
	minX, minY := math.MaxInt32, math.MaxInt32
	for _, n := range g.nodes {
		if !n.placed {
			continue
		}
		if n.x < minX {
			minX = n.x
		}
		if n.y < minY {
			minY = n.y
		}
	}
	if minX == math.MaxInt32 {
		return
	}
	for _, n := range g.nodes {
		if n.placed {
			n.x += Margin - minX
			n.y += Margin - minY
		}
	}
}

// anchorOrder returns ids sorted as the soft anchor had them — top to bottom,
// then left to right — when every id has an anchor position; nil otherwise
// (no anchor, or a node the anchor does not know). The free orders inside a
// component (a fan, a stack, a fork's branches) follow it so a state keeps
// the arrangement of the all-open layout; declaration order breaks ties.
func (g *graph) anchorOrder(ids []int) []int {
	if g.opts.Anchor == nil || len(ids) < 2 {
		return nil
	}
	pos := make(map[int][2]int, len(ids))
	for _, id := range ids {
		p, has := g.opts.Anchor[strconv.Itoa(g.nodes[id].id)]
		if !has {
			return nil
		}
		pos[id] = p
	}
	out := append([]int(nil), ids...)
	sort.SliceStable(out, func(a, b int) bool {
		pa, pb := pos[out[a]], pos[out[b]]
		if pa[1] != pb[1] {
			return pa[1] < pb[1]
		}
		return pa[0] < pb[0]
	})
	return out
}

// anchorOrderX is anchorOrder for a ROW: left to right, then top to bottom
// (a fork's branches share a row).
func (g *graph) anchorOrderX(ids []int) []int {
	if g.opts.Anchor == nil || len(ids) < 2 {
		return nil
	}
	pos := make(map[int][2]int, len(ids))
	for _, id := range ids {
		p, has := g.opts.Anchor[strconv.Itoa(g.nodes[id].id)]
		if !has {
			return nil
		}
		pos[id] = p
	}
	out := append([]int(nil), ids...)
	sort.SliceStable(out, func(a, b int) bool {
		pa, pb := pos[out[a]], pos[out[b]]
		if pa[0] != pb[0] {
			return pa[0] < pb[0]
		}
		return pa[1] < pb[1]
	})
	return out
}
