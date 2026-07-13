// Command solver-example runs the node-kind solver on ipmt and shows or verifies
// the result — a way to demonstrate, for different edge/type combinations, what
// the solver resolves undecided (::?etc) nodes to.
//
//	# print the solved ipmt for one input (the "call the solver" demo):
//	solver-example --in foo.ipmt          # or --in - for stdin
//
//	# verify every `# given` / `# then` ipmt block pair in markdown docs (golden):
//	solver-example --md docs/solver-examples        # a directory of *.md
//	solver-example --md docs/solver-examples/leadsto.md
//
// The solver path is pure ipmt: parse → nodekind.Solve → nodekind.ToGraph →
// ipmtext.Serialize. No N4L / SSTorytime involved. `# then` blocks are compared
// structurally (by node name→kind and edge set), so they may be hand-written in a
// readable form (bare arrows, original aliases) — only the resolved kinds matter.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/ipmtext"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
	"github.com/infinite-pm/ipm-tools/pkg/nodekind"
)

// solveDefaults selects SolveWithDefaults (role-based defaults) over the strict
// constraint-faithful Solve. Set by --defaults; false for the golden test.
var solveDefaults bool

func main() {
	inPath := flag.String("in", "", "single ipmt file (or - for stdin): print the solved ipmt")
	mdPath := flag.String("md", "", "markdown file or directory: verify every `# given`/`# then` ipmt pair")
	flag.BoolVar(&solveDefaults, "defaults", false, "apply role-based defaults (event-by-default, X-target→concept) to fully resolve grey nodes")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: solver-example --in <file.ipmt|-> | --md <file.md|dir>")
		flag.PrintDefaults()
	}
	flag.Parse()

	switch {
	case *inPath != "":
		if err := runSolve(*inPath); err != nil {
			fmt.Fprintln(os.Stderr, "solver-example:", err)
			os.Exit(1)
		}
	case *mdPath != "":
		ok, err := runVerify(*mdPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "solver-example:", err)
			os.Exit(2)
		}
		if !ok {
			os.Exit(1)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

// solveIpmt parses ipmt, runs the solver on it (typed, no n4l), and returns both
// the resolved graph and its serialized ipmt.
func solveIpmt(src []byte, name string, defaults bool) (*model.IpmGraph, string, error) {
	g, err := parser.ParseIPMTBytes(src, name)
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", name, err)
	}
	solver := nodekind.Solve
	if defaults {
		solver = nodekind.SolveWithDefaults
	}
	out := nodekind.ToGraph(solver(g))
	text, err := ipmtext.Serialize(out)
	if err != nil {
		return nil, "", fmt.Errorf("serialize %s: %w", name, err)
	}
	return out, text, nil
}

func runSolve(path string) error {
	var src []byte
	var err error
	name := path
	if path == "-" {
		src, err = io.ReadAll(os.Stdin)
		name = "<stdin>"
	} else {
		src, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}
	_, text, err := solveIpmt(src, name, solveDefaults)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func runVerify(path string) (bool, error) {
	files, err := mdFiles(path)
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		return false, fmt.Errorf("no .md files at %s", path)
	}
	allOK := true
	var pairs, failures int
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return false, err
		}
		examples, err := scanExamples(string(data))
		if err != nil {
			return false, fmt.Errorf("%s: %w", f, err)
		}
		for _, ex := range examples {
			pairs++
			ok, detail := verifyExample(f, ex)
			if !ok {
				allOK = false
				failures++
				fmt.Printf("FAIL %s [%s]\n%s\n", filepath.Base(f), ex.label, detail)
			}
		}
	}
	fmt.Printf("solver-example: %d pair(s) checked, %d failed\n", pairs, failures)
	return allOK, nil
}

// example is one `# given` / `# then` [/ `# then defaults`] block group: the
// unsolved input, the strict (constraint-faithful) expectation, and an optional
// defaults-mode expectation.
type example struct {
	label       string // nearest heading above the given block, for messages
	in, out     string // # given input and # then (strict) expectation
	outDefaults string // # then defaults expectation (SolveWithDefaults); empty if absent
}

// scanExamples groups consecutive `# given` → `# then` → (optional)
// `# then defaults` ipmt blocks in a markdown file. A block's role is its first
// non-blank content line.
func scanExamples(text string) ([]example, error) {
	lines, blocks := markdown.ScanIPMTBlocks(text)
	var out []example
	var pendingIn *markdown.IPMTBlock
	for i := range blocks {
		b := blocks[i]
		if b.EndLine < 0 {
			return nil, fmt.Errorf("unterminated ```ipmt block at line %d", b.StartLine+1)
		}
		switch blockRole(b.Content) {
		case "in":
			if pendingIn != nil {
				return nil, fmt.Errorf("`# given` block at line %d has no matching `# then`", pendingIn.StartLine+1)
			}
			cp := b
			pendingIn = &cp
		case "out":
			if pendingIn == nil {
				return nil, fmt.Errorf("`# then` block at line %d has no preceding `# given`", b.StartLine+1)
			}
			out = append(out, example{
				label: nearestHeading(lines, pendingIn.StartLine),
				in:    pendingIn.Content,
				out:   b.Content,
			})
			pendingIn = nil
		case "defaults":
			if len(out) == 0 || out[len(out)-1].outDefaults != "" {
				return nil, fmt.Errorf("`# then defaults` block at line %d has no preceding `# then`", b.StartLine+1)
			}
			out[len(out)-1].outDefaults = b.Content
		}
	}
	if pendingIn != nil {
		return nil, fmt.Errorf("`# given` block at line %d has no matching `# then`", pendingIn.StartLine+1)
	}
	return out, nil
}

