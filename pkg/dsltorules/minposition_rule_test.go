package dsltorules

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

func TestMinPositionRule(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "a1", Type: "event", X: 40, Y: 120, Width: 120, Height: 60}, // cx 100, cy 150
			{ID: "h1", Type: "event", X: 40, Y: 500, Width: 120, Height: 60}, // a wrapped row
		},
	}

	for _, tc := range []struct {
		rule string
		pass bool
	}{
		{"#h1 has y>=300", true},
		{"#a1 has y>=300", false},
		{"#a1 has x>=40", true},
		{"#a1 has center-x>=100", true},
		{"#a1 has center-x>=101", false},
		{"#h1 has center-y>=530", true},
	} {
		r, err := Parse(1, tc.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.rule, err)
		}
		err = r.Apply(layout)
		if tc.pass && err != nil {
			t.Errorf("%q should pass, got %v", tc.rule, err)
		}
		if !tc.pass && err == nil {
			t.Errorf("%q should fail", tc.rule)
		}
	}
}

func TestMinPositionRuleUnknownProperty(t *testing.T) {
	if _, err := Parse(1, "#a1 has depth>=3"); err == nil || !strings.Contains(err.Error(), "unsupported property") {
		t.Fatalf("unknown >= property must not parse, got %v", err)
	}
}
