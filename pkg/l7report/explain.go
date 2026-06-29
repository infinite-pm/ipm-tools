package l7report

// Explain is the NARRATED end-to-end report
// (docs/dev-tools/layout-explain.md): the pipeline ORDER is the table of contents, each
// stage a section that says what the engine decided, which PRINCIPLE
// decided it (a linked anchor), and — where the trace carries it — what
// it considered and declined, with a tip pointing at the terse
// layout-debug view for the same fact. Deterministic (no timestamps), so
// an explain diff doubles as a behaviour diff between engine versions.
//
// It narrates the SAME l7report.Report the terse Text view renders — one
// event stream, two voices.
//
// With ExplainOpts.Color, every node name, kind word and relation arrow
// is wrapped in an `<!--ipmt:as-token:…-->` marker
// (docs/inline-ipmt-colors.md) so md-html and the VS Code preview paint
// it in the ipmt palette; the markers are invisible HTML comments that
// degrade to plain inline code everywhere else. Off by default — the
// plain form stays greppable.

import (
	"fmt"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// The two docs the report links into. These are the repo-relative DISPLAY
// paths; the clickable URLs come from ExplainOpts (relative to the report
// on disk) when the caller resolves them, else fall back to these.
const (
	principlesDoc = "docs/dev/layout-gen/layout-principles.md"
	debugDoc      = "docs/dev/layout-gen/layout-debug.md"
)

// principleAnchor maps a principle id to the current GitHub heading slug
// in layout-principles.md.
var principleAnchor = map[string]string{
	"v7P1": "v7p1--components-separate-along-event-structure",
	"v7P2": "v7p2--the-most-central-component-first-tied-components-around-it",
	"v7P3": "v7p3--the-event-skeleton-leads-to-runs-down-part-of-indents-right-forks-spread-symmetrically",
	"v7P4": "v7p4--aux-attaches-in-groups-on-the-events-row-wholes-outward-parts-above-concepts-down",
	"v7P5": "v7p5--same-kind-ties-draw-order-or-wrap-as-the-outermost-layer",
	"v7P6": "v7p6--the-flow-corridor-the-skeleton-never-yields-space-does",
	"v7P7": "v7p7--shared-nodes-anchor-at-their-deepest-user",
	"v7P8": "v7p8--spacing-gaps-are-minimums-growth-is-symmetric-the-grid-is-exact",
	"v7P9": "v7p9--edge-routing-clean-kind-aware-or-a-stub",
}

func tip(b *strings.Builder, text string) {
	fmt.Fprintf(b, "> TIP  %s\n\n", text)
}

// bandPhrase turns a band side into a natural preposition phrase so the
// sentence reads ("sits above cX", "sits to the left of e1").
func bandPhrase(side string) string {
	switch side {
	case "left":
		return "to the left of"
	case "right":
		return "to the right of"
	case "above":
		return "above"
	case "below":
		return "below"
	default:
		return "beside"
	}
}

// ExplainOpts configure one narrated report.
type ExplainOpts struct {
	Heading    string // section heading (the source doc's heading)
	SourceRef  string // "file.md:123" or "" — the reference line
	Ipmt       string // the original ipmt, embedded as a fenced block
	SVGRelPath string // relative link to a rendered companion, "" = none
	Candidates bool   // include the per-candidate route story in a <details>
	Verbose    bool   // append the full raw trace in a <details>
	Color      bool   // wrap node names / kinds / arrows in as-token markers

	// PrinciplesHref / DebugDocHref are the CLICKABLE urls for the two
	// linked docs, relative to where the report is written (compute with
	// pkg/markdown.RelPath). Empty falls back to the repo-relative display
	// path, which only resolves from the repo root.
	PrinciplesHref string
	DebugDocHref   string
}

// explainer carries the per-render state the section writers share.
type explainer struct {
	r        *Report
	color    bool
	token    map[string]string // trace name → as-token style
	prinHref string            // clickable url for layout-principles.md
	dbgHref  string            // clickable url for layout-debug.md
}

// buildTokenMap maps every node's trace name (the alias if it has one,
// else its title — wrapped and unwrapped forms both, so a line-broken
// label still matches) to its as-token style: e/t/c × title/alias.
// Boundaries and unknown kinds get no entry (they render uncoloured).
func buildTokenMap(g *layout.Graph) map[string]string {
	m := map[string]string{}
	if g == nil {
		return m
	}
	for _, n := range g.Nodes {
		kind := kindLetter(n.Type)
		if kind == "" {
			continue // boundary / unknown → uncoloured
		}
		if n.Alias != "" {
			m[n.Alias] = kind + "-alias"
			continue
		}
		style := kind + "-title"
		if n.Label != "" {
			m[n.Label] = style
		}
		if n.LabelOriginal != "" {
			m[n.LabelOriginal] = style
		}
	}
	return m
}

// kindLetter is the palette prefix for a node type ("" for boundary).
func kindLetter(nodeType string) string {
	switch nodeType {
	case "event":
		return "e"
	case "thing":
		return "t"
	case "concept":
		return "c"
	}
	return ""
}

// asToken wraps text in an inline code span, prefixed with the as-token
// marker when colour is on and a style is given.
func (e *explainer) asToken(style, text string) string {
	span := "`" + strings.ReplaceAll(text, "\n", " ") + "`"
	if e.color && style != "" {
		return "<!--ipmt:as-token:" + style + "-->" + span
	}
	return span
}

// nm renders a node name in its kind colour (title vs alias resolved from
// the token map; the displayed text flattens any hard line break).
func (e *explainer) nm(name string) string { return e.asToken(e.token[name], name) }

// nms renders a list of names via nm, joined by sep.
func (e *explainer) nms(names []string, sep string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = e.nm(n)
	}
	return strings.Join(parts, sep)
}

