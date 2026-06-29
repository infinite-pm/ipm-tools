package layoutcheck

import "testing"

func TestFormatTotals(t *testing.T) {
	counts := map[string][4]int{
		"a": {2, 1, 0}, // dirty
		"b": {0, 0, 3}, // dirty (crossings only)
		"c": {0, 0, 0}, // clean
	}
	// files reflects all considered (3 swept), dirty counts only files with findings.
	got := FormatTotals(3, counts)
	want := "TOTAL: files=3 dirty=2 overlaps=2 edge-through=1 crossings=3 badge=0"
	if got != want {
		t.Errorf("FormatTotals = %q, want %q", got, want)
	}
}

// TestRegressed exercises the ratchet core: a file regresses when a
// worse-severity kind grows, or it ties on all worse kinds and a lesser kind
// grows. Severity order: overlaps[0] > through-node[1] > edge-edge[2] > badge[3].
func TestRegressed(t *testing.T) {
	cases := []struct {
		name string
		was  [4]int
		n    [4]int
		want bool
	}{
		{"identical", [4]int{1, 1, 1, 1}, [4]int{1, 1, 1, 1}, false},
		{"all shrink", [4]int{2, 2, 2, 2}, [4]int{1, 1, 1, 1}, false},
		// Each kind grows alone (others tie) -> regress.
		{"overlaps grow", [4]int{0, 0, 0, 0}, [4]int{1, 0, 0, 0}, true},
		{"through-node grows", [4]int{0, 0, 0, 0}, [4]int{0, 1, 0, 0}, true},
		{"edge-edge grows", [4]int{0, 0, 0, 0}, [4]int{0, 0, 1, 0}, true},
		{"badge grows", [4]int{0, 0, 0, 0}, [4]int{0, 0, 0, 1}, true},
		// A worse kind grows while a lesser kind shrinks -> still a regression.
		{"overlap grows, badge shrinks", [4]int{1, 0, 0, 5}, [4]int{2, 0, 0, 0}, true},
		{"through-node grows, edge-edge shrinks", [4]int{0, 1, 9, 0}, [4]int{0, 2, 0, 0}, true},
		// A lesser kind grows while worse kinds tie -> regression.
		{"badge grows, worse tie", [4]int{3, 2, 1, 0}, [4]int{3, 2, 1, 1}, true},
		{"edge-edge grows, worse tie", [4]int{3, 2, 0, 9}, [4]int{3, 2, 1, 9}, true},
		// A worse kind shrinks while a lesser grows -> NOT a regression (net better).
		{"overlap shrinks though badge grows", [4]int{2, 0, 0, 0}, [4]int{1, 0, 0, 9}, false},
		{"through-node shrinks though edge-edge grows", [4]int{0, 2, 0, 0}, [4]int{0, 1, 9, 0}, false},
	}
	for _, c := range cases {
		if got := Regressed(c.was, c.n); got != c.want {
			t.Errorf("%s: Regressed(%v, %v) = %v, want %v", c.name, c.was, c.n, got, c.want)
		}
	}
}

// TestParseBaselineSpaceInPath verifies a baseline line whose file path
// contains spaces is parsed with the full path preserved (regression for the
// fixed-arity split that dropped such lines or split the path). 3-column
// legacy lines load with badge=0.
func TestParseBaselineSpaceInPath(t *testing.T) {
	content := "# header comment\n" +
		"1 2 3 4 dir/a b/file one.ipmt\n" + // 4-column, spaces in path
		"5 6 7 legacy three col path.ipmt\n" // 3-column legacy, spaces in path
	old, warnings := ParseBaseline([]byte(content))
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if got, ok := old["dir/a b/file one.ipmt"]; !ok || got != [4]int{1, 2, 3, 4} {
		t.Errorf("4-column space path: ok=%v got=%v, want [1 2 3 4]", ok, got)
	}
	if got, ok := old["legacy three col path.ipmt"]; !ok || got != [4]int{5, 6, 7, 0} {
		t.Errorf("3-column legacy space path: ok=%v got=%v, want [5 6 7 0]", ok, got)
	}
}

// TestBaselineRoundTrip proves FormatBaseline → ParseBaseline preserves the
// dirty files' counts and that summing them lands the expected totals.
func TestBaselineRoundTrip(t *testing.T) {
	data := FormatBaseline(map[string][4]int{
		"x": {1, 0, 2},
		"y": {0, 4, 0},
		"z": {0, 0, 0}, // clean — dropped from the baseline
	})
	old, warnings := ParseBaseline(data)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if _, ok := old["z"]; ok {
		t.Errorf("clean file should not appear in the baseline")
	}
	got := FormatTotals(len(old), old)
	want := "TOTAL: files=2 dirty=2 overlaps=1 edge-through=4 crossings=2 badge=0"
	if got != want {
		t.Errorf("round-trip totals = %q, want %q", got, want)
	}
}