func verifyExample(file string, ex example) (bool, string) {
	// strict: Solve(given) must match the `# then` block.
	got, _, err := solveIpmt([]byte(ex.in), file+" [given]", false)
	if err != nil {
		return false, "  " + err.Error()
	}
	want, err := parser.ParseIPMTBytes([]byte(ex.out), file+" [then]")
	if err != nil {
		return false, "  parse # then: " + err.Error()
	}
	if diff := diffCanon(canonicalize(want), canonicalize(got)); diff != "" {
		return false, "# then (strict):\n" + diff
	}
	// defaults: SolveWithDefaults(given) must match `# then defaults`, when present.
	if ex.outDefaults != "" {
		gotD, _, err := solveIpmt([]byte(ex.in), file+" [given]", true)
		if err != nil {
			return false, "  " + err.Error()
		}
		wantD, err := parser.ParseIPMTBytes([]byte(ex.outDefaults), file+" [then defaults]")
		if err != nil {
			return false, "  parse # then defaults: " + err.Error()
		}
		if diff := diffCanon(canonicalize(wantD), canonicalize(gotD)); diff != "" {
			return false, "# then defaults:\n" + diff
		}
	}
	return true, ""
}

// --- structural comparison (alias- and arrow-style-agnostic) ---

type canon struct {
	nodes map[string]string // node name -> kind string ("Event", "Unresolved[Concept,Event]")
	edges map[string]bool   // canonical edge key
}

func canonicalize(g *model.IpmGraph) canon {
	c := canon{nodes: map[string]string{}, edges: map[string]bool{}}
	nameByID := map[int]string{}
	for _, n := range g.Nodes {
		nameByID[n.ID] = n.Name
		k := string(n.Type)
		if n.Type == model.Unresolved {
			cands := make([]string, len(n.Candidates))
			for i, cand := range n.Candidates {
				cands[i] = string(cand)
			}
			sort.Strings(cands) // order-independent: candidate set, not ranking
			k += "[" + strings.Join(cands, ",") + "]"
		}
		c.nodes[n.Name] = k
	}
	for _, e := range g.Edges {
		s, t := nameByID[e.Source], nameByID[e.Target]
		if e.SstLinkType == model.NearTo || e.Dir == model.DirUndir {
			if s > t { // undirected: order-independent
				s, t = t, s
			}
			c.edges[s+" --"+string(e.SstLinkType)+"-- "+t] = true
			continue
		}
		c.edges[s+" --"+string(e.SstLinkType)+"--> "+t] = true
	}
	return c
}

func diffCanon(want, got canon) string {
	var b strings.Builder
	// Nodes. Sort keys so the diagnostics are emitted in a stable order
	// regardless of Go's randomized map iteration.
	for _, name := range sortedKeys(want.nodes) {
		wk := want.nodes[name]
		if gk, ok := got.nodes[name]; !ok {
			fmt.Fprintf(&b, "  node %q: expected (%s) but solver produced no such node\n", name, wk)
		} else if gk != wk {
			fmt.Fprintf(&b, "  node %q: expected %s, got %s\n", name, wk, gk)
		}
	}
	for _, name := range sortedKeys(got.nodes) {
		if _, ok := want.nodes[name]; !ok {
			fmt.Fprintf(&b, "  node %q: solver produced (%s) but # then has no such node\n", name, got.nodes[name])
		}
	}
	// Edges.
	for _, e := range sortedBoolKeys(want.edges) {
		if !got.edges[e] {
			fmt.Fprintf(&b, "  edge expected but missing: %s\n", e)
		}
	}
	for _, e := range sortedBoolKeys(got.edges) {
		if !want.edges[e] {
			fmt.Fprintf(&b, "  edge produced but not in # then: %s\n", e)
		}
	}
	return b.String()
}

// sortedKeys returns the keys of a string-valued map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedBoolKeys returns the keys of a bool-valued (set) map in sorted order.
func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- helpers ---

// blockRole reads a block's first non-blank line and classifies it: `# given`
// (unsolved input) → "in"; `# then` (strict expectation) → "out"; a `# then` that
// also mentions "defaults" (e.g. `# then defaults` or `# then(defaults=true)`) →
// "defaults" (the SolveWithDefaults expectation). `# in`/`# out` are accepted as
// aliases for the older docs.
func blockRole(content string) string {
	for _, ln := range strings.Split(content, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		lower := strings.ToLower(t)
		switch {
		case t == "# given", t == "# in":
			return "in"
		case strings.HasPrefix(lower, "# then") && strings.Contains(lower, "default"):
			return "defaults"
		case t == "# then", t == "# out":
			return "out"
		}
		return "" // first non-blank line is neither marker
	}
	return ""
}

// nearestHeading returns the closest markdown heading (#, ##, …) above a line.
func nearestHeading(lines []string, startLine int) string {
	for i := startLine - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "#") {
			return strings.TrimLeft(t, "# ")
		}
	}
	return "?"
}

func mdFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	matches, err := filepath.Glob(filepath.Join(path, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}