// kindWord colours a kind noun ("event"/"thing"/"concept") in its palette
// colour; an unknown kind (boundary) renders plain.
func (e *explainer) kindWord(kind string) string {
	if k := kindLetter(kind); k != "" {
		return e.asToken(k+"-title", kind)
	}
	return e.asToken("", kind)
}

// arrow renders a relation as its three-char ipmt arrow, coloured by the
// relation: leads-to/part-of/expresses draw a directed `-->` (orange /
// green / blue), near-to the undirected `---` (grey). Colour off yields
// the bare glyph.
func (e *explainer) arrow(rel string) string {
	glyph, style := "-->", "L"
	switch rel {
	case "partof":
		style = "P"
	case "expresses":
		style = "X"
	case "nearto":
		glyph, style = "---", "N"
	}
	if e.color {
		return "<!--ipmt:as-token:" + style + "-->`" + glyph + "`"
	}
	return glyph
}

// princ renders one or more principle ids as linked citations, e.g.
// "[v7P4](…#…), [v7P5](…#…)", using the clickable principles url.
func (e *explainer) princ(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if a, ok := principleAnchor[id]; ok {
			parts = append(parts, fmt.Sprintf("[%s](%s#%s)", id, e.prinHref, a))
		} else {
			parts = append(parts, id)
		}
	}
	return strings.Join(parts, ", ")
}

// Explain renders the full narrated report for one graph.
func (r *Report) Explain(o ExplainOpts) string {
	e := &explainer{
		r:        r,
		color:    o.Color,
		token:    buildTokenMap(r.Graph),
		prinHref: o.PrinciplesHref,
		dbgHref:  o.DebugDocHref,
	}
	if e.prinHref == "" {
		e.prinHref = principlesDoc
	}
	if e.dbgHref == "" {
		e.dbgHref = debugDoc
	}

	var b strings.Builder
	if o.Heading != "" {
		fmt.Fprintf(&b, "# Why this layout: %s\n\n", o.Heading)
	} else {
		b.WriteString("# Why this layout\n\n")
	}
	refs := []string{fmt.Sprintf("principles [%s](%s)", principlesDoc, e.prinHref)}
	if o.SourceRef != "" {
		refs = append([]string{fmt.Sprintf("source `%s`", o.SourceRef)}, refs...)
	}
	refs = append(refs, "a throwaway dev artifact, safe to delete")
	b.WriteString(strings.Join(refs, " · ") + "\n\n")
	if o.Ipmt != "" {
		fmt.Fprintf(&b, "```ipmt\n%s\n```\n\n", strings.TrimRight(o.Ipmt, "\n"))
	}
	if o.SVGRelPath != "" {
		fmt.Fprintf(&b, "![](%s)\n\n", o.SVGRelPath)
	}

	e.arrived(&b)
	e.membership(&b)
	e.groups(&b)
	e.skeleton(&b)
	e.place(&b)
	e.assemble(&b)
	e.routes(&b, o.Candidates)
	e.checkYourself(&b)
	if o.Verbose {
		fmt.Fprintf(&b, "<details><summary>Full trace (every event — most verbose)</summary>\n\n```\n%s```\n\n</details>\n\n", r.rawTrace(nil))
	}
	return b.String()
}

