package layoutaudit

// Getting an engine: resolve a git ref, export that tree, build it.
//
// `git archive` rather than `git worktree`: there is no bookkeeping to leak,
// nothing to prune if the run dies, and it works on a dirty repository —
// which is the normal case here, since the whole point is to compare the
// working tree against a commit. ipm-tools has no `replace` directives, so an
// exported tree builds standalone against the module cache.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// WorkdirRef names the working tree as a pseudo-ref, so a caller can accept
// "HEAD", "v0.4.2" and "workdir" through one option.
const WorkdirRef = "workdir"

// Engine is one build of the layout engine: a layout-gen binary and the story
// of where it came from, which a report prints so a reader knows what they
// are looking at.
type Engine struct {
	Name      string // a label for the report ("old", "new", "2026-06-29")
	Ref       string // the ref as typed ("HEAD", "workdir", "v0.4.2")
	SHA       string // resolved commit, empty for the working tree
	Subject   string // commit subject, or the dirty-file count for a workdir
	Dirty     bool
	LayoutGen string // path to the built layout-gen
	LayoutDbg string // path to the built layout-debug ("" if that ref has none)
}

// Describe renders the one-line provenance a report shows.
func (e Engine) Describe() string {
	switch {
	case e.SHA == "":
		return fmt.Sprintf("%s — %s", e.Ref, e.Subject)
	case e.Dirty:
		return fmt.Sprintf("%s (%s, dirty) — %s", e.Ref, Short(e.SHA), e.Subject)
	default:
		return fmt.Sprintf("%s (%s) — %s", e.Ref, Short(e.SHA), e.Subject)
	}
}

// Short is the 7-character commit form the reports print.
func Short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// BuildOptions describes how an ERA of the project turns an .ipmt into
// layout JSON.
//
// The interface is not stable across a long history: today it is
// `layout-gen --in x.ipmt --out -`, but in 2025 the same repository parsed
// and laid out in two steps with different flags, and before that something
// else again. Rather than assume, a caller can name the packages to build and
// the commands to run — an adapter for that era — and everything downstream
// keeps talking to one binary that takes `--in file.ipmt` and writes JSON.
type BuildOptions struct {
	// Packages to `go build`; default ./cmd/layout-gen (plus
	// ./cmd-dev/layout-debug, best effort).
	Packages []string
	// Pipeline is the era's recipe, as shell commands with {bin}, {in},
	// {tmp} and {merge} placeholders. The LAST command must write the layout
	// JSON to stdout. Empty means today's single-step interface.
	Pipeline []string
	// Tools is the module TODAY's helper commands are built from — not the
	// era's. {merge} resolves to ipmt-name-merge built from here, because
	// putting an era's names back into its layout is this tool's repair of
	// that era, not something the era shipped.
	Tools string
}

// DefaultCache is where built engines live: outside the repository.
//
// They used to live under temp/, which put a 2 GB content-addressed cache
// inside a directory the editor watches recursively — and made --clean, whose
// job is to discard a report, throw away hours of builds along with it. A
// cache keyed by commit SHA is neither output nor scratch: it belongs where
// the platform puts caches, where it survives cleaning the report and costs
// the workspace nothing.
func DefaultCache() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "ipm-layout-engines")
	}
	return filepath.Join(os.TempDir(), "ipm-layout-engines")
}

// BuildEngine produces a layout-gen (and, when the ref has one, a
// layout-debug) for ref. "workdir" builds the working tree as it stands.
func BuildEngine(repo, ref, name, cache string, prebuilt string, verbose bool) (Engine, error) {
	return BuildEngineWith(repo, ref, name, cache, prebuilt, verbose, BuildOptions{})
}

