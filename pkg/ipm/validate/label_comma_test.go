package validate

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
)

func runLabelComma(t *testing.T, src string) []Finding {
	t.Helper()
	g, err := parser.ParseIPMTBytes([]byte(src), "<test>")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return AliasLabelCommaCheck{}.Run(g)
}

// A comma inside an aliased label splits it: "it creates" becomes a node and
// "links and closes items" another.
func TestAliasLabelComma_Flags(t *testing.T) {
	fs := runLabelComma(t, "a1 ::t --> x::a it creates, links and closes items ::e\n")
	if len(fs) != 1 {
		t.Fatalf("expected one finding, got %v", fs)
	}
	f := fs[0]
	if f.Code != "IPMV4.6" || f.Severity != SeverityWarning {
		t.Errorf("got %s/%v, want IPMV4.6 warning", f.Code, f.Severity)
	}
	if !strings.Contains(f.Suggest, "quote") {
		t.Errorf("suggest should tell to quote the label: %q", f.Suggest)
	}
	if want := `x::a "it creates, links and closes items" ::e`; !strings.Contains(f.Suggest, want) {
		t.Errorf("suggest = %q, want it to contain %q", f.Suggest, want)
	}
	if f.Line != 1 {
		t.Errorf("line = %d, want 1", f.Line)
	}
}

func TestAliasLabelComma_QuotedOK(t *testing.T) {
	fs := runLabelComma(t, "a1 ::t --> x::a \"it creates, links and closes items\" ::e\n")
	if len(fs) != 0 {
		t.Fatalf("quoted label should not warn, got %v", fs)
	}
}

func TestAliasLabelComma_TargetListOK(t *testing.T) {
	fs := runLabelComma(t, "a1 ::t --> e1 ::e, e2 ::e\na1 --> x::a Event X ::e, y::a Event Y ::e\n")
	if len(fs) != 0 {
		t.Fatalf("plain target list should not warn, got %v", fs)
	}
}
