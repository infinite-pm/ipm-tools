// regen-fixtures runs the whole fixture regeneration sequence in order and
// reports what changed. See docs/dev-tools/regen-fixtures.md#the-sequence.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const listCap = 40

type step struct {
	name string
	// cmd is the printed command line; run executes the step.
	cmd string
	run func(r *runner) error
}

type runner struct {
	root string
	bins string // temp dir with the built ipmt-parse, layout-gen, ipmsvg-gen
	base string
	tool bool // run go test at the end
}

func (r *runner) exec(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = r.root
	c.Env = append(os.Environ(), "GOWORK=off")
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	return c.Run()
}

func (r *runner) out(name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = r.root
	c.Env = append(os.Environ(), "GOWORK=off")
	var b bytes.Buffer
	c.Stdout = &b
	c.Stderr = os.Stderr
	err := c.Run()
	return b.String(), err
}

func goRun(tool string, args ...string) func(*runner) error {
	return func(r *runner) error { return r.exec("go", append([]string{"run", "./" + tool}, args...)...) }
}

func makeTarget(t string) func(*runner) error {
	return func(r *runner) error { return r.exec("make", t) }
}

// buildTools builds the three per-file tools once (hundreds of files).
func (r *runner) buildTools() error {
	if r.bins != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "regen-fixtures-bin")
	if err != nil {
		return err
	}
	r.bins = dir
	for _, t := range []string{"ipmt-parse", "layout-gen", "ipmsvg-gen"} {
		if err := r.exec("go", "build", "-o", filepath.Join(dir, t), "./cmd/"+t); err != nil {
			return err
		}
	}
	return nil
}

// perFile runs a shipping tool over every .ipmt in dirs (globs relative to the
// root) and writes the output next to it as <stem><ext>, only when it differs.
func perFile(tool string, globs []string, ext string, args func(in, out string) []string, stdout bool) func(*runner) error {
	return func(r *runner) error {
		if err := r.buildTools(); err != nil {
			return err
		}
		var files []string
		for _, g := range globs {
			m, _ := filepath.Glob(filepath.Join(r.root, g, "*.ipmt"))
			files = append(files, m...)
		}
		sort.Strings(files)
		for _, f := range files {
			out := strings.TrimSuffix(f, ".ipmt") + ext
			tmp := out + ".regen-tmp"
			c := exec.Command(filepath.Join(r.bins, tool), args(f, tmp)...)
			c.Dir = r.root
			var b bytes.Buffer
			c.Stdout = &b
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("%s %s: %w", tool, f, err)
			}
			if stdout {
				if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
					return err
				}
			}
			want, err := os.ReadFile(tmp)
			os.Remove(tmp)
			if err != nil {
				return err
			}
			if old, err := os.ReadFile(out); err == nil && bytes.Equal(old, want) {
				continue
			}
			if err := os.WriteFile(out, want, 0o644); err != nil {
				return err
			}
		}
		return nil
	}
}

func steps(base string, withTests bool) []step {
	ipmtDirs := []string{"tests/ipmt/*"}
	parseDirs := []string{
		"tests/ipmt/*", "tests/layout-gen", "tests/layout-gen-ext", "tests/layout-gen-anchor",
		"tests/layout-gen-containers", "tests/layout-gen-dupe", "tests/layout-gen-shells",
	}
	parseArgs := func(in, _ string) []string { return []string{"--in", in} }
	genArgs := func(in, out string) []string { return []string{"--in", in, "--out", out} }
	s := []step{
		{"gen-test-md", "make gen-test-md", makeTarget("gen-test-md")},
		{"gen-invalid-sidecars", "make gen-invalid-sidecars", makeTarget("gen-invalid-sidecars")},
		{"sync-test-cases", "make sync-test-cases", makeTarget("sync-test-cases")},
		{"ipmt-parse goldens", "ipmt-parse --in X.ipmt > X.json (tests/ipmt/*, tests/layout-gen*)", perFile("ipmt-parse", parseDirs, ".json", parseArgs, true)},
		{"layout-gen", "layout-gen --in X.ipmt --out X.layout.json (tests/ipmt/*)", perFile("layout-gen", ipmtDirs, ".layout.json", genArgs, false)},
		{"ipmsvg-gen", "ipmsvg-gen --in X.ipmt --out X.ipm.svg (tests/ipmt/*)", perFile("ipmsvg-gen", ipmtDirs, ".ipm.svg", genArgs, false)},
		{"gen-test-md again", "make gen-test-md", makeTarget("gen-test-md")},
		{"refs-rehash", "make refs-rehash", makeTarget("refs-rehash")},
		{"sync-test-cases (1)", "make sync-test-cases", makeTarget("sync-test-cases")},
		{"sync-test-cases (2)", "make sync-test-cases", makeTarget("sync-test-cases")},
		{"sync-test-cases --check", "go run ./cmd-dev/sync-test-cases --check", goRun("cmd-dev/sync-test-cases", "--check")},
		{"md-embed rehash", "go run ./cmd/md-embed --root . --no-prune", goRun("cmd/md-embed", "--root", ".", "--no-prune")},
	}
	if base != "" {
		s = append(s, step{"golden-identity", "go run ./cmd-dev/golden-identity -root . -base " + base, goRun("cmd-dev/golden-identity", "-root", ".", "-base", base)})
	}
	if withTests {
		s = append(s, step{"go test", "go test ./cmd-dev/... ./pkg/...", func(r *runner) error {
			return r.exec("go", "test", "./cmd-dev/...", "./pkg/...")
		}})
	}
	return s
}

