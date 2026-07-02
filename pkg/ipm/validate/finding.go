// Package validate runs semantic checks on a parsed IpmGraph, surfacing
// modelling problems the parser does not enforce.
//
// See docs/ipm-validator-rules.md for the catalogue of checks; each check
// is implemented in its own file and exposes a function returning
// []Finding.
package validate

import (
	"fmt"
	"sort"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// Severity classifies a finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Finding is one semantic issue surfaced by a check.
type Finding struct {
	Code     string   `json:"code"`     // stable identifier, e.g. "IPMV2.7"
	Severity Severity `json:"severity"` // error / warning / info
	Message  string   `json:"message"`  // human-readable description
	Suggest  string   `json:"suggest,omitempty"`
	// Location in the source ipmt, derived from the parser's byte positions.
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`   // 1-based
	Column int    `json:"column,omitempty"` // 1-based
	// Node / edge identifiers for programmatic consumers.
	NodeIDs []int `json:"nodeIds,omitempty"`
	EdgeID  int   `json:"edgeId,omitempty"`
}

// String renders the finding in compiler-style: file:line:col: severity: code: message.
func (f Finding) String() string {
	loc := f.File
	if loc == "" {
		loc = "<stdin>"
	}
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", loc, f.Line, f.Column)
	}
	out := fmt.Sprintf("%s: %s: %s: %s", loc, f.Severity, f.Code, f.Message)
	if f.Suggest != "" {
		out += "\n  suggest: " + f.Suggest
	}
	return out
}

// sortFindings orders findings by file, line, column, then code, for stable output.
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		if fs[i].Line != fs[j].Line {
			return fs[i].Line < fs[j].Line
		}
		if fs[i].Column != fs[j].Column {
			return fs[i].Column < fs[j].Column
		}
		return fs[i].Code < fs[j].Code
	})
}

// nodeLoc returns the 1-based line:col of a node's first source occurrence
// (via the parser's byte-range positions) and the file name from g.Src.
// Returns (0, 0) if the node is not present in Positions.
func nodeLoc(g *model.IpmGraph, nodeID int) (line, col int) {
	rng, ok := g.Src.Positions.Nodes[nodeID]
	if !ok {
		return 0, 0
	}
	return offsetToLineCol(g.Src.Ipmt, rng[0])
}

// edgeLoc returns the 1-based line:col of an edge's arrow position in source.
// Falls back to the edge's source node position if the arrow position is missing.
func edgeLoc(g *model.IpmGraph, edge model.Edge) (line, col int) {
	ep, ok := g.Src.Positions.Edges[edge.ID]
	if ok {
		return offsetToLineCol(g.Src.Ipmt, ep.Arrow[0])
	}
	return nodeLoc(g, edge.Source)
}

// offsetToLineCol converts a byte offset in src to a 1-based (line, column).
//
// The column is a byte offset within the line, not a rune count: every
// non-newline byte (including UTF-8 continuation bytes) advances the column by
// one, so a multi-byte rune counts as several columns. For predominantly-ASCII
// ipmt this matches what editors display, but once a non-ASCII character
// precedes the position on a line the reported column will be larger than the
// editor's character column. Consumers needing editor-accurate columns should
// re-derive them from the rune-aware byte offset.
func offsetToLineCol(src string, offset int) (line, col int) {
	if offset < 0 {
		return 0, 0
	}
	if offset > len(src) {
		offset = len(src)
	}
	line = 1
	col = 1
	for i := 0; i < offset; i++ {
		if src[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

// nodeByID returns the node with the given ID, or nil.
func nodeByID(g *model.IpmGraph, id int) *model.Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

// nodeLabel returns a short human-friendly name for a node, preferring alias.
func nodeLabel(n *model.Node) string {
	if n == nil {
		return "<nil>"
	}
	if n.Alias != "" {
		return n.Alias
	}
	return n.Name
}

// idsOf returns the IDs of the given non-nil nodes, in order.
func idsOf(ns ...*model.Node) []int {
	ids := make([]int, 0, len(ns))
	for _, n := range ns {
		if n != nil {
			ids = append(ids, n.ID)
		}
	}
	return ids
}
