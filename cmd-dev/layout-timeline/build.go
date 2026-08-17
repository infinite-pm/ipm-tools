package main

// Phase 1: the engines.
//
// Building is the slow half of a long history — 142 commits is 142 `go
// build`s — and it is also the half that never changes: a commit's binary is
// the same today as it was last week. So it is separated from the report,
// cached by commit, and run in parallel.
//
// `--build-only` stops here. The report run afterwards finds every binary in
// the cache and does sweeps only.

import (
	"fmt"
	"os"
	"sync"

	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
)

// buildAll builds every distinct commit in snaps, at most jobs at a time.
// A commit that will not build is not an error: an old enough one predates
// cmd/layout-gen, and the column simply says so later.
func buildAll(snaps []snapshot, cache string, jobs int, verbose bool) (built, failed int) {
	if jobs < 1 {
		jobs = 1
	}
	type job struct {
		repo, sha, label string
		opts             layoutaudit.BuildOptions
	}
	seen := map[string]bool{}
	var jobsList []job
	for _, s := range snaps {
		if s.SHA == "" || seen[s.Repo+s.SHA] {
			continue
		}
		seen[s.Repo+s.SHA] = true
		jobsList = append(jobsList, job{repo: s.Repo, sha: s.SHA, label: s.Label(),
			opts: layoutaudit.BuildOptions{Packages: s.Build, Pipeline: s.Pipeline}})
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan job)
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				_, err := layoutaudit.BuildEngineWith(j.repo, j.sha, j.label, cache, "", false, j.opts)
				mu.Lock()
				if err != nil {
					failed++
					if verbose {
						fmt.Fprintf(os.Stderr, "layout-timeline: %s will not build: %v\n", j.label, err)
					}
				} else {
					built++
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobsList {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return built, failed
}