// BuildEngineWith is BuildEngine for an era whose interface is not today's.
//
// Builds are cached under <cache>/<sha>/; a second run against the same
// commit costs nothing. The working tree is never cached — that is the side
// being iterated on.
func BuildEngineWith(repo, ref, name, cache string, prebuilt string, verbose bool, o BuildOptions) (Engine, error) {
	e := Engine{Name: name, Ref: ref}

	if prebuilt != "" {
		abs, err := filepath.Abs(prebuilt)
		if err != nil {
			return e, err
		}
		if _, err := os.Stat(abs); err != nil {
			return e, fmt.Errorf("--%s-bin: %w", name, err)
		}
		e.LayoutGen, e.Subject = abs, "supplied binary"
		return e, nil
	}

	if ref == WorkdirRef {
		e.Subject = workdirSubject(repo)
		e.Dirty = strings.Contains(e.Subject, "uncommitted")
		binDir := filepath.Join(cache, "workdir")
		gen, dbg, err := goBuild(repo, binDir, verbose, o)
		if err != nil {
			return e, err
		}
		e.LayoutGen, e.LayoutDbg = gen, dbg
		return e, nil
	}

	sha, err := Git(repo, "rev-parse", ref+"^{commit}")
	if err != nil {
		return e, fmt.Errorf("resolve %q: %w", ref, err)
	}
	e.SHA = sha
	if subj, err := Git(repo, "log", "-1", "--format=%s", sha); err == nil {
		e.Subject = subj
	}

	binDir := filepath.Join(cache, Short(sha))
	gen := filepath.Join(binDir, "layout-gen")
	dbg := filepath.Join(binDir, "layout-debug")
	// An era with its own recipe is entered through its ADAPTER, not through
	// whatever binary happens to be called layout-gen. Returning the latter on
	// a cache hit handed the sweep a tool that takes JSON where it was
	// promised one that takes ipmt: every diagram failed, and the column
	// reported "nothing moved" when the truth was "nothing ran".
	if len(o.Pipeline) > 0 {
		gen = filepath.Join(binDir, "adapter")
	}
	if _, err := os.Stat(gen); err == nil {
		e.LayoutGen = gen
		if _, err := os.Stat(dbg); err == nil {
			e.LayoutDbg = dbg
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "layout-audit: %s engine cached (%s)\n", name, Short(sha))
		}
		return e, nil
	}

	srcDir := filepath.Join(cache, "src", Short(sha))
	if err := exportTree(repo, sha, srcDir); err != nil {
		return e, err
	}
	gen, dbg, err = goBuild(srcDir, binDir, verbose, o)
	if err != nil {
		// The tree stays on a failure: the error names it, and a build that
		// did not work is the one time its contents are worth reading.
		return e, err
	}
	// The binaries are the cache; the tree they came out of is scratch. Left
	// behind it is not free — a checkout of this repository is ~11 MB and
	// ~2,000 files, and a history sweep does that per engine commit. Two
	// hundred and eighty of them had accumulated to 3.1 GB and 550,000 files,
	// inside a directory the editor watches recursively. Nothing reads a
	// source tree after its build, so nothing keeps one.
	if err := os.RemoveAll(srcDir); err != nil && verbose {
		fmt.Fprintf(os.Stderr, "layout-audit: leaving %s behind: %v\n", srcDir, err)
	}
	e.LayoutGen, e.LayoutDbg = gen, dbg
	return e, nil
}

// exportTree writes the tree at sha into dir (replacing whatever was there).
func exportTree(repo, sha, dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	archive := exec.Command("git", "-C", repo, "archive", "--format=tar", sha)
	untar := exec.Command("tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	var stderr bytes.Buffer
	archive.Stderr, untar.Stderr = &stderr, &stderr
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		return fmt.Errorf("git archive %s: %v: %s", Short(sha), err, stderr.String())
	}
	if err := pipe.Close(); err != nil && !errIsClosed(err) {
		return err
	}
	if err := untar.Wait(); err != nil {
		return fmt.Errorf("untar %s: %v: %s", Short(sha), err, stderr.String())
	}
	return nil
}

func errIsClosed(err error) bool {
	return strings.Contains(err.Error(), "file already closed")
}

// goBuild builds layout-gen (required) and layout-debug (best effort — an
// older ref may predate it, and its absence costs only the old-side
// copy-paste commands in the report).
func goBuild(srcDir, binDir string, verbose bool, o BuildOptions) (gen, dbg string, err error) {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", "", err
	}
	pkgs := o.Packages
	if len(pkgs) == 0 {
		pkgs = []string{"./cmd/layout-gen"}
	}
	for _, pkg := range pkgs {
		out := filepath.Join(binDir, filepath.Base(pkg))
		if msg, err := goBuildOne(srcDir, out, pkg); err != nil {
			return "", "", fmt.Errorf("build %s in %s: %v\n%s", pkg, srcDir, err, msg)
		}
	}
	gen = filepath.Join(binDir, "layout-gen")
	if len(o.Pipeline) > 0 {
		// An era whose interface is not today's gets an adapter: one
		// executable that takes --in file.ipmt and writes layout JSON, so
		// nothing downstream has to know which era it is talking to.
		merge, err := ensureMergeTool(filepath.Dir(binDir), o)
		if err != nil {
			return "", "", err
		}
		if gen, err = writeAdapter(binDir, o.Pipeline, merge); err != nil {
			return "", "", err
		}
	} else if _, serr := os.Stat(gen); serr != nil {
		return "", "", fmt.Errorf("no layout-gen built in %s", binDir)
	}
	dbg = filepath.Join(binDir, "layout-debug")
	if out, err := goBuildOne(srcDir, dbg, "./cmd-dev/layout-debug"); err != nil || len(o.Pipeline) > 0 {
		if verbose {
			fmt.Fprintf(os.Stderr, "layout-audit: no layout-debug at this ref (%v)\n%s", err, out)
		}
		dbg = ""
	}
	return gen, dbg, nil
}

