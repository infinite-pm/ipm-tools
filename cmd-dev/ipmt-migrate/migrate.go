package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// Fence statuses.
const (
	StatusOK         = "ok"
	StatusUnparsable = "unparsable"
	StatusFail       = "fail"
)

// FenceResult is the outcome for one fence (or for one whole .ipmt file).
type FenceResult struct {
	Line   int // 1-based line of the opening fence (1 for a .ipmt file)
	Nodes  int
	Marked int
	Status string
	Detail string
}

// FileResult is the outcome for one file; Out is the (possibly unchanged) text.
type FileResult struct {
	Out    []byte
	Fences []FenceResult
	Nodes  int
	Marked int
}

// corrupt is a test seam: when set it alters the migrated text before the proof.
var corrupt func(string) string

// migrateDiagram adds ::t to every unmarked thing spec of one diagram and
// proves the result is the same graph, under the current grammar and under
// DefaultNodeType=event. On unparsable input or a failed proof the source is
// returned unchanged.
func migrateDiagram(src string) (out string, f FenceResult) {
	ins := map[int]bool{}
	unplaceable := 0
	hook := func(e parser.SpecEvent) {
		s := e.Spec
		if s.KindMarker[0] >= 0 || s.Type == model.Unresolved || e.AliasRef || e.Node.Type != model.Thing {
			return
		}
		switch {
		case e.NameRaw[0] >= 0:
			ins[e.NameRaw[1]] = true
		case e.AliasMarkRaw[0] >= 0:
			ins[e.AliasMarkRaw[1]] = true
		default:
			unplaceable++
		}
	}
	orig, err := parser.Parse([]byte(src), parser.Options{SpecHook: hook})
	f.Status = StatusOK
	if err != nil {
		f.Status = StatusUnparsable
		f.Detail = err.Error()
		return src, f
	}
	f.Nodes = len(orig.Nodes)
	if unplaceable > 0 {
		f.Status, f.Detail = StatusFail, fmt.Sprintf("%d unmarked thing spec(s) have no place for ::t", unplaceable)
		return src, f
	}
	if len(ins) == 0 {
		return src, f
	}
	at := make([]int, 0, len(ins))
	for k := range ins {
		at = append(at, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(at)))
	out = src
	for _, p := range at {
		ins := " ::t"
		if p < len(out) && !strings.ContainsRune(" \t\n,", rune(out[p])) {
			ins += " "
		}
		out = out[:p] + ins + out[p:]
	}
	if corrupt != nil {
		out = corrupt(out)
	}
	cur, err := parser.Parse([]byte(out), parser.Options{})
	if err != nil {
		f.Status, f.Detail = StatusFail, "migrated text does not parse: "+err.Error()
		return src, f
	}
	if err := sameGraph(orig, cur); err != nil {
		f.Status, f.Detail = StatusFail, "current grammar: "+err.Error()
		return src, f
	}
	ev, err := parser.Parse([]byte(out), parser.Options{DefaultNodeType: model.Event})
	if err != nil {
		f.Status, f.Detail = StatusFail, "migrated text does not parse with event default: "+err.Error()
		return src, f
	}
	if err := sameGraph(orig, ev); err != nil {
		f.Status, f.Detail = StatusFail, "event default: "+err.Error()
		return src, f
	}
	f.Marked = len(at)
	return out, f
}

// sameGraph reports the first difference in node ids, names, aliases, types,
// tooltips, or edges (source, target, type, direction, label).
func sameGraph(a, b *model.IpmGraph) error {
	if len(a.Nodes) != len(b.Nodes) {
		return fmt.Errorf("node count %d != %d", len(a.Nodes), len(b.Nodes))
	}
	for i, x := range a.Nodes {
		y := b.Nodes[i]
		if x.ID != y.ID || x.Name != y.Name || x.Alias != y.Alias || x.Type != y.Type || x.Tooltip != y.Tooltip ||
			fmt.Sprint(x.Candidates) != fmt.Sprint(y.Candidates) {
			return fmt.Errorf("node %d differs: %+v vs %+v", x.ID, x, y)
		}
	}
	if len(a.Edges) != len(b.Edges) {
		return fmt.Errorf("edge count %d != %d", len(a.Edges), len(b.Edges))
	}
	for i, x := range a.Edges {
		y := b.Edges[i]
		if x.Source != y.Source || x.Target != y.Target || x.SstLinkType != y.SstLinkType || x.Dir != y.Dir || x.Tooltip != y.Tooltip {
			return fmt.Errorf("edge %d differs: %+v vs %+v", x.ID, x, y)
		}
	}
	return nil
}

func isIPMT(name string) bool { return strings.HasSuffix(name, ".ipmt") }

