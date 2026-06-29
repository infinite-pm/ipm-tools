package l7report

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
)

// TestRecordingRoundTrip proves a recorded run replays identically: the
// events survive JSON (the []string/[][]string payloads the renderers
// assert on are restored by normalizeDecoded), so the terminal view — at
// its most verbose, which prints every payload — is byte-for-byte the
// same before and after a record→decode cycle.
func TestRecordingRoundTrip(t *testing.T) {
	const src = `S->e1: started
e1->e2: ran
e2->E: done
e1 #thing "cfg" expresses e1
e2 ~ e1`
	doc, err := parser.Parse([]byte(src), parser.Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rep, err := Run(doc)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := NewRecording(rep, Source{File: "case.ipmt"}).JSON()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	rep2, err := DecodeRecording(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	opts := TextOpts{Candidates: true, Verbose: true}
	if want, got := rep.Text(opts), rep2.Text(opts); want != got {
		t.Errorf("replayed view differs from live view:\n--- live ---\n%s\n--- replay ---\n%s", want, got)
	}
}

func TestDecodeRecordingVersionMismatch(t *testing.T) {
	_, err := DecodeRecording([]byte(`{"version":999,"events":[],"graph":null}`))
	if err == nil {
		t.Fatal("expected a version-mismatch error, got nil")
	}
}
