package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// runDraft prints a draft ipmt-markers.manifest built from the -prose report.
// It is read-only; the lead reviews and prunes the draft.
// gl:docs/dev-tools/ipmt-migrate.md#the-manifest
func runDraft(files [][2]string, stdout, stderr io.Writer) int {
	sorted := append([][2]string(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })

	var body []string
	var nFiles, nFileEntries, nLineEntries, nComments int
	for _, f := range sorted {
		data, err := os.ReadFile(f[1])
		if err != nil {
			fmt.Fprintf(stderr, "ipmt-migrate: %v\n", err)
			return 2
		}
		name := f[0]
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		var out []string
		seen := map[string]bool{}
		teaching := false
		for _, h := range ProseReport(name, data) {
			if strings.HasSuffix(h, teachingNote) {
				teaching = true
				continue
			}
			n := 0
			if rest := strings.TrimPrefix(h, name+":"); rest != h {
				if i := strings.IndexByte(rest, ':'); i > 0 {
					n, _ = strconv.Atoi(rest[:i])
				}
			}
			text := strings.TrimPrefix(h, fmt.Sprintf("%s:%d: ", name, n))
			if n >= 1 && n <= len(lines) {
				text = lines[n-1]
			}
			switch {
			case !reAnyMark.MatchString(text) || strings.Contains(text, "\t"):
				out = append(out, fmt.Sprintf("# %s:%d: %s", name, n, strings.ReplaceAll(text, "\t", " ")))
				nComments++
			case seen[text]:
			default:
				seen[text] = true
				out = append(out, fmt.Sprintf("line\t%s\t%s\tprose about the markers", name, text))
				nLineEntries++
			}
		}
		if teaching {
			e, t := countMarks(string(data))
			out = append([]string{fmt.Sprintf("file\t%s\te=%d t=%d\tteaching fence with explicit ::t", name, e, t)}, out...)
			nFileEntries++
		}
		if len(out) > 0 {
			nFiles++
			body = append(body, out...)
		}
	}
	fmt.Fprintln(stdout, "# DRAFT ipmt-markers.manifest, made by ipmt-migrate -manifest-draft.")
	fmt.Fprintln(stdout, "# Review it and prune what should not stay; '# path:line:' lines are for a human to decide.")
	fmt.Fprintln(stdout, "rule\tthing")
	for _, l := range body {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintf(stdout, "# DRAFT-TOTAL files=%d file-entries=%d line-entries=%d comments=%d\n", nFiles, nFileEntries, nLineEntries, nComments)
	return 0
}
