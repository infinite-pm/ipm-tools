package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo makes a temp git repo with the given base files under tests/ and
// returns its dir and the base sha.
func repo(t *testing.T, base map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	write(t, dir, base)
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	return dir, git("rev-parse", "HEAD")
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runTool(dir, sha string) (int, string) {
	var out, errb bytes.Buffer
	code := run([]string{"-root", dir, "-base", sha}, &out, &errb)
	return code, out.String() + errb.String()
}

const golden1 = `{"src":{"type":"ipmt","ipmt":"a --> b","positions":{"nodes":{"1":[0,1]}}},"nodes":[{"id":1,"type":"Thing"}],"edges":[]}`
const golden2 = `{"src":{"type":"ipmt","ipmt":"a::t --> b","positions":{"nodes":{"1":[0,3]}}},"nodes":[{"id":1,"type":"Thing"}],"edges":[]}`

func TestGoldenIdentityEqualGraphPasses(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": golden1, "tests/x/a.ipmt": "a --> b"})
	write(t, dir, map[string]string{"tests/x/a.json": golden2, "tests/x/a.ipmt": "a::t --> b"})
	code, out := runTool(dir, sha)
	if code != 0 || !strings.Contains(out, "golden-identity ok") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityChangedNodeTypeFails(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": golden1})
	write(t, dir, map[string]string{"tests/x/a.json": strings.Replace(golden2, `"Thing"`, `"Event"`, 1)})
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/a.json") || !strings.Contains(out, "golden-identity FAIL 1") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityLayoutCoordinateFails(t *testing.T) {
	l1 := `{"version":"v","nodes":[{"id":"1","x":10}]}`
	l2 := `{"version":"v","nodes":[{"id":"1","x":11}]}`
	dir, sha := repo(t, map[string]string{"tests/x/a.layout.json": l1})
	write(t, dir, map[string]string{"tests/x/a.layout.json": l2})
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/a.layout.json") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityHashFieldsIgnoredInLayout(t *testing.T) {
	l1 := `{"nodes":[{"id":"1","x":10}],"outputs":{"a":"1"}}`
	l2 := `{"nodes":[{"id":"1","x":10}],"outputs":{"a":"2"}}`
	dir, sha := repo(t, map[string]string{"tests/x/a.layout.json": l1})
	write(t, dir, map[string]string{"tests/x/a.layout.json": l2})
	if code, out := runTool(dir, sha); code != 0 {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentitySvgByteChangeFails(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.ipm.svg": "<svg/>"})
	write(t, dir, map[string]string{"tests/x/a.ipm.svg": "<svg />"})
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/a.ipm.svg") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentitySourceAndRefsNotCompared(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/_refs.json": "{}", "tests/x/a.md": "a", "tests/x/a.ipmt": "a"})
	write(t, dir, map[string]string{"tests/x/_refs.json": `{"k":1}`, "tests/x/a.md": "b", "tests/x/a.ipmt": "b"})
	code, out := runTool(dir, sha)
	if code != 0 || !strings.Contains(out, "not compared") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityOtherFileFails(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.txt": "a"})
	write(t, dir, map[string]string{"tests/x/a.txt": "b"})
	if code, _ := runTool(dir, sha); code != 1 {
		t.Fatalf("code %d", code)
	}
}

// A deleted golden whose content exists in no current file of its directory
// is content lost, not a merge into a twin.
func TestGoldenIdentityDeletedGoldenFails(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": golden1})
	if err := os.Remove(filepath.Join(dir, "tests/x/a.json")); err != nil {
		t.Fatal(err)
	}
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/a.json: content lost") {
		t.Fatalf("code %d out %s", code, out)
	}
}

// An untracked golden whose content is in no base file of its directory.
func TestGoldenIdentityUntrackedFileFails(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": golden1})
	write(t, dir, map[string]string{"tests/x/new.ipm.svg": "<svg/>"})
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/new.ipm.svg: new content") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func gold(name string) string {
	return `{"src":{"ipmt":"` + name + `"},"nodes":[{"id":1,"name":"` + name + `"}],"edges":[]}`
}

func TestGoldenIdentityRenumbered(t *testing.T) {
	base := map[string]string{"tests/x/v.json": gold("A"), "tests/x/v-02.json": gold("B"), "tests/x/v-03.json": gold("C")}
	// B gone from everywhere: content lost.
	dir, sha := repo(t, base)
	write(t, dir, map[string]string{"tests/x/v-02.json": gold("C")})
	os.Remove(filepath.Join(dir, "tests/x/v-03.json"))
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/v-02.json: content lost") {
		t.Fatalf("code %d out %s", code, out)
	}
	// B and C swapped paths: renumbered.
	dir, sha = repo(t, base)
	write(t, dir, map[string]string{"tests/x/v-02.json": gold("C"), "tests/x/v-03.json": gold("B")})
	code, out = runTool(dir, sha)
	if code != 0 || !strings.Contains(out, "RENUMBERED tests/x/v-02.json") || !strings.Contains(out, "renumbered 2") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityDeduped(t *testing.T) {
	x := func(s string) string {
		return `{"src":{"ipmt":"` + s + `"},"nodes":[{"id":1}],"edges":[]}`
	}
	dir, sha := repo(t, map[string]string{"tests/x/c.json": x("s1"), "tests/x/c-02.json": x("s2")})
	os.Remove(filepath.Join(dir, "tests/x/c-02.json"))
	code, out := runTool(dir, sha)
	if code != 0 || !strings.Contains(out, "DEDUPED tests/x/c-02.json") || !strings.Contains(out, "deduped 1") {
		t.Fatalf("code %d out %s", code, out)
	}
}

// The content lookup runs over all directories of the same kind.
func TestGoldenIdentityDedupedCrossDir(t *testing.T) {
	x := func(s string) string {
		return `{"src":{"ipmt":"s"},"nodes":[{"id":1,"name":"` + s + `"}],"edges":[]}`
	}
	// Y of the deleted dirA/c-02.json now lives in dirB/new.json.
	dir, sha := repo(t, map[string]string{"tests/dirA/c.json": x("X"), "tests/dirA/c-02.json": x("Y")})
	os.Remove(filepath.Join(dir, "tests/dirA/c-02.json"))
	write(t, dir, map[string]string{"tests/dirB/new.json": x("Y")})
	code, out := runTool(dir, sha)
	if code != 0 || !strings.Contains(out, "DEDUPED tests/dirA/c-02.json") || strings.Contains(out, "new content") {
		t.Fatalf("cross dir: code %d out %s", code, out)
	}
	// Negative twin: the content is in no directory.
	dir, sha = repo(t, map[string]string{"tests/dirA/c.json": x("X"), "tests/dirA/c-02.json": x("Y")})
	os.Remove(filepath.Join(dir, "tests/dirA/c-02.json"))
	write(t, dir, map[string]string{"tests/dirB/new.json": x("X")})
	code, out = runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/dirA/c-02.json: content lost") {
		t.Fatalf("lost: code %d out %s", code, out)
	}
	// Kind separation: a layout.json equal to the parser json is no match.
	dir, sha = repo(t, map[string]string{"tests/dirA/c.json": x("X"), "tests/dirA/c-02.json": x("Y")})
	os.Remove(filepath.Join(dir, "tests/dirA/c-02.json"))
	write(t, dir, map[string]string{"tests/dirB/new.layout.json": x("Y")})
	code, out = runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/dirA/c-02.json: content lost") {
		t.Fatalf("kind: code %d out %s", code, out)
	}
}

