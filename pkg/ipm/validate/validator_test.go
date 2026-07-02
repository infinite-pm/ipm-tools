package validate

import (
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

func TestSiblingOrdering_FlagsUnorderedSiblings(t *testing.T) {
	// Two sub-events of workflow with no leads-to between them.
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
compile-run-1::a Compile run ::e --::P--> workflow-1
`
	fs := parseOrFatal(t, src)
	hits := onlyCode(fs, "IPMV2.7")
	if len(hits) == 0 {
		t.Fatalf("expected IPMV2.7 finding for unordered siblings, got: %v", fs)
	}
	if hits[0].Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %s", hits[0].Severity)
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
	// Sub-sub-events of edit-run-1 (two of them) need ordering too.
	src := `workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
wrote-file::a Wrote file ::e --::P--> edit-run-1
emitted-done::a Emitted done ::e --::P--> edit-run-1
`
	fs := parseOrFatal(t, src)
	hits := onlyCode(fs, "IPMV2.7")
	if len(hits) == 0 {
		t.Fatalf("expected IPMV2.7 for inner siblings, got: %v", fs)
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
