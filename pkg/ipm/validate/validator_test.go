package validate

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
)

// parseOrFatal parses ipmt and fatals the test on parser error.
func parseOrFatal(t *testing.T, src string) []Finding {
	t.Helper()
	g, err := parser.ParseIPMTBytes([]byte(src), "<test>")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return Run(g)
}

// containsCode reports whether findings include one with the given code.
func containsCode(fs []Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

// onlyCode filters findings to those matching code.
func onlyCode(fs []Finding, code string) []Finding {
	out := make([]Finding, 0, len(fs))
	for _, f := range fs {
		if f.Code == code {
			out = append(out, f)
		}
	}
	return out
}

// --- IPMV2.7 sibling-ordering ---
//
// The rule reads a PARENT, not a pair: a sequence (every sub-event ordered)
// and a set (no sub-event ordered) are both whole statements, and only the
// mixture of the two is flagged — once, for the parent.

func TestSiblingOrdering_FlagsMixedParent(t *testing.T) {
	// edit --> compile is a sequence; test-run-1 stands outside it, so the
	// parent is half a sequence and the reader cannot tell why.
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
compile-run-1::a Compile run ::e --::P--> workflow-1
test-run-1::a Test run ::e --::P--> workflow-1
edit-run-1 --> compile-run-1
`
	fs := parseOrFatal(t, src)
	hits := onlyCode(fs, "IPMV2.7")
	if len(hits) != 1 {
		t.Fatalf("expected one IPMV2.7 finding for the mixed parent, got %d: %v", len(hits), fs)
	}
	if hits[0].Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %s", hits[0].Severity)
	}
	if !strings.Contains(hits[0].Message, `"test-run-1"`) {
		t.Errorf("message should name the loose sub-event, got %q", hits[0].Message)
	}
	if !strings.Contains(hits[0].Message, `"workflow-1"`) {
		t.Errorf("message should name the parent, got %q", hits[0].Message)
	}
	if strings.Contains(hits[0].Message, `"compile-run-1"`) {
		t.Errorf("message should not name an ordered sub-event, got %q", hits[0].Message)
	}
	if !strings.Contains(hits[0].Suggest, "near-to") {
		t.Errorf("suggest should offer the near-to way out, got %q", hits[0].Suggest)
	}
	// The loose part and its parent, for a programmatic consumer.
	if len(hits[0].NodeIDs) != 2 {
		t.Errorf("expected the loose sub-event and the parent in NodeIDs, got %v", hits[0].NodeIDs)
	}
	if hits[0].Line <= 0 {
		t.Errorf("expected the finding to point at the loose sub-event's line, got %d", hits[0].Line)
	}
}

func TestSiblingOrdering_AllLooseIsASet(t *testing.T) {
	// Three parts of one run, no order between any of them. Part-of without
	// leads-to already says "these are the parts, their order is not stated";
	// the old per-pair rule asked for three near-to edges to hear it.
	src := `run-1::a Run ::e
controller-side::a Controller side ::e --::P--> run-1
apiserver-side::a Apiserver side ::e --::P--> run-1
etcd-side::a Etcd side ::e --::P--> run-1
`
	fs := parseOrFatal(t, src)
	if containsCode(fs, "IPMV2.7") {
		t.Fatalf("an all-parallel parent is a set, not a defect, got: %v", fs)
	}
}

func TestSiblingOrdering_OneFindingPerParentNamesEveryLoosePart(t *testing.T) {
	// Two ordered, three loose: one finding, not six pair warnings.
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
compile-run-1::a Compile run ::e --::P--> workflow-1
lint-run-1::a Lint run ::e --::P--> workflow-1
doc-run-1::a Doc run ::e --::P--> workflow-1
sign-run-1::a Sign run ::e --::P--> workflow-1
edit-run-1 --> compile-run-1
`
	fs := parseOrFatal(t, src)
	hits := onlyCode(fs, "IPMV2.7")
	if len(hits) != 1 {
		t.Fatalf("expected one finding for the parent, got %d: %v", len(hits), hits)
	}
	for _, want := range []string{`"lint-run-1"`, `"doc-run-1"`, `"sign-run-1"`} {
		if !strings.Contains(hits[0].Message, want) {
			t.Errorf("message should name %s, got %q", want, hits[0].Message)
		}
	}
	if len(hits[0].NodeIDs) != 4 {
		t.Errorf("expected three loose sub-events and the parent in NodeIDs, got %v", hits[0].NodeIDs)
	}
}

func TestSiblingOrdering_MixedParentSilencedByOneNearToPerLoosePart(t *testing.T) {
	// One near-to per loose part — not one per pair — declares them parallel.
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
compile-run-1::a Compile run ::e --::P--> workflow-1
lint-run-1::a Lint run ::e --::P--> workflow-1
doc-run-1::a Doc run ::e --::P--> workflow-1
edit-run-1 --> compile-run-1
lint-run-1 --- edit-run-1
doc-run-1 --- edit-run-1
`
	fs := parseOrFatal(t, src)
	if containsCode(fs, "IPMV2.7") {
		t.Fatalf("each loose part declared parallel should silence the parent, got: %v", fs)
	}
}

func TestSiblingOrdering_MixedParentNamesOnlyTheUndeclaredLoosePart(t *testing.T) {
	// lint says it runs beside the sequence; doc says nothing.
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
compile-run-1::a Compile run ::e --::P--> workflow-1
lint-run-1::a Lint run ::e --::P--> workflow-1
doc-run-1::a Doc run ::e --::P--> workflow-1
edit-run-1 --> compile-run-1
lint-run-1 --- edit-run-1
`
	fs := parseOrFatal(t, src)
	hits := onlyCode(fs, "IPMV2.7")
	if len(hits) != 1 {
		t.Fatalf("expected one finding for the undeclared loose part, got %d: %v", len(hits), hits)
	}
	if !strings.Contains(hits[0].Message, `"doc-run-1"`) {
		t.Errorf("message should name the undeclared part, got %q", hits[0].Message)
	}
	if strings.Contains(hits[0].Message, `"lint-run-1"`) {
		t.Errorf("message should not name the part declared parallel, got %q", hits[0].Message)
	}
}

func TestSiblingOrdering_AcceptsDirectLeadsTo(t *testing.T) {
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
compile-run-1::a Compile run ::e --::P--> workflow-1
edit-run-1 --> compile-run-1
`
	fs := parseOrFatal(t, src)
	if containsCode(fs, "IPMV2.7") {
		t.Fatalf("direct leads-to should satisfy the rule, got: %v", fs)
	}
}

func TestSiblingOrdering_AcceptsFanoutPartialOrder(t *testing.T) {
	// edit --> compile, test (parallel branches from shared predecessor)
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
compile-run-1::a Compile run ::e --::P--> workflow-1
test-run-1::a Test run ::e --::P--> workflow-1
edit-run-1 --> compile-run-1
edit-run-1 --> test-run-1
`
	fs := parseOrFatal(t, src)
	if containsCode(fs, "IPMV2.7") {
		t.Fatalf("fan-out partial order should satisfy the rule, got: %v", fs)
	}
}

func TestSiblingOrdering_AcceptsExplicitParallelNearTo(t *testing.T) {
	// The pair form a person writes by hand — accepted before the rule became
	// per-parent, and still accepted.
	src := `workflow-1::a Workflow ::e
compile-run-1::a Compile run ::e --::P--> workflow-1
test-run-1::a Test run ::e --::P--> workflow-1
compile-run-1 --- test-run-1
`
	fs := parseOrFatal(t, src)
	if containsCode(fs, "IPMV2.7") {
		t.Fatalf("explicit near-to should mark siblings as parallel, got: %v", fs)
	}
}

func TestSiblingOrdering_RecursiveOnSubEvents(t *testing.T) {
	// The mix is looked for at every level: edit-run-1's own parts are half a
	// sequence, while workflow-1 above has a single part and nothing to say.
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
wrote-file::a Wrote file ::e --::P--> edit-run-1
emitted-done::a Emitted done ::e --::P--> edit-run-1
flushed-log::a Flushed log ::e --::P--> edit-run-1
wrote-file --> emitted-done
`
	fs := parseOrFatal(t, src)
	hits := onlyCode(fs, "IPMV2.7")
	if len(hits) != 1 {
		t.Fatalf("expected one IPMV2.7 for the inner parent, got %d: %v", len(hits), fs)
	}
	if !strings.Contains(hits[0].Message, `"edit-run-1"`) {
		t.Errorf("finding should name the inner parent, got %q", hits[0].Message)
	}
}

func TestSiblingOrdering_SingleChildNoWarning(t *testing.T) {
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
`
	fs := parseOrFatal(t, src)
	if containsCode(fs, "IPMV2.7") {
		t.Fatalf("single child has no sibling pair, got: %v", fs)
	}
}