// §1 — what the normalize/sizing stage received and shaped.
func (e *explainer) arrived(b *strings.Builder) {
	r := e.r
	fmt.Fprintf(b, "## 1 What arrived  (normalize, sizing — %s)\n\n", e.princ("v7P8"))
	if r.Graph == nil {
		b.WriteString("No graph.\n\n")
		return
	}
	var ev, th, co, bo int
	var wrapped []string
	for _, n := range r.Graph.Nodes {
		switch n.Type {
		case "event":
			ev++
		case "thing":
			th++
		case "concept":
			co++
		case "boundary":
			bo++
		}
		if strings.Contains(n.Label, "\n") {
			name := n.Alias
			if name == "" {
				name = n.Label
			}
			wrapped = append(wrapped, name)
		}
	}
	fmt.Fprintf(b, "%d nodes — %d %s, %d %s, %d %s, %d boundary — and %d edges; canvas %d×%d.\n\n",
		len(r.Graph.Nodes), ev, e.kindWord("event"), th, e.kindWord("thing"),
		co, e.kindWord("concept"), bo, len(r.Graph.Edges),
		r.Graph.Meta.Bounds.Width, r.Graph.Meta.Bounds.Height)
	if len(wrapped) > 0 {
		fmt.Fprintf(b, "Labels that wrapped to a taller box: %s.\n\n", e.nms(wrapped, ", "))
	}
	tip(b, "geometry numbers per node: `layout-debug --facts` (or `--table`).")
}

// §2 — membership: components, the anchor election, satellites, demotes.
func (e *explainer) membership(b *strings.Builder) {
	r := e.r
	fmt.Fprintf(b, "## 2 Components and anchors  (membership — %s)\n\n", e.princ("v7P1", "v7P7"))
	comps := r.byKind("membership", "component")
	for _, ev := range comps {
		evs, _ := ev.Data["events"].([]string)
		aux, _ := ev.Data["aux"].([]string)
		fmt.Fprintf(b, "- **component %d** — events [%s]", int(num(ev.Data, "index")), e.nms(evs, " "))
		if len(aux) > 0 {
			fmt.Fprintf(b, ", aux [%s]", e.nms(aux, " "))
		}
		if ct := int(num(ev.Data, "crossTies")); ct > 0 {
			fmt.Fprintf(b, "; %d cross-tie(s) pull it toward centre (%s)", ct, e.princ("v7P2"))
		}
		b.WriteString("\n")
	}
	if len(comps) > 0 {
		b.WriteString("\n")
	}

	elections := r.byKind("membership", "election")
	for _, ev := range elections {
		losers, _ := ev.Data["losers"].([]string)
		reason := "it is the strictly part-most (deepest) user"
		if str(ev.Data, "rule") == "declaration" {
			reason = "a depth tie, so DECLARATION order (the earlier edge) wins"
		}
		// Lead with a plain word: a list item whose first content is an
		// as-token marker (`- <!--…`) renders BROKEN — CommonMark parses it
		// as an HTML block. "node"/"edge" keep the marker off column-0-plus-bullet.
		fmt.Fprintf(b, "- node %s anchors at %s, not [%s] — %s (%s).\n",
			e.nm(str(ev.Data, "node")), e.nm(str(ev.Data, "winner")), e.nms(losers, " "), reason, e.princ("v7P7"))
	}
	if len(elections) > 0 {
		b.WriteString("\n")
	}

	// satellites, unanchored, and demoted ties — the tie-only placements.
	var lines []string
	for _, ev := range r.Events {
		if ev.Stage != "membership" {
			continue
		}
		switch ev.Kind {
		case "satellite":
			lines = append(lines, fmt.Sprintf("node %s (%s) is a SATELLITE of %s — placed by a tie only, joins as the outermost layer (%s)",
				e.nm(str(ev.Data, "node")), e.kindWord(str(ev.Data, "nodeKind")), e.nm(str(ev.Data, "partner")), e.princ("v7P5")))
		case "unanchored":
			lines = append(lines, fmt.Sprintf("node %s (%s) is unanchored — oriented by its own parts/whole",
				e.nm(str(ev.Data, "node")), e.kindWord(str(ev.Data, "nodeKind"))))
		case "demote":
			lines = append(lines, fmt.Sprintf("the %s %s %s (%s) tie DEMOTES — it draws or stubs, it never places (%s)",
				e.nm(str(ev.Data, "from")), e.arrow(str(ev.Data, "rel")), e.nm(str(ev.Data, "to")), str(ev.Data, "rel"), e.princ("v7P1")))
		}
	}
	for _, l := range lines {
		fmt.Fprintf(b, "- %s.\n", l)
	}
	if len(lines) > 0 {
		b.WriteString("\n")
	}
	tip(b, "one node's election at a glance: `layout-debug --why --sel <node>`.")
}

