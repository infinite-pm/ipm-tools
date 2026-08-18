package layout7

import (
	"sort"
	"strconv"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// Shells (Options.Shells): an OPEN composite — a composite event whose part-of
// members are present in the graph — gets a container box around itself and
// every descendant member, ShellPad of air inside. The zoom canvas drew that
// box AFTER layout (ApplyContainerGeometry) and then had to move whatever the
// box now covered; here the shell is a node of the layout from the moment the
// composite's sub-grid is placed: the component's bbox includes it (tiling and
// rings keep their gap from it), the router treats it as an obstacle for every
// edge that does not touch what it wraps, and emit writes it as the
// layout.Node with Container the canvas already understands. Only ROOT
// composites get a shell (a nested composite shares its root's), as the canvas
// does.

// addShellNodes appends one unplaced shell node per root composite with
// members present. Called after buildSkeleton (sp.subParent is the part-of
// hierarchy the engine lays out) and before place, which sizes them.
func (g *graph) addShellNodes(sp *skeletonPlan) {
	if !g.opts.Shells {
		return
	}
	// members per root: walk every sub-event up to its root composite
	membersOf := map[int]map[int]bool{}
	for sub := range sp.subParent {
		root := sub
		for hops := 0; hops < len(g.nodes); hops++ {
			p, ok := sp.subParent[root]
			if !ok {
				break
			}
			root = p
		}
		if root == sub {
			continue
		}
		if membersOf[root] == nil {
			membersOf[root] = map[int]bool{root: true}
		}
		membersOf[root][sub] = true
	}
	roots := make([]int, 0, len(membersOf))
	for r := range membersOf {
		roots = append(roots, r)
	}
	sort.Ints(roots)
	maxID := 0
	for _, n := range g.nodes {
		if n.id > maxID {
			maxID = n.id
		}
	}
	for _, r := range roots {
		c := g.nodes[r]
		maxID++
		g.nodes = append(g.nodes, &node{
			idx:          len(g.nodes),
			id:           maxID,
			name:         "",
			kind:         c.kind,
			comp:         c.comp,
			shell:        true,
			shellOf:      r,
			shellMembers: membersOf[r],
		})
	}
}

// sizeShells sets each shell's rect from its members' placed boxes plus
// ShellPad, and marks it placed. Called at the end of place, per component,
// before the component bbox is taken.
func (g *graph) sizeShells(ci int) {
	for _, sh := range g.nodes {
		if !sh.shell || sh.comp != ci {
			continue
		}
		first := true
		x0, y0, x1, y1 := 0, 0, 0, 0
		for m := range sh.shellMembers {
			n := g.nodes[m]
			if !n.placed {
				continue
			}
			if first {
				x0, y0, x1, y1 = n.x, n.y, n.x+n.w, n.y+n.h
				first = false
				continue
			}
			x0, y0 = minInt(x0, n.x), minInt(y0, n.y)
			x1, y1 = maxInt(x1, n.x+n.w), maxInt(y1, n.y+n.h)
		}
		if first {
			sh.placed = false
			continue
		}
		sh.x, sh.y = x0-ShellPad, y0-ShellPad
		sh.w, sh.h = x1-x0+2*ShellPad, y1-y0+2*ShellPad
		sh.placed = true
	}
}

// shellExempts says whether shell node sh is transparent for edge e: an edge
// touching anything the shell wraps crosses its border legitimately.
func shellExempts(sh *node, e *edge) bool {
	return sh.shellMembers[e.from] || sh.shellMembers[e.to]
}

// emitShell writes a shell as the container node the zoom canvas draws, and
// returns the IDs of the nodes it wraps (so emit can set their ParentNodeIDs).
func (g *graph) emitShell(sh *node) (layout.Node, []string) {
	c := g.nodes[sh.shellOf]
	t, _ := emitType(c)
	ids := make([]string, 0, len(sh.shellMembers))
	for m := range sh.shellMembers {
		ids = append(ids, strconv.Itoa(g.nodes[m].id))
	}
	sort.Strings(ids)
	shellID := "shell-" + strconv.Itoa(c.id)
	// the alias makes the shell addressable where the composite is: a
	// fixture rule says `#shell-eC` (pkg/layouttest names nodes by alias
	// first — the composite's alias, else its name); the canvas ignores it
	alias := "shell-" + c.name
	if c.alias != "" {
		alias = "shell-" + c.alias
	}
	return layout.Node{
		ID:         shellID,
		Alias:      alias,
		Type:       t,
		X:          sh.x,
		Y:          sh.y,
		Width:      sh.w,
		Height:     sh.h,
		RenderKind: t + "-container",
		Container: &layout.Container{
			ChildNodeIDs: ids,
			ShellStyle:   t + "-expanded",
		},
	}, ids
}

// evictAuxFromShells moves a thing/concept of the shell's own component that
// landed INSIDE the shell (a composite's band above or below its box — the
// shell is taller than the box by the sub-grid; a member's satellite hanging
// into the grid) to just outside the nearest shell edge, one Clearance of
// air. A shell wraps events; aux sit around it. Runs after sizeShells and
// before routing, so the routes are drawn to the evicted boxes — the zoom
// canvas did this after routing (evictTCsFromShells) and its routes went
// stale. Members, boundaries and other shells are not aux.
func (g *graph) evictAuxFromShells(ci int, gp *groupsPlan) {
	for pass := 0; pass < 4; pass++ {
		if !g.evictAuxFromShellsOnce(ci, gp) {
			return
		}
	}
}

// evictAuxFromShellsOnce is one sweep over the component's shells; true when
// it moved anything (an eviction out of one shell can land beside another
// whose sweep already ran).
func (g *graph) evictAuxFromShellsOnce(ci int, gp *groupsPlan) bool {
	moved := false
	for _, sh := range g.nodes {
		if !sh.shell || sh.comp != ci || !sh.placed {
			continue
		}
		// intruders, grouped by the BAND they belong to (gp.rel: a stack
		// of part-of things beside a spine event, a fan of concepts; a
		// bandless aux is its own group), each group with ONE way out
		// and ONE delta per side — the largest any of its intruders
		// needs — applied to the whole band, so a stacked band leaves the
		// shell as the stack it was (evicting each to the edge collapsed a
		// column of things onto one row; evicting the few that crossed the
		// shell edge broke the stack and leapfrogged the rest).
		type out struct {
			n    *node
			side int
			need int
		}
		var outs []out
		isAux := func(n *node) bool {
			return n.comp == ci && n.placed && !n.shell && !n.boundary && n.kind != KindEvent
		}
		intrudes := func(n *node) bool {
			return !(n.x >= sh.x+sh.w || n.x+n.w <= sh.x || n.y >= sh.y+sh.h || n.y+n.h <= sh.y)
		}
		groupOf := func(n *node) int {
			if gp != nil {
				if rp, ok := gp.rel[n.idx]; ok {
					return rp.event
				}
			}
			return -(n.idx + 1)
		}
		groups := map[int][]*node{}
		var keys []int
		for _, n := range g.nodes {
			if !isAux(n) || sh.shellMembers[n.idx] || !intrudes(n) {
				continue
			}
			k := groupOf(n)
			if _, ok := groups[k]; !ok {
				keys = append(keys, k)
			}
			groups[k] = append(groups[k], n)
		}
		sort.Ints(keys)
		for _, k := range keys {
			intruders := groups[k]
			// the whole band moves: every band mate of the owner
			members := append([]*node{}, intruders...)
			inGroup := map[int]bool{}
			for _, n := range intruders {
				inGroup[n.idx] = true
			}
			if k >= 0 && gp != nil {
				for m, rp := range gp.rel {
					if rp.event != k || inGroup[m] {
						continue
					}
					mn := g.nodes[m]
					if !isAux(mn) || sh.shellMembers[m] {
						continue
					}
					members = append(members, mn)
					inGroup[m] = true
				}
			}
			// per side, the push the deepest intruder needs
			need := [4]int{}
			for _, n := range intruders {
				d := [4]int{n.x + n.w - sh.x, sh.x + sh.w - n.x, n.y + n.h - sh.y, sh.y + sh.h - n.y}
				for s := 0; s < 4; s++ {
					if d[s]+Clearance > need[s] {
						need[s] = d[s] + Clearance
					}
				}
			}
			// sideways first: above and below the shell is the spine's
			// corridor (the composite's flow to its successor and to E,
			// which never bends), and a thing evicted into it was cut by
			// that flow; left/right by the shorter push, then up/down
			order := []int{0, 1, 2, 3}
			sort.Slice(order, func(a, b int) bool {
				ha, hb := order[a] <= 1, order[b] <= 1
				if ha != hb {
					return ha
				}
				return need[order[a]] < need[order[b]]
			})
			// ... but a band never CHANGES SIDES of its owner while any
			// other exit works: the horizontal exit that would carry a
			// left band to the owner's right is tried only after every
			// other side failed even the shells-only test — in the zoom
			// canvas a click that opens a neighbour must not flip a
			// composite's things from its left to its right (NDA: part
			// 1's band, left in every other state, right of the shell in
			// s:211.213.215 — every pair with those things flipped)
			flipSide := -1
			if k >= 0 {
				own := g.nodes[k]
				ocx := own.x + own.w/2
				sum, cnt := 0, 0
				for _, n := range members {
					sum += n.x + n.w/2
					cnt++
				}
				flip := -1
				if cnt > 0 {
					if sum/cnt < ocx {
						flip = 1 // a left band: the right exit flips it
					} else if sum/cnt > ocx {
						flip = 0
					}
				}
				if flip >= 0 {
					kept := order[:0]
					for _, s := range order {
						if s != flip {
							kept = append(kept, s)
						}
					}
					order = kept
					flipSide = flip
				}
			}
			movedBox := func(n *node, side int) (int, int) {
				d := gridUp(need[side])
				switch side {
				case 0:
					return n.x - d, n.y
				case 1:
					return n.x + d, n.y
				case 2:
					return n.x, n.y - d
				}
				return n.x, n.y + d
			}
			// a side is taken only when the moved band lands clear of
			// every OTHER shell of the component (two composites stacked:
			// evicting up out of the lower one landed in the upper one)
			// — and, preferably, clear of other aux: a horizontal exit
			// that lands on another event's band grows past it and leaves
			// the evicted band BEHIND that band, its part-of lines through
			// it (NDA: part 1's things pushed left past part 0's, 16
			// throughs per state); the gap above the shell held them
			// beside their owner
			clearOf := func(side int, aux bool) bool {
				for _, n := range members {
					x, y := movedBox(n, side)
					for _, o := range g.nodes {
						if o.comp != ci || !o.placed || inGroup[o.idx] {
							continue
						}
						if o.shell {
							if o.idx != sh.idx && x < o.x+o.w && o.x < x+n.w && y < o.y+o.h && o.y < y+n.h {
								return false
							}
							continue
						}
						if aux && isAux(o) &&
							x < o.x+o.w+Clearance && o.x < x+n.w+Clearance && y < o.y+o.h+Clearance && o.y < y+n.h+Clearance {
							return false
						}
					}
				}
				return true
			}
			side := order[0]
			found := false
			tryOrder := func(sides []int) {
				for _, aux := range []bool{true, false} {
					for _, s := range sides {
						if clearOf(s, aux) {
							side, found = s, true
							return
						}
					}
				}
			}
			tryOrder(order)
			if !found && flipSide >= 0 {
				tryOrder([]int{flipSide})
			}
			for _, n := range members {
				nd := 0
				if intrudes(n) {
					nd = need[side]
				}
				outs = append(outs, out{n, side, nd})
			}
		}
		delta := [4]int{}
		for _, o := range outs {
			if o.need > delta[o.side] {
				delta[o.side] = o.need
			}
		}
		evicted := map[int]bool{}
		for _, o := range outs {
			evicted[o.n.idx] = true
		}
		for side := 0; side < 4; side++ {
			if delta[side] == 0 {
				continue
			}
			// grow the push until the group clears every OTHER aux of the
			// component that already sits on that side (the composite's
			// band above the shell top, a member's satellite beyond it) —
			// evicting onto them collapsed two things onto one spot
			d := gridUp(delta[side])
			for guard := 0; guard < 64; guard++ {
				grew := false
				for _, o := range outs {
					if o.side != side {
						continue
					}
					x, y := o.n.x, o.n.y
					switch side {
					case 0:
						x -= d
					case 1:
						x += d
					case 2:
						y -= d
					default:
						y += d
					}
					for _, m := range g.nodes {
						if m.comp != ci || !m.placed || m.shell || m.boundary || m.kind == KindEvent || evicted[m.idx] {
							continue
						}
						if x < m.x+m.w+Clearance && m.x < x+o.n.w+Clearance && y < m.y+m.h+Clearance && m.y < y+o.n.h+Clearance {
							d += GridStep
							grew = true
							break
						}
					}
					if grew {
						break
					}
				}
				if !grew {
					break
				}
			}
			for _, o := range outs {
				if o.side != side {
					continue
				}
				moved = true
				switch side {
				case 0:
					o.n.x -= d
				case 1:
					o.n.x += d
				case 2:
					o.n.y -= d
				default:
					o.n.y += d
				}
			}
		}
	}
	return moved
}
