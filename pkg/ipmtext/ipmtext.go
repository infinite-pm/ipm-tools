// Package ipmtext serializes a model.IpmGraph back to canonical ipmt text —
// the inverse of ipm-tools' ipmt parser. Used by cmd-dev/ipm-to-ipmt and by
// cmd-dev/corpus-gallery (to emit an editable .ipmt next to each graph).
package ipmtext

import (
	"fmt"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// Serialize renders the graph as ipmt: one alias-tagged node declaration per
// node (`a::a name ::type ["tooltip" ::tip]`), then one line per edge using
// `--::L-->`, `--::P-->`, `--::X-->`, and `--::N--`. Reconstructed from graph
// structure only (ignores src.ipmt / src.positions), so it round-trips through
// the parser to an equivalent graph.
func Serialize(g *model.IpmGraph) (string, error) {
	aliases := make(map[int]string, len(g.Nodes))
	nodes := append([]model.Node(nil), g.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	for _, n := range nodes {
		aliases[n.ID] = generatedAlias(n)
	}

	var lines []string
	for _, n := range nodes {
		lines = append(lines, formatNodeLine(n, aliases[n.ID]))
	}
	if len(g.Edges) > 0 {
		lines = append(lines, "")
	}

	edges := append([]model.Edge(nil), g.Edges...)
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	for _, e := range edges {
		src, ok := aliases[e.Source]
		if !ok {
			return "", fmt.Errorf("edge %d references missing source node %d", e.ID, e.Source)
		}
		tgt, ok := aliases[e.Target]
		if !ok {
			return "", fmt.Errorf("edge %d references missing target node %d", e.ID, e.Target)
		}
		lines = append(lines, formatEdgeLine(e, src, tgt))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func generatedAlias(node model.Node) string {
	prefix := "t"
	switch node.Type {
	case model.Event:
		prefix = "e"
	case model.Concept:
		prefix = "c"
	case model.Unresolved:
		prefix = "u"
	}
	return fmt.Sprintf("%s%d", prefix, node.ID)
}

// candidateLetters renders a node's candidate kinds as the e/t/c letters used in
// a ::?<letters> marker (primary first). Falls back to "etc" if none recorded.
func candidateLetters(cands []model.NodeType) string {
	var b strings.Builder
	for _, c := range cands {
		switch c {
		case model.Event:
			b.WriteByte('e')
		case model.Thing:
			b.WriteByte('t')
		case model.Concept:
			b.WriteByte('c')
		}
	}
	if b.Len() < 2 {
		return "etc"
	}
	return b.String()
}

func formatNodeLine(node model.Node, alias string) string {
	parts := []string{alias + "::a", quoteIfNeeded(node.Name)}
	switch node.Type {
	case model.Event:
		parts = append(parts, "::e")
	case model.Concept:
		parts = append(parts, "::c")
	case model.Unresolved:
		parts = append(parts, "::?"+candidateLetters(node.Candidates))
	default:
		parts = append(parts, "::t")
	}
	if node.Tooltip != "" {
		parts = append(parts, quoteString(node.Tooltip), "::tip")
	}
	return strings.Join(parts, " ")
}

func formatEdgeLine(edge model.Edge, sourceAlias, targetAlias string) string {
	if edge.SstLinkType == model.NearTo || edge.Dir == model.DirUndir {
		if edge.Tooltip != "" {
			return fmt.Sprintf("%s --::N %s-- %s", sourceAlias, quoteString(edge.Tooltip), targetAlias)
		}
		return fmt.Sprintf("%s --::N-- %s", sourceAlias, targetAlias)
	}
	arrowType := "::L"
	switch edge.SstLinkType {
	case model.PartOf:
		arrowType = "::P"
	case model.Expresses:
		arrowType = "::X"
	}
	if edge.Tooltip != "" {
		return fmt.Sprintf("%s --%s %s--> %s", sourceAlias, arrowType, quoteString(edge.Tooltip), targetAlias)
	}
	return fmt.Sprintf("%s --%s--> %s", sourceAlias, arrowType, targetAlias)
}

func quoteIfNeeded(value string) string {
	if value == "" || needsQuoting(value) {
		return quoteString(value)
	}
	return value
}

func needsQuoting(value string) bool {
	if strings.TrimSpace(value) != value {
		return true
	}
	for _, token := range []string{"::", "--", "\"", ",", "\n", "\t"} {
		if strings.Contains(value, token) {
			return true
		}
	}
	// An unquoted "[...]" is a bracket label the parser strips from the name
	// ("e1 [long label text]" -> "e1"); a name that contains one must be
	// quoted to round-trip. SSTorytime's chinese_story_bear had
	// `not until [when used before a time expression]`, which stripped to
	// `not until` and collided with the node of that name.
	if bi := strings.Index(value, "["); bi >= 0 && strings.LastIndex(value, "]") > bi {
		return true
	}
	// A '#' surrounded by whitespace (or at a token boundary) begins a trailing
	// comment when re-parsed, so such a name must be quoted to round-trip. A '#'
	// glued to a non-space character ("Agent #1", "C#") stays literal and is fine.
	for i := 0; i < len(value); i++ {
		if value[i] != '#' {
			continue
		}
		prevWS := i == 0 || value[i-1] == ' ' || value[i-1] == '\t'
		nextWS := i+1 == len(value) || value[i+1] == ' ' || value[i+1] == '\t'
		if prevWS && nextWS {
			return true
		}
	}
	return false
}

func quoteString(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\n", "\\n",
		"\t", "\\t",
	)
	return "\"" + replacer.Replace(value) + "\""
}
