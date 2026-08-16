package layoutaudit

// View helpers shared by every report built on this package: the one-line
// summary of a diff, the selector list for a copy-paste command, the pane
// scaling, and inlining an SVG into HTML. They live here so two reports
// cannot drift into describing the same change two different ways.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

// Summarize is the one line under the heading: what changed, in counts, in
// severity order. It is what makes the ranking explainable — a reader can
// see WHY this row is above that one without opening either.
func Summarize(rep layoutdiff.Report) string {
	if len(rep.Counts) == 0 {
		return ""
	}
	order := []struct {
		kind, one, many string
	}{
		{layoutdiff.KindFindingAdded, "invariant broken", "invariants broken"},
		{layoutdiff.KindNodeAdded, "node added", "nodes added"},
		{layoutdiff.KindNodeRemoved, "node removed", "nodes removed"},
		{layoutdiff.KindEdgeAdded, "edge added", "edges added"},
		{layoutdiff.KindEdgeRemoved, "edge removed", "edges removed"},
		{layoutdiff.KindVisibility, "visibility flip", "visibility flips"},
		{layoutdiff.KindDeferred, "deferred flip", "deferred flips"},
		{layoutdiff.KindPortSide, "port changed side", "ports changed side"},
		{layoutdiff.KindBendCount, "route re-bent", "routes re-bent"},
		{layoutdiff.KindRelabelled, "label changed", "labels changed"},
		{layoutdiff.KindContainerShell, "shell changed", "shells changed"},
		{layoutdiff.KindNodeMoved, "node moved", "nodes moved"},
		{layoutdiff.KindNodeResized, "node resized", "nodes resized"},
		{layoutdiff.KindPortSlide, "port slid", "ports slid"},
		{layoutdiff.KindBendMoved, "bend moved", "bends moved"},
		{layoutdiff.KindBoundsChanged, "canvas resized", "canvas resized"},
		{layoutdiff.KindFindingFixed, "invariant fixed", "invariants fixed"},
	}
	var parts []string
	for _, o := range order {
		n := rep.Counts[o.kind]
		if n == 0 {
			continue
		}
		word := o.one
		if n > 1 {
			word = o.many
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, word))
	}
	if rep.TranslationX != 0 || rep.TranslationY != 0 {
		parts = append(parts, fmt.Sprintf("canvas shift %+d,%+d removed", rep.TranslationX, rep.TranslationY))
	}
	return strings.Join(parts, " · ")
}

// Selectors builds the `--sel` list for the copy-paste commands: the nodes
// this diagram's changes are about, so the pasted command answers THIS row
// rather than dumping the whole decision story.
func Selectors(rep layoutdiff.Report) string {
	seen := map[string]bool{}
	var names []string
	for _, c := range rep.Changes {
		for _, part := range strings.Split(c.Label, "→") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] || strings.ContainsAny(part, ",\"'") {
				continue
			}
			seen[part] = true
			names = append(names, part)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	if len(names) > 6 {
		names = names[:6]
	}
	return " --sel " + strings.Join(names, ",")
}

// PaneWidths puts both panes on ONE pixel scale: the wider canvas fills the
// column, the narrower one takes its true proportion of it.
func PaneWidths(oldW, newW int) (string, string) {
	max := oldW
	if newW > max {
		max = newW
	}
	if max <= 0 {
		return "100%", "100%"
	}
	pct := func(w int) string {
		if w <= 0 {
			return "0%"
		}
		return fmt.Sprintf("%.2f%%", float64(w)/float64(max)*100)
	}
	return pct(oldW), pct(newW)
}

// InlineSVG strips the XML declaration so the markup can be embedded in HTML,
// and drops the fixed width/height so CSS owns the scale (the viewBox keeps
// the aspect ratio).
func InlineSVG(svg []byte) string {
	s := string(svg)
	if i := strings.Index(s, "<svg"); i > 0 {
		s = s[i:]
	}
	if i := strings.Index(s, ">"); i > 0 {
		head := s[:i]
		head = removeAttr(head, "width")
		head = removeAttr(head, "height")
		s = head + s[i:]
	}
	return s
}

func removeAttr(tag, attr string) string {
	for {
		i := strings.Index(tag, " "+attr+"=\"")
		if i < 0 {
			return tag
		}
		j := strings.Index(tag[i+len(attr)+3:], "\"")
		if j < 0 {
			return tag
		}
		tag = tag[:i] + tag[i+len(attr)+3+j+1:]
	}
}
