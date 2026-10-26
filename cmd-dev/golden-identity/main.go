package main

// Command golden-identity proves that regenerated test fixtures changed only
// in source text, byte offsets or hashes: the graph is identical.
//
// Usage:
//
//	go run ./cmd-dev/golden-identity -root DIR -base SHA
//
// Every file under DIR/tests that differs between git SHA and the working tree
// (changed, deleted or untracked) is checked by the rule table in
// gl:docs/dev-tools/golden-identity.md#golden-identity. Exit 0 and
// "golden-identity ok" when no file violates its rule, 1 and
// "golden-identity FAIL n" otherwise, 2 on unreadable input or a bad -base.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// dropKeys are removed at every level before comparing: the source text, the
// offset maps and the hash fields.
var dropKeys = map[string]bool{
	"ipmt": true, "positions": true,
	"source_sha1": true, "content_sha1": true, "outputs": true,
}

func git(root string, args ...string) ([]byte, error) {
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func strip(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, x := range t {
			if !dropKeys[k] {
				m[k] = strip(x)
			}
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, x := range t {
			s[i] = strip(x)
		}
		return s
	}
	return v
}

func decode(b []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func isSource(p string) bool {
	base := filepath.Base(p)
	if base == "_refs.json" {
		return true
	}
	for _, s := range []string{".md", ".ipmt", ".ipmt-invalid", ".dsl", ".solved.ipmt", ".defaults.ipmt"} {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	return false
}

// kindOf returns the comparison kind of a file: "error" (parser error json),
// "layout", "svg", "parser" (parser golden json) or "" for any other file.
func kindOf(p string) string {
	switch {
	case strings.HasSuffix(p, ".parser.error.json"):
		return "error"
	case strings.HasSuffix(p, ".validate.error.json"):
		return "validate"
	case strings.HasSuffix(p, ".ipm.svg"):
		return "svg"
	case strings.HasSuffix(p, ".layout.json"):
		return "layout"
	case strings.HasSuffix(p, ".json"):
		return "parser"
	}
	return ""
}

// norm returns the normalized content of a golden: svg bytes, or the stripped
// json encoded canonically (map keys sorted by encoding/json).
func norm(p string, b []byte, what string) (string, string) {
	switch kindOf(p) {
	case "svg":
		return string(b), ""
	case "layout", "parser":
		m, err := decode(b)
		if err != nil {
			return "", what + " unreadable: " + err.Error()
		}
		if kindOf(p) == "parser" {
			_, hn := m["nodes"]
			_, he := m["edges"]
			if !hn && !he {
				return "", "json is neither parser golden nor layout"
			}
		}
		enc, _ := json.Marshal(strip(m))
		return string(enc), ""
	}
	return "", "unexpected file type changed"
}

// errorKey returns the error and message fields of a .parser.error.json.
func errorKey(b []byte) (string, string, error) {
	m, err := decode(b)
	if err != nil {
		return "", "", err
	}
	e, _ := m["error"].(string)
	msg, _ := m["message"].(string)
	return e, msg, nil
}

// findingKey returns the sorted (code, message) pairs of a .validate.error.json;
// line, column and the other fields are ignored.
func findingKey(b []byte) (string, error) {
	m, err := decode(b)
	if err != nil {
		return "", err
	}
	var keys []string
	errs, _ := m["errors"].([]any)
	for _, x := range errs {
		f, _ := x.(map[string]any)
		c, _ := f["code"].(string)
		msg, _ := f["message"].(string)
		keys = append(keys, c+": "+msg)
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n"), nil
}

// genStem returns the case stem (directory and name) of a generated file.
func genStem(p string) string {
	for _, s := range []string{".layout.json", ".ipm.svg", ".json"} {
		if strings.HasSuffix(p, s) {
			return strings.TrimSuffix(p, s)
		}
	}
	return p
}

// check returns "" when ok, "skip" for source/hash files, else the violation.
// It compares one file by path and is used for a path that exists at base and
// now; a content mismatch is left to the caller (returns "differs").
func check(p string, old, cur []byte) string {
	if isSource(p) {
		return "skip"
	}
	if kindOf(p) == "validate" {
		ok, err := findingKey(old)
		if err != nil {
			return "base json unreadable: " + err.Error()
		}
		nk, err := findingKey(cur)
		if err != nil {
			return "json unreadable: " + err.Error()
		}
		if ok != nk {
			return "findings changed (old: " + strings.ReplaceAll(ok, "\n", " | ") + " new: " + strings.ReplaceAll(nk, "\n", " | ") + ")"
		}
		return ""
	}
	if kindOf(p) == "error" {
		oe, om, err := errorKey(old)
		if err != nil {
			return "base json unreadable: " + err.Error()
		}
		ne, nm, err := errorKey(cur)
		if err != nil {
			return "json unreadable: " + err.Error()
		}
		if oe != ne || om != nm {
			return "error message changed (old: " + om + " new: " + nm + ")"
		}
		return ""
	}
	a, r := norm(p, old, "base json")
	if r != "" {
		return r
	}
	b, r := norm(p, cur, "json")
	if r != "" {
		return r
	}
	if a != b {
		return "differs"
	}
	return ""
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("golden-identity", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	base := fs.String("base", "", "git sha to compare against")
	if fs.Parse(args) != nil || *base == "" {
		fmt.Fprintln(stderr, "usage: golden-identity -root DIR -base SHA")
		return 2
	}
	if st, err := os.Stat(*root); err != nil || !st.IsDir() {
		fmt.Fprintln(stderr, "golden-identity: unreadable root", *root)
		return 2
	}
	if _, err := git(*root, "rev-parse", "--verify", "--quiet", *base+"^{commit}"); err != nil {
		fmt.Fprintln(stderr, "golden-identity: bad -base:", err)
		return 2
	}
	diff, err := git(*root, "diff", "--no-renames", "--name-status", *base, "--", "tests")
	if err != nil {
		fmt.Fprintln(stderr, "golden-identity:", err)
		return 2
	}
	untracked, err := git(*root, "ls-files", "--others", "--exclude-standard", "--", "tests")
	if err != nil {
		fmt.Fprintln(stderr, "golden-identity:", err)
		return 2
	}
	// Groups: the kind only, over all directories (the generator dedups across
	// directories). Base and current content sets are loaded lazily, once per
	// group.
	type group struct{ kind string }
	baseSets, curSets := map[group]map[string]bool{}, map[group]map[string]bool{}
	var baseNames, curNames []string
	if out, err := git(*root, "ls-tree", "-r", "--name-only", *base, "--", "tests"); err == nil {
		baseNames = strings.Fields(string(out))
	} else {
		fmt.Fprintln(stderr, "golden-identity:", err)
		return 2
	}
	if out, err := git(*root, "ls-files", "--cached", "--others", "--exclude-standard", "--", "tests"); err == nil {
		for _, n := range strings.Fields(string(out)) {
			if _, err := os.Stat(filepath.Join(*root, n)); err == nil {
				curNames = append(curNames, n)
			}
		}
	} else {
		fmt.Fprintln(stderr, "golden-identity:", err)
		return 2
	}
	load := func(sets map[group]map[string]bool, g group, names []string, read func(string) ([]byte, error)) map[string]bool {
		if s, ok := sets[g]; ok {
			return s
		}
		s := map[string]bool{}
		for _, n := range names {
			if kindOf(n) != g.kind {
				continue
			}
			b, err := read(n)
			if err != nil {
				continue
			}
			if k, r := norm(n, b, ""); r == "" {
				s[k] = true
			}
		}
		sets[g] = s
		return s
	}
	baseSet := func(g group) map[string]bool {
		return load(baseSets, g, baseNames, func(n string) ([]byte, error) { return git(*root, "show", *base+":"+n) })
	}
	curSet := func(g group) map[string]bool {
		return load(curSets, g, curNames, func(n string) ([]byte, error) { return os.ReadFile(filepath.Join(*root, n)) })
	}

	var lines []string
	fails, compared, renumbered, deduped, newSrc, delSrc, skipped := 0, 0, 0, 0, 0, 0, 0
	fail := func(p, what string) {
		fails++
		lines = append(lines, fmt.Sprintf("FAIL %s: %s", p, what))
	}
	type entry struct{ st, p string }
	var entries []entry
	for _, line := range strings.Split(strings.TrimSpace(string(diff)), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 2)
		if len(f) != 2 {
			fmt.Fprintln(stderr, "golden-identity: bad diff line", line)
			return 2
		}
		entries = append(entries, entry{f[0][:1], f[1]})
	}
	for _, p := range strings.Split(strings.TrimSpace(string(untracked)), "\n") {
		if p != "" {
			entries = append(entries, entry{"A", p})
		}
	}
	newStems := map[string]bool{}
	for _, e := range entries {
		if e.st == "A" && strings.HasSuffix(e.p, ".ipmt") && !strings.HasSuffix(e.p, ".solved.ipmt") && !strings.HasSuffix(e.p, ".defaults.ipmt") {
			newStems[strings.TrimSuffix(e.p, ".ipmt")] = true
		}
	}
	newCase := 0
	failNew := func(p string) {
		if newStems[genStem(p)] {
			newCase++
			lines = append(lines, "NEW-CASE "+p)
			return
		}
		fail(p, "new content")
	}
	for _, e := range entries {
		st, p := e.st, e.p
		g := group{kindOf(p)}
		if isSource(p) {
			switch st {
			case "D":
				delSrc++
				lines = append(lines, "DELETED-SOURCE "+p)
			case "A":
				newSrc++
				lines = append(lines, "NEW-SOURCE "+p)
			default:
				skipped++
			}
			continue
		}
		var old, cur []byte
		if st != "A" {
			b, err := git(*root, "show", *base+":"+p)
			if err != nil {
				fmt.Fprintln(stderr, "golden-identity:", err)
				return 2
			}
			old = b
		}
		if st != "D" {
			b, err := os.ReadFile(filepath.Join(*root, p))
			if err != nil {
				fmt.Fprintln(stderr, "golden-identity:", err)
				return 2
			}
			cur = b
		}
		if g.kind == "error" || g.kind == "validate" {
			// By path: the error and message must equal base.
			switch st {
			case "D":
				fail(p, "deleted")
			case "A":
				fail(p, "new file")
			default:
				if r := check(p, old, cur); r != "" {
					fail(p, r)
				} else {
					compared++
				}
			}
			continue
		}
		if g.kind == "" {
			fail(p, "unexpected file type changed")
			continue
		}
		var oldN, curN string
		if st != "A" {
			n, r := norm(p, old, "base json")
			if r != "" {
				fail(p, r)
				continue
			}
			oldN = n
		}
		if st != "D" {
			n, r := norm(p, cur, "json")
			if r != "" {
				fail(p, r)
				continue
			}
			curN = n
		}
		switch st {
		case "D":
			if curSet(g)[oldN] {
				deduped++
				lines = append(lines, "DEDUPED "+p)
			} else {
				fail(p, "content lost")
			}
		case "A":
			if baseSet(g)[curN] {
				renumbered++
				lines = append(lines, "RENUMBERED "+p)
			} else {
				failNew(p)
			}
		default:
			switch {
			case oldN == curN:
				compared++
			case !curSet(g)[oldN]:
				fail(p, "content lost")
			case !baseSet(g)[curN]:
				fail(p, "new content")
			default:
				renumbered++
				lines = append(lines, "RENUMBERED "+p)
			}
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintf(stdout, "compared %d, renumbered %d, deduped %d, new source %d, deleted source %d, not compared %d, new case %d\n",
		compared, renumbered, deduped, newSrc, delSrc, skipped, newCase)
	if fails > 0 {
		fmt.Fprintf(stdout, "golden-identity FAIL %d\n", fails)
		return 1
	}
	fmt.Fprintln(stdout, "golden-identity ok")
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
