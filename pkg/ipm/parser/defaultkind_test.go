package parser

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// TestDefaultNodeType: the zero Options value makes a bare node spec an event;
// DefaultNodeType=Thing selects the old rule; explicit markers always win.
func TestDefaultNodeType(t *testing.T) {
	cases := []struct {
		name string
		src  string
		opt  Options
		want model.NodeType
	}{
		{"zero value is event", "book\n", Options{}, model.Event},
		{"explicit thing option", "book\n", Options{DefaultNodeType: model.Thing}, model.Thing},
		{"event option", "book\n", Options{DefaultNodeType: model.Event}, model.Event},
		{"event option, marker ::t wins", "book ::t\n", Options{DefaultNodeType: model.Event}, model.Thing},
		{"event option, marker ::c wins", "book ::c\n", Options{DefaultNodeType: model.Event}, model.Concept},
		{"event option, marker ::e", "book ::e\n", Options{DefaultNodeType: model.Event}, model.Event},
		{"zero value, marker ::t wins", "book ::t\n", Options{}, model.Thing},
		{"thing option, marker ::e wins", "book ::e\n", Options{DefaultNodeType: model.Thing}, model.Event},
		{"event option, undecided stays", "book ::?et\n", Options{DefaultNodeType: model.Event}, model.Unresolved},
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
