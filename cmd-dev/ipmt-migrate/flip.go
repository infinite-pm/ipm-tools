package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// Keep says which places of one file the manifest keeps with explicit markers.
type Keep struct {
	File    bool     // the whole file is kept
	Anchors []string // fences kept: a fence containing one of these lines
}

// FlipFence is the outcome for one fence (or one whole .ipmt file).
type FlipFence struct {
	Line     int
	Marked   int // ::t added
	Unmarked int // ::e removed
	Kept     bool
	Status   string
	Detail   string
}

// FlipResult is the outcome for one file.
type FlipResult struct {
	Out      []byte
	Fences   []FlipFence
	Marked   int
	Unmarked int
	Kept     int
}

type edit struct {
	pos, del int
	ins      string
}

// flipDiagram is step A (::t on unmarked things) and step B (the explicit ::e
// removed from events) for one diagram, then the proof: the original under the
// current default equals the result under the event default. Positions come from
// parser.Options.SpecHook. On unparsable input or a failed proof the source is
// returned unchanged.
func flipDiagram(src string, keepE bool) (out string, f FlipFence) {
	edits := map[int]edit{}
	unplaceable := 0
	hook := func(e parser.SpecEvent) {
		s := e.Spec
		switch {
		case s.KindMarker[0] < 0:
			if s.Type == model.Unresolved || e.AliasRef || e.Node.Type != model.Thing {
				return
			}
			switch {
			case e.NameRaw[0] >= 0:
				edits[e.NameRaw[1]] = edit{pos: e.NameRaw[1], ins: " ::t"}
			case e.AliasMarkRaw[0] >= 0:
				edits[e.AliasMarkRaw[1]] = edit{pos: e.AliasMarkRaw[1], ins: " ::t"}
			default:
				unplaceable++
			}
		case s.Type == model.Event && !keepE:
			at := markerStart(src, e)
			if at < 0 {
				unplaceable++
				return
			}
			start := at
			if start > 0 && src[start-1] == ' ' {
				start--
			}
			edits[start] = edit{pos: start, del: at + 3 - start}
		}
	}
	orig, err := parser.Parse([]byte(src), parser.Options{SpecHook: hook})
	f.Status = StatusOK
	if err != nil {
		f.Status, f.Detail = StatusUnparsable, err.Error()
		return src, f
	}
	if unplaceable > 0 {
		f.Status, f.Detail = StatusFail, fmt.Sprintf("%d spec(s) have no place for ::t or no findable ::e", unplaceable)
		return src, f
	}
	at := make([]int, 0, len(edits))
	for k := range edits {
		at = append(at, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(at)))
	out = src
	for _, p := range at {
		e := edits[p]
		ins := e.ins
		if e.del == 0 && p < len(out) && !strings.ContainsRune(" \t\n,", rune(out[p])) {
			ins += " "
		}
		out = out[:p] + ins + out[p+e.del:]
		if e.del > 0 {
			f.Unmarked++
		} else {
			f.Marked++
		}
	}
	if corrupt != nil {
		out = corrupt(out)
	}
	ev, err := parser.Parse([]byte(out), parser.Options{DefaultNodeType: model.Event})
	if err != nil {
		f.Status, f.Detail = StatusFail, "flipped text does not parse with event default: "+err.Error()
		return src, f
	}
	if err := sameGraph(orig, ev); err != nil {
		f.Status, f.Detail = StatusFail, "old/thing != new/event: "+err.Error()
		return src, f
	}
	return out, f
}

// markerStart returns the raw offset of the explicit ::e of a spec, or -1. The
// parser reports the marker in logical-line offsets only; the gap between the
// name (or the ::a marker) and the marker is the same in both, and the text there
// is checked, so a surprising line shape gives -1 (a FAIL), never a wrong edit.
func markerStart(src string, e parser.SpecEvent) int {
	s := e.Spec
	check := func(at int) int {
		if at >= 0 && at+3 <= len(src) && src[at:at+3] == "::e" {
			return at
		}
		return -1
	}
	if e.NameRaw[0] >= 0 {
		if at := check(e.NameRaw[1] + s.KindMarker[0] - s.NameEnd); at >= 0 {
			return at
		}
	}
	if e.AliasMarkRaw[0] >= 0 {
		return check(e.AliasMarkRaw[1] + s.KindMarker[0] - s.AliasMark[1])
	}
	return -1
}