func TestGoldenIdentityNewContentFails(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": gold("A")})
	write(t, dir, map[string]string{"tests/x/b.json": gold("Z")})
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/b.json: new content") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityParserErrorRule(t *testing.T) {
	e := func(msg string, start int) string {
		return `{"end":9,"error":"` + msg + `","message":"` + msg + `","start":` + string(rune('0'+start)) + `}`
	}
	p := "tests/x/bad.parser.error.json"
	dir, sha := repo(t, map[string]string{p: e("PartOf", 1)})
	write(t, dir, map[string]string{p: e("PartOf", 2)})
	if code, out := runTool(dir, sha); code != 0 {
		t.Fatalf("same message, other offsets: code %d out %s", code, out)
	}
	write(t, dir, map[string]string{p: e("LeadsTo", 1)})
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL "+p+": error message changed (old: PartOf new: LeadsTo)") {
		t.Fatalf("other message: code %d out %s", code, out)
	}
	if strings.Contains(out, "neither parser golden") {
		t.Fatalf("old rule applied: %s", out)
	}
}

// A generated file whose stem has a new .ipmt next to it is a NEW-CASE, not a
// FAIL; without the new source it stays "new content".
func TestGoldenIdentityNewCase(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": gold("A")})
	write(t, dir, map[string]string{
		"tests/x/n.ipmt": "n", "tests/x/n.json": gold("Z"),
		"tests/x/n.layout.json": gold("L"), "tests/x/n.ipm.svg": "<svg/>",
	})
	code, out := runTool(dir, sha)
	if code != 0 || !strings.Contains(out, "NEW-CASE tests/x/n.json") ||
		!strings.Contains(out, "NEW-CASE tests/x/n.layout.json") || !strings.Contains(out, "NEW-CASE tests/x/n.ipm.svg") ||
		!strings.Contains(out, "new case 3") || strings.Contains(out, "FAIL") {
		t.Fatalf("with source: code %d out %s", code, out)
	}
	dir, sha = repo(t, map[string]string{"tests/x/a.json": gold("A")})
	write(t, dir, map[string]string{"tests/x/n.ipmt": "n", "tests/x/m.json": gold("Z")})
	code, out = runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/m.json: new content") {
		t.Fatalf("without source: code %d out %s", code, out)
	}
}