// statusEntry is one parsed line of git status --porcelain -uall.
func parseStatus(out string) []Change {
	var cs []Change
	for _, l := range strings.Split(out, "\n") {
		if len(l) < 4 {
			continue
		}
		xy, p := l[:2], strings.TrimSpace(l[3:])
		var st string
		switch {
		case xy == "??":
			st = "?"
		case strings.Contains(xy, "D"):
			st = "D"
		case strings.Contains(xy, "A"):
			st = "A"
		default:
			st = "M"
		}
		cs = append(cs, Change{Path: p, Status: st})
	}
	return cs
}

func underOK(p string) bool {
	return strings.HasPrefix(p, "tests/") || strings.HasPrefix(p, "docs/")
}

func main() { os.Exit(run()) }

func run() int {
	doRun := flag.Bool("run", false, "run the sequence (default: print the plan only)")
	dry := flag.Bool("dry-run", false, "print the ordered plan, run nothing")
	keep := flag.Bool("keep-stale", false, "keep STALE-AT-BASE files instead of restoring them to HEAD")
	root := flag.String("root", ".", "repository root")
	base := flag.String("base", "", "git sha for the optional golden-identity step")
	noTest := flag.Bool("no-test", false, "skip the final go test step")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regen-fixtures:", err)
		return 2
	}
	r := &runner{root: abs, base: *base}
	plan := steps(*base, !*noTest)

	if *dry || !*doRun {
		fmt.Println("regen-fixtures plan (nothing is run):")
		for i, s := range plan {
			fmt.Printf("%2d. %-24s %s\n", i+1, s.name, s.cmd)
		}
		if !*doRun {
			fmt.Println("\nto run it: regen-fixtures -run   (make regen-fixtures-run)")
		}
		return 0
	}

	st, err := r.out("git", "status", "--porcelain", "-uall")
	if err != nil {
		fmt.Fprintln(os.Stderr, "regen-fixtures: git status:", err)
		return 2
	}
	pre := parseStatus(st)
	var bad []string
	for _, c := range pre {
		if !underOK(c.Path) {
			bad = append(bad, c.Path)
		}
	}
	fmt.Printf("git status before: %d changed paths (%d outside tests/ and docs/)\n", len(pre), len(bad))
	if len(bad) > 0 {
		for _, p := range bad {
			fmt.Println("  dirty:", p)
		}
		fmt.Fprintln(os.Stderr, "regen-fixtures: refusing a dirty tree outside tests/ and docs/")
		return 2
	}

	for i, s := range plan {
		fmt.Printf("== step %d/%d: %s\n", i+1, len(plan), s.name)
		if err := s.run(r); err != nil {
			fmt.Fprintf(os.Stderr, "regen-fixtures: step %d (%s) failed: %v\n", i+1, s.name, err)
			return 3
		}
	}
	if r.bins != "" {
		os.RemoveAll(r.bins)
	}
	return report(r, *keep)
}

// classifyTree reads git status and classifies every changed path.
func classifyTree(r *runner) (Report, error) {
	st, err := r.out("git", "status", "--porcelain", "-uall")
	if err != nil {
		return nil, err
	}
	cs := parseStatus(st)
	tracked, _ := r.out("git", "ls-files")
	stems := map[string]bool{}
	for _, p := range strings.Split(tracked, "\n") {
		if p != "" {
			stems[Stem(p)] = true
		}
	}
	changedSrc := map[string]bool{}
	for _, c := range cs {
		if c.Status != "D" {
			changedSrc[c.Path] = true
		}
	}
	for i := range cs {
		if cs[i].Status == "?" && !stems[Stem(cs[i].Path)] {
			cs[i].NoTrackedSibling = true
		}
	}
	return Classify(cs, func(p string) bool { return changedSrc[p] }), nil
}

func report(r *runner, keep bool) int {
	rep, err := classifyTree(r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regen-fixtures: git status:", err)
		return 3
	}
	if stale := rep[ClassStale]; !keep && len(stale) > 0 {
		args := append([]string{"checkout", "--"}, stale...)
		if err := r.exec("git", args...); err != nil {
			fmt.Fprintln(os.Stderr, "regen-fixtures: restoring stale files failed:", err)
			return 3
		}
		// The restored files change what the _refs.json hash maps should say.
		if err := r.exec("make", "refs-rehash"); err != nil {
			fmt.Fprintln(os.Stderr, "regen-fixtures: refs-rehash after restore failed:", err)
			return 3
		}
		after, err := classifyTree(r)
		if err != nil {
			fmt.Fprintln(os.Stderr, "regen-fixtures: git status:", err)
			return 3
		}
		after[ClassStale] = stale
		rep = after
	}

	fmt.Println("\n== report")
	for _, class := range Classes {
		ps := rep[class]
		if class == ClassExpected {
			fmt.Printf("%s: %d\n", class, len(ps))
			continue
		}
		note := ""
		if class == ClassStale {
			if keep {
				note = " (kept)"
			} else {
				note = " (restored to HEAD)"
			}
		}
		fmt.Printf("%s: %d%s\n", class, len(ps), note)
		for i, p := range ps {
			if i == listCap {
				fmt.Printf("  ... %d more\n", len(ps)-listCap)
				break
			}
			fmt.Println("  " + p)
		}
	}
	return 0
}