// §3 — groups: each aux node's band side and offset.
func (e *explainer) groups(b *strings.Builder) {
	fmt.Fprintf(b, "## 3 Bands and satellites  (groups — %s)\n\n", e.princ("v7P4", "v7P5"))
	bands := e.r.byKind("groups", "band")
	if len(bands) == 0 {
		b.WriteString("No aux nodes to band — every node sits on the event skeleton.\n\n")
	}
	for _, ev := range bands {
		fmt.Fprintf(b, "- node %s (%s) sits **%s** %s (offset %+d,%+d).\n",
			e.nm(str(ev.Data, "node")), e.kindWord(str(ev.Data, "nodeKind")), bandPhrase(str(ev.Data, "side")),
			e.nm(str(ev.Data, "anchor")), int(num(ev.Data, "dx")), int(num(ev.Data, "dy")))
	}
	if len(bands) > 0 {
		b.WriteString("\n")
	}
	tip(b, "one node's band offsets: `layout-debug --why --sel <node> --verbose` (the `groups/band` events).")
}

// §4 — skeleton: the per-parent rank rows.
func (e *explainer) skeleton(b *strings.Builder) {
	fmt.Fprintf(b, "## 4 The skeleton  (skeleton — %s)\n\n", e.princ("v7P3", "v7P6"))
	subs := e.r.byKind("skeleton", "subrows")
	if len(subs) == 0 {
		b.WriteString("No sub-structures — the events form a single spine.\n\n")
	}
	for _, ev := range subs {
		fmt.Fprintf(b, "- **%s** sub-structure:\n", e.nm(str(ev.Data, "parent")))
		if rows, ok := ev.Data["rows"].([][]string); ok {
			for ri, row := range rows {
				fmt.Fprintf(b, "  - row %d: %s\n", ri, e.nms(row, " | "))
			}
		}
	}
	if len(subs) > 0 {
		b.WriteString("\n")
	}
	tip(b, "forks spread symmetrically about the parent's centre — see `--table` for the resulting columns.")
}

// §5 — place: final coordinates and the movement across stages.
func (e *explainer) place(b *strings.Builder) {
	fmt.Fprintf(b, "## 5 Coordinates  (place — %s)\n\n", e.princ("v7P8"))
	if geo := e.r.geometryTable(e.nm, e.kindWord); geo != "" {
		b.WriteString("Final boxes:\n\n")
		b.WriteString(geo)
		b.WriteString("\n")
	}
	if traj := e.r.trajectoryTable(e.nm); traj != "" {
		b.WriteString("Movement across stages (a node that slid after its first placement):\n\n")
		b.WriteString(traj)
		b.WriteString("\n")
	} else {
		b.WriteString("Movement: no node moved between floors, pull, place and assemble beyond its component's common shift.\n\n")
	}
	tip(b, "who moved when: the floors/pull/place/assemble snapshot columns; geometry numbers: `layout-debug --facts`.")
}

