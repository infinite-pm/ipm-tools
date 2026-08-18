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
	return layout.Node{
		ID:         shellID,
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
		// intruders, each with its shortest way out; then ONE delta per
		// side — the largest needed — applied to every intruder on that side,
		// so a stacked band leaves the shell as the stack it was (evicting
		// each to the edge collapsed a column of things onto one row)
		type out struct {
			n    *node
			side int
			need int
		}
		var outs []out
		for _, n := range g.nodes {
			if n.comp != ci || !n.placed || n.shell || n.boundary || sh.shellMembers[n.idx] {
				continue
			}
			if n.kind == KindEvent {
				continue // an event that is not a member is a spine neighbour: the Y pass owns it
			}
			if n.x >= sh.x+sh.w || n.x+n.w <= sh.x || n.y >= sh.y+sh.h || n.y+n.h <= sh.y {
				continue
			}
			left := n.x + n.w - sh.x
			right := sh.x + sh.w - n.x
			up := n.y + n.h - sh.y
			down := sh.y + sh.h - n.y
			// the shortest way out that does not land inside ANOTHER shell
			// of the component (two composites stacked: evicting up out of
			// the lower one landed in the upper one; sideways is the way)
			// sideways first: above and below the shell is the spine's
			// corridor (the composite's flow to its successor and to E, which
			// never bends), and a thing evicted into it was cut by that flow;
			// left/right by the shorter push, then up/down only when both
			// horizontal exits land inside another shell
			dists := []int{left, right, up, down}
			order := []int{0, 1, 2, 3}
			sort.Slice(order, func(a, b int) bool {
				ha, hb := order[a] <= 1, order[b] <= 1
				if ha != hb {
					return ha
				}
				return dists[order[a]] < dists[order[b]]
			})
			side, best := order[0], dists[order[0]]
			for _, k := range order {
				x, y := n.x, n.y
				switch k {
				case 0:
					x = sh.x - Clearance - n.w
				case 1:
					x = sh.x + sh.w + Clearance
				case 2:
					y = sh.y - Clearance - n.h
				default:
					y = sh.y + sh.h + Clearance
				}
				clear := true
				for _, o := range g.nodes {
					if !o.shell || o.idx == sh.idx || o.comp != ci || !o.placed {
						continue
					}
					if x < o.x+o.w && o.x < x+n.w && y < o.y+o.h && o.y < y+n.h {
						clear = false
						break
					}
				}
				if clear {
					side, best = k, dists[k]
					break
				}
			}
			outs = append(outs, out{n, side, best + Clearance})
		}
		delta := [4]int{}
		for _, o := range outs {
			if o.need > delta[o.side] {
				delta[o.side] = o.need
			}
		}
		// an intruder that is one of an event's BAND (gp.rel: a stack of
		// part-of things beside a spine event, a fan of concepts) takes the
		// whole band with it, on the same side — evicting the few that
		// crossed the shell edge broke the stack and leapfrogged the rest
		if gp != nil {
			byOwner := map[int]int{} // owner event -> side of its intruders
			for _, o := range outs {
				if rp, ok := gp.rel[o.n.idx]; ok {
					byOwner[rp.event] = o.side
				}
			}
			have := map[int]bool{}
			for _, o := range outs {
				have[o.n.idx] = true
			}
			for m, rp := range gp.rel {
				side, ok := byOwner[rp.event]
				if !ok || have[m] {
					continue
				}
				mn := g.nodes[m]
				if mn.comp != ci || !mn.placed || sh.shellMembers[m] {
					continue
				}
				outs = append(outs, out{mn, side, 0})
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