// FlipFile adds ::t to unmarked things and removes ::e from events, outside kept places.
// A kept fence (or file) keeps its explicit ::e and ::t, but its unmarked things
// still get ::t, because after the flip an unmarked name would turn into an event.
func FlipFile(name string, data []byte, keep Keep) FlipResult {
	orig := string(data)
	lines := strings.Split(orig, "\n")
	res := FlipResult{Out: data}
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
	isKept := func(content string) bool {
		if keep.File {
			return true
		}
		for _, a := range keep.Anchors {
			if hasLine(content, a) {
				return true
			}
		}
		return false
	}
	// one handles a diagram whose first line is file line `first` (1-based).
	one := func(content string, fenceLine, first int) (string, bool) {
		var f FlipFence
		changed := false
		var out string
		kept := isKept(content)
		out, f = flipDiagram(content, kept)
		f.Line, f.Kept = fenceLine, kept
		if kept {
			res.Kept++
		}
		changed = f.Status == StatusOK && (f.Marked > 0 || f.Unmarked > 0)
		res.Fences = append(res.Fences, f)
		res.Marked += f.Marked
		res.Unmarked += f.Unmarked
		return out, changed
	}
	changedAny := false
	if isIPMT(name) {
		out, changed := one(markdown.NormalizeLF(orig), 1, 1)
		if changed {
			setLines(0, out)
			changedAny = true
		}
	} else {
		sl, blocks := markdown.ScanIPMTBlocks(orig)
		if len(sl) != len(lines) {
			for _, b := range blocks {
				res.Fences = append(res.Fences, FlipFence{Line: b.StartLine + 1, Status: StatusUnparsable, Detail: "lone CR line ending, not supported"})
			}
			return res
		}
		for _, b := range blocks {
			out, changed := one(b.Content, b.StartLine+1, b.StartLine+2)
			if changed {
				setLines(b.StartLine+1, out)
				changedAny = true
			}
		}
	}
	if changedAny {
		res.Out = []byte(strings.Join(lines, "\n"))
	}
	return res
}

// runFlip is the -flip mode over (show, path) pairs. manifestPath is read for
// the kept places and for the rule line.
func runFlip(files [][2]string, manifestPath string, write bool, stdout, stderr io.Writer) int {
	if manifestPath == "" {
		fmt.Fprintln(stderr, "ipmt-migrate: -flip needs the manifest: give -manifest FILE (or -root DIR with DIR/ipmt-markers.manifest)")
		return 2
	}
	mdata, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "ipmt-migrate: cannot read the manifest: %v\n", err)
		return 2
	}
	m, err := ParseManifest(mdata)
	if err != nil {
		fmt.Fprintf(stderr, "ipmt-migrate: bad manifest %s: %v\n", manifestPath, err)
		return 2
	}
	if m.Rule == "event" {
		fmt.Fprintln(stdout, "already flipped: the manifest rule is event, nothing changed")
		return 0
	}
	keeps := map[string]*Keep{}
	for _, en := range m.Entries {
		k := keeps[en.Path]
		if k == nil {
			k = &Keep{}
			keeps[en.Path] = k
		}
		switch en.Kind {
		case KindFile:
			k.File = true
		case KindFence:
			k.Anchors = append(k.Anchors, en.Anchor)
		}
	}
	var totFiles, totFences, totMarked, totUnmarked, totKept, changed int
	var unparsable, fails []string
	for _, in := range files {
		data, err := os.ReadFile(in[1])
		if err != nil {
			fmt.Fprintf(stderr, "ipmt-migrate: %v\n", err)
			return 2
		}
		var keep Keep
		if k := keeps[in[0]]; k != nil {
			keep = *k
		}
		r := FlipFile(in[0], data, keep)
		totFiles++
		for _, f := range r.Fences {
			switch f.Status {
			case StatusUnparsable:
				unparsable = append(unparsable, fmt.Sprintf("%s:%d: %s", in[0], f.Line, firstLine(f.Detail)))
			case StatusFail:
				fails = append(fails, fmt.Sprintf("FAIL: %s:%d: %s", in[0], f.Line, firstLine(f.Detail)))
			}
		}
		if len(r.Fences) > 0 {
			fmt.Fprintf(stdout, "%s: fences=%d things-marked=%d events-unmarked=%d\n", in[0], len(r.Fences), r.Marked, r.Unmarked)
		}
		totFences += len(r.Fences)
		totMarked += r.Marked
		totUnmarked += r.Unmarked
		totKept += r.Kept
		if string(r.Out) != string(data) {
			changed++
			if write {
				if err := os.WriteFile(in[1], r.Out, 0o644); err != nil {
					fmt.Fprintf(stderr, "ipmt-migrate: %v\n", err)
					return 2
				}
			}
		}
	}
	mode := "dry-run"
	if write {
		mode = "write"
	}
	fmt.Fprintf(stdout, "TOTAL (%s) files=%d changed=%d fences=%d things-marked=%d events-unmarked=%d kept=%d unparsable=%d fail=%d\n",
		mode, totFiles, changed, totFences, totMarked, totUnmarked, totKept, len(unparsable), len(fails))
	for _, u := range unparsable {
		fmt.Fprintf(stdout, "unparsable, left unchanged: %s\n", u)
	}
	for _, f := range fails {
		fmt.Fprintln(stdout, f)
	}
	if len(fails) > 0 {
		return 1
	}
	if write {
		if err := os.WriteFile(manifestPath, setRuleEvent(mdata), 0o644); err != nil {
			fmt.Fprintf(stderr, "ipmt-migrate: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "manifest rule set to event: %s\n", manifestPath)
	}
	return 0
}

// setRuleEvent replaces the manifest's rule line (the first data line).
func setRuleEvent(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		t := strings.TrimRight(l, "\r")
		if strings.TrimSpace(t) == "" || strings.HasPrefix(t, "#") {
			continue
		}
		lines[i] = "rule\tevent" + l[len(t):]
		break
	}
	return []byte(strings.Join(lines, "\n"))
}
