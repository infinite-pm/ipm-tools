package main

// The copy handler, RUN rather than grepped.
//
// Twice now a string assertion has passed against code that could not work:
// once because paneJS already contained "IntersectionObserver", and once
// because the substitution line was present but unreachable behind an early
// return — so the agent buttons copied a bare URL while the test was happy.
// A browser is the only thing that can say what a click actually copies, so
// these tests drive the real copyJS through node with a minimal DOM.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// jsStr renders a Go string as a JS literal. A payload contains backticks
// (its ipmt fence), so a template literal cannot hold it.
func jsStr(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// clickInNode loads the real copyJS against a stub DOM, clicks the button with
// the given id, and returns what reached the clipboard.
func clickInNode(t *testing.T, buttons, elements, buttonID string) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	harness := `
const HREF = "file:///report/d/x/index.html#c-old";
let copied = null;

// --- the smallest DOM copyJS actually touches ------------------------------
function El(props) {
  return Object.assign({
    dataset: {}, textContent: "", innerHTML: "", hidden: false,
    _listeners: {}, classList: { add() {}, remove() {} },
    addEventListener(ev, fn) { this._listeners[ev] = fn; },
    click() { this._listeners.click && this._listeners.click(); },
  }, props);
}
const BUTTONS = ` + buttons + `;
const ELEMENTS = ` + elements + `;
const byId = {};
for (const [id, spec] of Object.entries(ELEMENTS)) { byId[id] = El(spec); }
const buttonList = Object.entries(BUTTONS).map(([id, spec]) => {
  const b = El(spec); b._id = id; byId[id] = b; return b;
});

// Declared as module-scope consts, which SHADOW the real globals. Node 22
// exposes navigator as a getter-only global, so assigning to it throws.
const location = { href: HREF };
const document = {
  getElementById: (id) => byId[id] || null,
  querySelectorAll: () => buttonList,
  createElement: () => El({ style: {} }),
  body: { appendChild() {}, removeChild() {} },
  createRange: () => ({ selectNodeContents() {} }),
  execCommand: () => { throw new Error("execCommand must not be the path here"); },
};
const window = { getSelection: () => ({ removeAllRanges() {}, addRange() {} }) };
const navigator = { clipboard: { writeText: (t) => { copied = t; return Promise.resolve(); } } };
const setTimeout = () => 0;
const clearTimeout = () => {};

` + copyJS + `

byId[` + "`" + buttonID + "`" + `].click();
process.stdout.write(copied === null ? "<NOTHING COPIED>" : copied);
`
	cmd := exec.Command("node", "--input-type=module", "--eval", harness)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	return string(out)
}

// The whole point of the agent buttons: a click yields the PAYLOAD, with the
// page's own URL spliced in — not the URL on its own.
func TestAgentButtonCopiesThePayloadNotJustTheURL(t *testing.T) {
	payload := "## layout-timeline\n\n- page: __URL__\n\n```ipmt\na --> b\n```"
	got := clickInNode(t,
		`{ "agent": { dataset: { copy: "md-1", anchor: "c-2026-07-20" } } }`,
		`{ "md-1": { textContent: `+jsStr(payload)+`, hidden: true } }`,
		"agent")

	if !strings.Contains(got, "## layout-timeline") {
		t.Errorf("the payload text was not copied; got:\n%s", got)
	}
	if !strings.Contains(got, "```ipmt\na --> b\n```") {
		t.Errorf("the ipmt source was not copied; got:\n%s", got)
	}
	if strings.Contains(got, "__URL__") {
		t.Errorf("the URL placeholder was left unsubstituted; got:\n%s", got)
	}
	if !strings.Contains(got, "file:///report/d/x/index.html#c-2026-07-20") {
		t.Errorf("this version's own URL is not in the payload; got:\n%s", got)
	}
	// The failure that prompted this test: copying the link and nothing else.
	if strings.TrimSpace(got) == "file:///report/d/x/index.html#c-2026-07-20" {
		t.Error("copied ONLY the URL — the payload branch is unreachable again")
	}
}

// The plain link button has no data-copy, and must still copy just the link,
// with any fragment already in the address bar replaced rather than appended.
func TestAnchorButtonStillCopiesJustTheLink(t *testing.T) {
	got := clickInNode(t,
		`{ "link": { dataset: { anchor: "c-2026-07-20" } } }`,
		`{}`, "link")
	if got != "file:///report/d/x/index.html#c-2026-07-20" {
		t.Errorf("anchor button copied %q", got)
	}
}

// The source box copies its own text and has no URL to splice.
func TestSourceBoxCopiesItsText(t *testing.T) {
	got := clickInNode(t,
		`{ "src": { dataset: { copy: "ipmt-src" } } }`,
		`{ "ipmt-src": { textContent: `+jsStr("Commit ::e --> Build ::e")+` } }`,
		"src")
	if got != "Commit ::e --> Build ::e" {
		t.Errorf("source box copied %q", got)
	}
}

// A short literal needs no hidden element to live in — and must not have the
// URL placeholder substituted into it, which would corrupt a path.
func TestLiteralTextIsCopiedVerbatim(t *testing.T) {
	got := clickInNode(t,
		`{ "path": { dataset: { text: "docs/x.md:42", anchor: "c-2026-07-20" } } }`,
		`{}`, "path")
	if got != "docs/x.md:42" {
		t.Errorf("copied %q, want the bare source location", got)
	}
}