// writeAdapter emits the era's recipe as one executable.
// ensureMergeTool builds the helper an era's recipe may pipe through, once
// per cache, from TODAY's source — it is this tool's repair of that era, not
// something the era shipped. A recipe that never says {merge} never needs it,
// so a missing Tools module is only an error for one that does.
func ensureMergeTool(cache string, o BuildOptions) (string, error) {
	if !mentionsMerge(o.Pipeline) {
		return "", nil
	}
	mergeMu.Lock()
	defer mergeMu.Unlock()
	if p, done := mergeBuilt[cache]; done {
		return p, nil
	}

	path := filepath.Join(cache, "tools", "ipmt-name-merge")
	if o.Tools == "" {
		// Nothing to build from. An existing one still beats failing the run.
		if _, err := os.Stat(path); err == nil {
			mergeBuilt[cache] = path
			return path, nil
		}
		return "", fmt.Errorf("this era's recipe uses {merge} but no Tools module was given to build it from")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if msg, err := goBuildOne(o.Tools, path, "./cmd-dev/ipmt-name-merge"); err != nil {
		return "", fmt.Errorf("build ipmt-name-merge in %s: %v\n%s", o.Tools, err, msg)
	}
	mergeBuilt[cache] = path
	return path, nil
}

// mergeBuilt records which caches have had the helper rebuilt in THIS process.
//
// It used to be an exists-check with no invalidation, which is a different
// thing entirely: the helper is built from today's ipm-tools, so editing it
// left every era column running a binary from whenever the cache was first
// populated — the engines beside it are keyed by commit and cannot go stale,
// but this one silently could. Rebuilding once per run costs one small
// compile and removes the whole class; the map is what keeps it ONE, across
// the parallel builds of a long history.
var (
	mergeMu    sync.Mutex
	mergeBuilt = map[string]string{}
)

// shellQuote makes a path safe to drop into the generated /bin/sh recipe.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func mentionsMerge(pipeline []string) bool {
	for _, s := range pipeline {
		if strings.Contains(s, "{merge}") {
			return true
		}
	}
	return false
}

func writeAdapter(binDir string, pipeline []string, merge string) (string, error) {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n# generated by layoutaudit: this era's ipmt → layout JSON recipe\nset -eu\n")
	b.WriteString("in=''\nwhile [ $# -gt 0 ]; do\n  case \"$1\" in\n")
	b.WriteString("    --in) in=\"$2\"; shift 2 ;;\n    --in=*) in=\"${1#--in=}\"; shift ;;\n")
	b.WriteString("    *) shift ;;\n  esac\ndone\n")
	b.WriteString("tmp=\"$(mktemp)\"\ntrap 'rm -f \"$tmp\"' EXIT\n")
	// {bin} resolves at RUN time, from the script's own directory — never the
	// absolute path it was generated at. An adapter is a cached artifact, and
	// a cache that cannot be moved is not one: baking the path in meant that
	// relocating the cache left every era adapter pointing into thin air, and
	// because a missing engine reads as "this diagram could not be laid out",
	// seven columns reported 312 skipped diagrams instead of an error.
	b.WriteString("bin=\"$(cd \"$(dirname \"$0\")\" && pwd)\"\n")
	for _, step := range pipeline {
		cmd := strings.ReplaceAll(step, "{bin}", "\"$bin\"")
		cmd = strings.ReplaceAll(cmd, "{merge}", shellQuote(merge))
		cmd = strings.ReplaceAll(cmd, "{in}", "\"$in\"")
		cmd = strings.ReplaceAll(cmd, "{tmp}", "\"$tmp\"")
		b.WriteString(cmd + "\n")
	}
	path := filepath.Join(binDir, "adapter")
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func goBuildOne(dir, out, pkg string) (string, error) {
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// Git runs a git command in repo and returns its trimmed stdout.
func Git(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// workdirSubject describes the working tree the way provenance should be
// described: dirty is RECORDED, never hidden, because a report produced from
// uncommitted work cannot be reproduced from a commit alone.
func workdirSubject(repo string) string {
	head, err := Git(repo, "log", "-1", "--format=%h %s")
	if err != nil {
		head = "(no commits)"
	}
	status, err := Git(repo, "status", "--porcelain")
	if err != nil || strings.TrimSpace(status) == "" {
		return "clean working tree at " + head
	}
	n := len(strings.Split(strings.TrimSpace(status), "\n"))
	return fmt.Sprintf("%d uncommitted file(s) on top of %s", n, head)
}