// §6 — assemble: how each tied component flanked the central one.
func (e *explainer) assemble(b *strings.Builder) {
	fmt.Fprintf(b, "## 6 The canvas  (assemble — %s)\n\n", e.princ("v7P2"))
	tiles := e.r.byKind("assemble", "tile")
	if len(tiles) == 0 {
		b.WriteString("A single component — nothing to flank; it owns the canvas.\n\n")
	}
	for _, ev := range tiles {
		fmt.Fprintf(b, "- component %d (%s) placed on the **%s** of its hub — winning flank scored %d crossing(s), aspect-deviation %.2f.\n",
			int(num(ev.Data, "comp")), e.nm(str(ev.Data, "self")), str(ev.Data, "side"),
			int(num(ev.Data, "cross")), num(ev.Data, "aspectDev"))
	}
	if len(tiles) > 0 {
		b.WriteString("\n")
	}
	// The candidate dump is a FENCED code block — as-token markers do not
	// render inside a fence, so names here stay plain.
	cands := e.r.byKind("assemble", "tile-candidate")
	if len(cands) > 0 {
		b.WriteString("<details><summary>Flank candidates scored (side · crossings · aspect-dev)</summary>\n\n```\n")
		for _, ev := range cands {
			fmt.Fprintf(b, "  comp %d %s: side %-6s cross %d aspectDev %.2f (off %+d,%+d)\n",
				int(num(ev.Data, "comp")), str(ev.Data, "self"), str(ev.Data, "side"),
				int(num(ev.Data, "cross")), num(ev.Data, "aspectDev"),
				int(num(ev.Data, "offX")), int(num(ev.Data, "offY")))
		}
		b.WriteString("```\n\n</details>\n\n")
	}
	tip(b, "flank scoring lives in the `assemble/tile-candidate` events (`--verbose`).")
}

// §7 — route: every edge's final port, shape, and visibility.
func (e *explainer) routes(b *strings.Builder, candidates bool) {
	fmt.Fprintf(b, "## 7 Every edge's route  (route — %s)\n\n", e.princ("v7P9"))
	for _, ev := range e.r.Events {
		if ev.Stage != "route" || (ev.Kind != "chosen" && ev.Kind != "stubbed") {
			continue
		}
		rel := str(ev.Data, "rel")
		shape := "straight"
		switch int(num(ev.Data, "bends")) {
		case 0:
		case 1:
			shape = "dogleg"
		default:
			shape = "lane"
		}
		if ev.Kind == "stubbed" {
			fmt.Fprintf(b, "- edge %s %s %s (%s) STUBS — no candidate stayed within the crossing budget; drawn as two badges.\n",
				e.nm(str(ev.Data, "from")), e.arrow(rel), e.nm(str(ev.Data, "to")), rel)
			continue
		}
		fmt.Fprintf(b, "- edge %s %s %s (%s): port %s@%.2f to %s@%.2f, %s.\n",
			e.nm(str(ev.Data, "from")), e.arrow(rel), e.nm(str(ev.Data, "to")), rel,
			str(ev.Data, "srcSide"), num(ev.Data, "srcPos"),
			str(ev.Data, "tgtSide"), num(ev.Data, "tgtPos"), shape)
	}
	b.WriteString("\n")
	if candidates {
		// Fenced block → plain names.
		if cand := e.r.candidateText(nil); cand != "" {
			fmt.Fprintf(b, "<details><summary>Route candidates (budget arithmetic)</summary>\n\n```\n%s```\n\n</details>\n\n", cand)
		} else {
			b.WriteString("Every edge took its straight first choice — no tie pass ran, nothing was scored.\n\n")
		}
	}
	tip(b, "one edge fast: `layout-debug --why --candidates --sel <a>,<b>`.")
}

// §8 — the debug cheat-sheet: where to dig next.
func (e *explainer) checkYourself(b *strings.Builder) {
	b.WriteString("## 8 Check yourself\n\n")
	b.WriteString("The terse companions to this narrative — same run, one fact per line:\n\n")
	b.WriteString("- `layout-debug --facts` — the layout as rule-DSL facts (pin drafting, canon-vs-actual diffs)\n")
	b.WriteString("- `layout-debug --check` — the universal invariants over THIS diagram (overlaps, cuts, crossings, badges)\n")
	b.WriteString("- `layout-debug --why --verbose` — the raw trace, every engine event on one line\n")
	b.WriteString("- `layout-debug --table` / `--edges` — the node and edge geometry tables\n")
	fmt.Fprintf(b, "- the contract: [%s](%s) and [%s](%s)\n\n", principlesDoc, e.prinHref, debugDoc, e.dbgHref)
}
