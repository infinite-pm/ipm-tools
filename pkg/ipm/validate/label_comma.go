package validate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// AliasLabelCommaCheck implements IPMV4.6: in an edge line a comma separates
// targets, so an aliased label written without quotes, such as
// `x::a it creates, links and closes items ::e`, parses as two nodes (the
// first with no kind marker, the second carrying the `::e`). The check looks
// at the source text: an aliased node (`alias::a LABEL`) whose label is
// followed directly by a comma, with the next target not declaring its own
// alias, is almost certainly a label that wanted quotes.
type AliasLabelCommaCheck struct{}

func (AliasLabelCommaCheck) Code() string { return "IPMV4.6" }

func (AliasLabelCommaCheck) Description() string {
	return "comma inside an unquoted aliased label splits it into several nodes (quote the label)"
}

var aliasMarkBefore = regexp.MustCompile(`::a\s+$`)
var kindMarkerTail = regexp.MustCompile(`\s*::[?etc]+\b.*$`)

func (AliasLabelCommaCheck) Run(g *model.IpmGraph) []Finding {
	src := g.Src.Ipmt
	var out []Finding
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Alias == "" {
			continue
		}
		rng, ok := g.Src.Positions.Nodes[n.ID]
		if !ok || rng[0] < 0 || rng[1] > len(src) || rng[0] >= rng[1] {
			continue
		}
		start, end := rng[0], rng[1]
		lineStart := strings.LastIndexByte(src[:start], '\n') + 1
		if !aliasMarkBefore.MatchString(src[lineStart:start]) {
			continue
		}
		if src[start] == '"' || end >= len(src) || src[end] != ',' {
			continue
		}
		lineEnd := strings.IndexByte(src[end:], '\n')
		if lineEnd < 0 {
			lineEnd = len(src) - end
		}
		rest := src[end+1 : end+lineEnd]
		next := rest
		if c := strings.IndexByte(next, ','); c >= 0 {
			next = next[:c]
		}
		if strings.Contains(next, "::a") || strings.TrimSpace(next) == "" {
			continue
		}
		tail := ""
		if m := kindMarkerTail.FindStringIndex(next); m != nil {
			tail = " " + strings.TrimSpace(next[m[0]:])
			next = next[:m[0]]
		}
		line, col := nodeLoc(g, n.ID)
		out = append(out, Finding{
			Code:     "IPMV4.6",
			Severity: SeverityWarning,
			Message: fmt.Sprintf(
				"a comma after %q splits the label of %q into several nodes (only %q is left unmarked)",
				n.Name, n.Alias, n.Name,
			),
			Suggest: fmt.Sprintf(
				"quote the whole label, e.g. %s::a \"%s,%s\"%s",
				n.Alias, n.Name, strings.TrimRight(next, " "), tail,
			),
			Line:    line,
			Column:  col,
			NodeIDs: []int{n.ID},
		})
	}
	return out
}
