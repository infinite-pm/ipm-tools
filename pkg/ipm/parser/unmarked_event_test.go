package parser

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// TestUnmarkedNodeIsEvent: with zero Options a bare node spec is an event;
// explicit markers win; Options{DefaultNodeType: model.Thing} selects the old rule.
func TestUnmarkedNodeIsEvent(t *testing.T) {
	cases := []struct {
		name string
		src  string
		opt  Options
		want model.NodeType
	}{
		{"bare node", "book\n", Options{}, model.Event},
		{"marker ::t", "book ::t\n", Options{}, model.Thing},
		{"marker ::c", "book ::c\n", Options{}, model.Concept},
		{"undecided", "book ::?et\n", Options{}, model.Unresolved},
		{"old rule by option", "book\n", Options{DefaultNodeType: model.Thing}, model.Thing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := Parse([]byte(c.src), c.opt)
			if err != nil {
				t.Fatal(err)
			}
			if len(g.Nodes) != 1 || g.Nodes[0].Type != c.want {
				t.Fatalf("got %+v, want one node of type %s", g.Nodes, c.want)
			}
		})
	}
}
