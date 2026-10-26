// Command ipmt-migrate adds an explicit ::t to every unmarked thing in ipmt
// text, so the default node kind can change without changing any diagram.
// It never changes anything else, and proves each diagram parses to the same
// graph before and after (also with DefaultNodeType=event).
//
//	ipmt-migrate -root DIR            dry run over the tracked *.ipmt and *.md of a git repo
//	ipmt-migrate -root DIR -write     the same, writing the files
//	ipmt-migrate -prose -root DIR     read-only: prose and comments that talk about ::t or the default
//	ipmt-migrate -flip -root DIR      add ::t to things and remove ::e from events outside the manifest (dry run; -write)
//	ipmt-migrate -manifest-draft -root DIR   read-only: print a draft manifest from the -prose report
//	ipmt-migrate -check-manifest -root DIR   fail if a manifest place lost its explicit marker
//
// gl:docs/dev-tools/ipmt-migrate.md#ipmt-migrate
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ipmt-migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "git repo: migrate its tracked *.ipmt and *.md files")
	write := fs.Bool("write", false, "write the migrated files")
	fs.Bool("dry-run", true, "print the report, write nothing (the default)")
	prose := fs.Bool("prose", false, "read-only prose report instead of a migration")
	flip := fs.Bool("flip", false, "add ::t to things and remove ::e from events outside the manifest places")
	draft := fs.Bool("manifest-draft", false, "read-only: print a draft ipmt-markers.manifest from the prose report")
	checkManifest := fs.Bool("check-manifest", false, "check the manifest places against the files")
	manifestFile := fs.String("manifest", "", "manifest file (default ROOT/ipmt-markers.manifest)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *draft && (*prose || *flip || *write || *checkManifest) {
		fmt.Fprintln(stderr, "ipmt-migrate: -manifest-draft is read-only and cannot be combined with -prose, -flip, -write or -check-manifest")
		return 2
	}
	if *checkManifest {
		if *root == "" {
			fmt.Fprintln(stderr, "ipmt-migrate: -check-manifest needs -root DIR")
			return 2
		}
		mf := *manifestFile
		if mf == "" {
			mf = filepath.Join(*root, "ipmt-markers.manifest")
		}
		fails, err := CheckManifestRoot(*root, mf)
		if err != nil {
			fmt.Fprintf(stderr, "ipmt-migrate: %v\n", err)
			return 2
		}
		for _, f := range fails {
			fmt.Fprintln(stdout, f)
		}
		if len(fails) > 0 {
			return 1
		}
		fmt.Fprintln(stdout, "manifest ok")
		return 0
	}
	if *root == "" && fs.NArg() == 0 {
		fmt.Fprintln(stderr, "ipmt-migrate: give -root DIR (a git repo) or file arguments")
		return 2
	}
	if *prose && *write {
		fmt.Fprintln(stderr, "ipmt-migrate: -prose is read-only and cannot be combined with -write")
		return 2
	}

	type input struct{ show, path string }
	var files []input
	if *root != "" {
		out, err := exec.Command("git", "-C", *root, "ls-files", "-z").Output()
		if err != nil {
			fmt.Fprintf(stderr, "ipmt-migrate: %s is not a git repository (git ls-files failed: %v)\n", *root, err)
			return 2
		}
		for _, rel := range strings.Split(string(out), "\x00") {
			if strings.HasSuffix(rel, ".md") || strings.HasSuffix(rel, ".ipmt") {
				p := filepath.Join(*root, rel)
				if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
					files = append(files, input{rel, p})
				}
			}
		}
	}
	for _, a := range fs.Args() {
		files = append(files, input{a, a})
	}

	if *draft {
		return runDraft(files2(files, func(i input) (string, string) { return i.show, i.path }), stdout, stderr)
	}
	if *flip {
		if *prose {
			fmt.Fprintln(stderr, "ipmt-migrate: -flip and -prose cannot be combined")
			return 2
		}
		mf := *manifestFile
		if mf == "" && *root != "" {
			mf = filepath.Join(*root, "ipmt-markers.manifest")
		}
		return runFlip(files2(files, func(i input) (string, string) { return i.show, i.path }), mf, *write, stdout, stderr)
	}
	if *prose {
		return runProse(files2(files, func(i input) (string, string) { return i.show, i.path }), stdout, stderr)
	}

	var totFiles, totFences, totNodes, totMarked, changed int
	var unparsable, fails []string
	for _, in := range files {
		data, err := os.ReadFile(in.path)
		if err != nil {
			fmt.Fprintf(stderr, "ipmt-migrate: %v\n", err)
			return 2
		}
		r := MigrateFile(in.show, data)
		totFiles++
		for _, f := range r.Fences {
			switch f.Status {
			case StatusUnparsable:
				unparsable = append(unparsable, fmt.Sprintf("%s:%d: %s", in.show, f.Line, firstLine(f.Detail)))
			case StatusFail:
				fails = append(fails, fmt.Sprintf("%s:%d: %s", in.show, f.Line, firstLine(f.Detail)))
			}
		}
		if len(r.Fences) > 0 {
			fmt.Fprintf(stdout, "%s: fences=%d nodes=%d marked=%d\n", in.show, len(r.Fences), r.Nodes, r.Marked)
		}
		totFences += len(r.Fences)
		totNodes += r.Nodes
		totMarked += r.Marked
		if string(r.Out) != string(data) {
			changed++
			if *write {
				if err := os.WriteFile(in.path, r.Out, 0o644); err != nil {
					fmt.Fprintf(stderr, "ipmt-migrate: %v\n", err)
					return 2
				}
			}
		}
	}
	mode := "dry-run"
	if *write {
		mode = "write"
	}
	fmt.Fprintf(stdout, "TOTAL (%s) files=%d changed=%d fences=%d nodes=%d marked=%d unparsable=%d fail=%d\n",
		mode, totFiles, changed, totFences, totNodes, totMarked, len(unparsable), len(fails))
	for _, u := range unparsable {
		fmt.Fprintf(stdout, "unparsable, left unchanged: %s\n", u)
	}
	for _, f := range fails {
		fmt.Fprintf(stdout, "FAIL: %s\n", f)
	}
	if len(fails) > 0 {
		return 1
	}
	return 0
}

func runProse(files [][2]string, stdout, stderr io.Writer) int {
	var nProse, nTeach int
	for _, f := range files {
		data, err := os.ReadFile(f[1])
		if err != nil {
			fmt.Fprintf(stderr, "ipmt-migrate: %v\n", err)
			return 2
		}
		for _, h := range ProseReport(f[0], data) {
			fmt.Fprintln(stdout, h)
			if strings.HasSuffix(h, teachingNote) {
				nTeach++
			} else {
				nProse++
			}
		}
	}
	fmt.Fprintf(stdout, "PROSE-TOTAL files=%d prose-lines=%d teaching-fences=%d\n", len(files), nProse, nTeach)
	return 0
}

func files2[T any](in []T, f func(T) (string, string)) [][2]string {
	out := make([][2]string, len(in))
	for i, x := range in {
		a, b := f(x)
		out[i] = [2]string{a, b}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
