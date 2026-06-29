package layout7

import "strings"

// v7P8 — spacing: gaps are minimums, growth is symmetric, the grid is exact.
// "Every constant a multiple of the grid step, so the solver works in grid
// units and snapping is exact by construction." Grid step is 20 per the
// spec; every constant below is a multiple of it.
const (
	GridStep = 20

	NodeW        = 120 // standard node 120×60
	NodeH        = 60
	BoundarySize = 40 // S/E boundary nodes

	RowGap  = 60  // vertical gap between event rows (base minimum)
	ColGap  = 60  // horizontal gap between adjacent columns (base minimum)
	NearGap = 100 // near-to satellite stand-off (v7P5): visibly MORE than
	// an attached column gap — near-to is adjacency, not attachment
	// ("more than partof/express gap", ~90 gridded up)
	BoundaryGap = 40  // S/E to the adjacent event (v7P8 proven constant)
	StackGap    = 40  // vertical gap inside an aux stack (clearance)
	Clearance   = 40  // generic aux clearance (v7P8 "clearance 40")
	CompGap     = 120 // gap between components (v7P2 tiles / wrap rows)
	Margin      = 40  // canvas margin

	// ColPitch is the centre-to-centre distance of adjacent standard
	// columns: NodeW + ColGap. Derived, not tunable separately.
	ColPitch = NodeW + ColGap // 180

	// RowPitch is the base centre distance of adjacent event rows.
	RowPitch = NodeH + RowGap // 120

	// MaxFanAngleDeg caps how flat a wide fork fan may get (v7P3/P8): past
	// it the fan row drops instead of only widening.
	MaxFanAngleDeg = 150

	// TargetAspect is the 16:9 canvas budget every arrangement aims for
	// (v7P2); the single global valve of the spec's decision log.
	TargetAspectW = 16.0
	TargetAspectH = 9.0

	// Text sizing (v7P8): widths grow for long labels, heights for
	// wrapped lines.
	charsPerLine  = 12 // at NodeW; scales with width
	linesPerBaseH = 3  // lines that fit the base height
	lineHeight    = 20 // height per extra line
	aspectLimit   = 3  // widen when height reaches 3× width
	maxNodeWidth  = 600
)

// breatheGap: the sibling gap of a k-wide fan (v7P8 "wide forks
// breathe"): the base column gap plus one grid step per member
// beyond two — a wide fan's edges enter at ever shallower angles, so the
// boxes need air. Two siblings 60, three 80, four 100. Applies to ANY
// one-to-many fan (forks and the virtual root fork of rank-0 starts).
func breatheGap(k int) int {
	if k <= 2 {
		return ColGap
	}
	return ColGap + GridStep*(k-2)
}

// computeSize maps a label to a node box (v7P8). Base 120×60 holds ~3 lines
// at ~12 chars/line; each further line adds 20px; when the box would reach
// 3× as tall as wide it widens in 120px steps (240, 360, …, 600 max) and
// re-flows.
func computeSize(text string) (int, int) {
	w := NodeW
	h := heightForWidth(text, w)
	for h >= aspectLimit*w && w < maxNodeWidth {
		w += NodeW
		if w > maxNodeWidth {
			w = maxNodeWidth
		}
		h = heightForWidth(text, w)
	}
	return w, h
}

func heightForWidth(text string, width int) int {
	if text == "" {
		return NodeH
	}
	perLine := charsPerLine * width / NodeW
	lines := lineCount(text, perLine)
	if lines <= linesPerBaseH {
		return NodeH
	}
	return NodeH + (lines-linesPerBaseH)*lineHeight
}

// wrapUnit is one wrap token: glue units join the previous one without a
// space (the tail of a hyphen split).
type wrapUnit struct {
	s    string
	glue bool
}

// wrapUnits splits a label into wrap units: spaces separate words, and a
// word too long for a line splits AFTER its last hyphen that fits (each
// segment keeps its hyphen — kube-controller-manager wraps at the
// hyphens), hard-splitting only when no hyphen helps. KEEP IN SYNC with
// pkg/ipmsvg's copy: the renderer must break exactly where the box was
// sized to break.
func wrapUnits(label string, maxChars int) []wrapUnit {
	var units []wrapUnit
	for _, w := range strings.Fields(label) {
		glue := false
		for w != "" {
			r := []rune(w)
			cut := -1
			for i := 0; i < len(r) && i < maxChars; i++ {
				if r[i] == '-' && i+1 < len(r) {
					cut = i + 1
				}
			}
			if len(r) <= maxChars {
				cut = len(r)
			} else if cut == -1 {
				cut = maxChars
			}
			units = append(units, wrapUnit{string(r[:cut]), glue})
			w = string(r[cut:])
			glue = true
		}
	}
	return units
}

// lineCount estimates greedy wrap line usage over wrapUnits (a glued unit
// costs no separating space).
func lineCount(label string, maxChars int) int {
	if maxChars <= 0 {
		return 1
	}
	units := wrapUnits(label, maxChars)
	if len(units) == 0 {
		return 1
	}
	lines, cur := 1, 0
	for _, u := range units {
		ul := len([]rune(u.s))
		sep := 1
		if u.glue {
			sep = 0
		}
		switch {
		case cur == 0:
			cur = ul
		case cur+sep+ul <= maxChars:
			cur += sep + ul
		default:
			lines++
			cur = ul
		}
	}
	return lines
}