// A .validate.error.json is compared by its sorted (code, message) pairs;
// line, column and the other fields are ignored.
func TestGoldenIdentityValidateSidecar(t *testing.T) {
	v := func(msg1 string, col1 int, msg2 string, col2 int) string {
		f := func(msg string, col int) string {
			return `{"code":"IPMV1.1","message":"` + msg + `","line":1,"column":` + string(rune('0'+col)) + `}`
		}
		return `{"codes":["IPMV1.1"],"errors":[` + f(msg1, col1) + `,` + f(msg2, col2) + `]}`
	}
	p := "tests/x/s.validate.error.json"
	dir, sha := repo(t, map[string]string{p: v("a", 3, "b", 7)})
	write(t, dir, map[string]string{p: v("b", 3, "a", 7)})
	if code, out := runTool(dir, sha); code != 0 || strings.Contains(out, "neither parser golden") {
		t.Fatalf("same pairs, other columns and order: code %d out %s", code, out)
	}
	write(t, dir, map[string]string{p: v("a", 3, "c", 7)})
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL "+p) {
		t.Fatalf("other message: code %d out %s", code, out)
	}
	os.Remove(filepath.Join(dir, p))
	if code, out := runTool(dir, sha); code != 1 || !strings.Contains(out, "FAIL "+p+": deleted") {
		t.Fatalf("deleted: code %d out %s", code, out)
	}
}

func TestGoldenIdentitySourceListsAndCounts(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.ipmt": "a", "tests/x/b.md": "b"})
	os.Remove(filepath.Join(dir, "tests/x/b.md"))
	write(t, dir, map[string]string{"tests/x/n.ipmt": "n"})
	code, out := runTool(dir, sha)
	if code != 0 || !strings.Contains(out, "DELETED-SOURCE tests/x/b.md") || !strings.Contains(out, "NEW-SOURCE tests/x/n.ipmt") ||
		!strings.Contains(out, "compared 0, renumbered 0, deduped 0, new source 1, deleted source 1, not compared 0") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityUnreadableBaseJSONFails(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": "not json"})
	write(t, dir, map[string]string{"tests/x/a.json": gold("A")})
	code, out := runTool(dir, sha)
	if code != 1 || !strings.Contains(out, "FAIL tests/x/a.json") || !strings.Contains(out, "base json unreadable") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityEmptyDiffOk(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": gold("A")})
	code, out := runTool(dir, sha)
	if code != 0 || !strings.Contains(out, "compared 0, renumbered 0, deduped 0, new source 0, deleted source 0, not compared 0") {
		t.Fatalf("code %d out %s", code, out)
	}
}

func TestGoldenIdentityBadInputExits2(t *testing.T) {
	dir, sha := repo(t, map[string]string{"tests/x/a.json": golden1})
	if code, _ := runTool(dir, "deadbeefdeadbeef"); code != 2 {
		t.Fatalf("bad base: code %d", code)
	}
	if code, _ := runTool(filepath.Join(dir, "nope"), sha); code != 2 {
		t.Fatalf("missing root: code %d", code)
	}
}