// MigrateFile migrates one .ipmt file (whole file) or .md file (every ipmt
// fence). Everything outside the migrated diagrams is kept byte for byte,
// including each line's own line ending.
func MigrateFile(name string, data []byte) FileResult {
	orig := string(data)
	lines := strings.Split(orig, "\n") // original lines, a CR stays at line end
	res := FileResult{Out: data}
	setLines := func(start int, newText string) {
		for k, l := range strings.Split(newText, "\n") {
			i := start + k
			if i >= len(lines) {
				break
			}
			cr := ""
			if strings.HasSuffix(lines[i], "\r") {
				cr = "\r"
			}
			lines[i] = l + cr
		}
	}
	add := func(f FenceResult) {
		res.Fences = append(res.Fences, f)
		res.Nodes += f.Nodes
		res.Marked += f.Marked
	}
	if isIPMT(name) {
		src := markdown.NormalizeLF(orig)
		out, f := migrateDiagram(src)
		f.Line = 1
		add(f)
		if f.Status == StatusOK && f.Marked > 0 {
			setLines(0, out)
		}
	} else {
		sl, blocks := markdown.ScanIPMTBlocks(orig)
		if len(sl) != len(lines) {
			// A lone CR splits lines differently; do not guess.
			if len(blocks) > 0 {
				for _, b := range blocks {
					add(FenceResult{Line: b.StartLine + 1, Status: StatusUnparsable, Detail: "lone CR line ending, not supported"})
				}
			}
			return res
		}
		for _, b := range blocks {
			out, f := migrateDiagram(b.Content)
			f.Line = b.StartLine + 1
			add(f)
			if f.Status == StatusOK && f.Marked > 0 {
				setLines(b.StartLine+1, out)
			}
		}
	}
	if res.Marked > 0 {
		res.Out = []byte(strings.Join(lines, "\n"))
	}
	return res
}

var (
	explicitT  = regexp.MustCompile(`::t\b`)
	proseWords = []string{"default", "optional", "unmarked", "without a marker", "no marker"}
	kindWords  = []string{"thing", "event", "marker"}
)

const teachingNote = "ipmt fence with explicit ::t"

func talksAboutMarker(line string) bool {
	if explicitT.MatchString(line) {
		return true
	}
	low := strings.ToLower(line)
	return containsAny(low, proseWords) && containsAny(low, kindWords)
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// hasExplicitThing reports whether a diagram carries an explicit ::t.
func hasExplicitThing(src string) bool {
	found := false
	_, err := parser.Parse([]byte(src), parser.Options{SpecHook: func(e parser.SpecEvent) {
		if e.Spec.KindMarker[0] >= 0 && e.Spec.Type == model.Thing {
			found = true
		}
	}})
	if err != nil {
		for _, l := range strings.Split(src, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(l), "#") && explicitT.MatchString(l) {
				return true
			}
		}
		return false
	}
	return found
}

// ProseReport is the read-only prose mode: lines outside ipmt fences (and
// comment lines inside fences or .ipmt files) that talk about the marker or the
// default, plus the fences that carry an explicit ::t. Entries are
// "file:line: text".
func ProseReport(name string, data []byte) []string {
	type entry struct {
		line int
		s    string
	}
	var ents []entry
	hit := func(line int, text string) {
		ents = append(ents, entry{line, fmt.Sprintf("%s:%d: %s", name, line, strings.TrimRight(text, "\r"))})
	}
	collect := func() []string {
		sort.SliceStable(ents, func(i, j int) bool { return ents[i].line < ents[j].line })
		out := make([]string, len(ents))
		for i, e := range ents {
			out[i] = e.s
		}
		return out
	}
	if isIPMT(name) {
		src := markdown.NormalizeLF(string(data))
		for i, l := range strings.Split(src, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "#") && talksAboutMarker(l) {
				hit(i+1, l)
			}
		}
		if hasExplicitThing(src) {
			hit(1, teachingNote)
		}
		return collect()
	}
	lines, blocks := markdown.ScanIPMTBlocks(string(data))
	inFence := make([]bool, len(lines))
	for _, b := range blocks {
		end := b.EndLine
		if end < 0 {
			end = len(lines) - 1
		}
		for i := b.StartLine; i <= end; i++ {
			inFence[i] = true
		}
	}
	for i, l := range lines {
		if inFence[i] {
			if strings.HasPrefix(strings.TrimSpace(l), "#") && talksAboutMarker(l) {
				hit(i+1, l)
			}
		} else if talksAboutMarker(l) {
			hit(i+1, l)
		}
	}
	for _, b := range blocks {
		if hasExplicitThing(b.Content) {
			hit(b.StartLine+1, teachingNote)
		}
	}
	return collect()
}
