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

// BuildEngine produces a layout-gen (and, when the ref has one, a
// layout-debug) for ref. "workdir" builds the working tree as it stands.
//
// Builds are cached under <cache>/<sha>/; a second run against the same
// commit costs nothing. The working tree is never cached — that is the side
// being iterated on.
func BuildEngine(repo, ref, name, cache string, prebuilt string, verbose bool) (Engine, error) {
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
		gen, dbg, err := goBuild(repo, binDir, verbose)
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
	gen, dbg, err = goBuild(srcDir, binDir, verbose)
	if err != nil {
		return e, err
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
func goBuild(srcDir, binDir string, verbose bool) (gen, dbg string, err error) {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", "", err
	}
	gen = filepath.Join(binDir, "layout-gen")
	if out, err := goBuildOne(srcDir, gen, "./cmd/layout-gen"); err != nil {
		return "", "", fmt.Errorf("build layout-gen in %s: %v\n%s", srcDir, err, out)
	}
	dbg = filepath.Join(binDir, "layout-debug")
	if out, err := goBuildOne(srcDir, dbg, "./cmd-dev/layout-debug"); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "layout-audit: no layout-debug at this ref (%v)\n%s", err, out)
		}
		dbg = ""
	}
	return gen, dbg, nil
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
