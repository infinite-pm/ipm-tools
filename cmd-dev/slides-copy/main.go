// slides-copy — copies selected ipmt-embedded SVGs to flat, sequentially
// named slide files (e.g. the infinite.pm landing-page carousel).
//
// The embedded diagrams live under a doc's `_ipm/<doc>/<id>.ipm.svg` tree
// with three-character marker ids (100, 110, 120, …) that don't line up with
// the carousel's 01..NN slot names. This tool reads a small JSON config that
// maps each source SVG to its destination slide name and copies them into a
// single out_dir.
//
// Config shape (paths resolved relative to the config file's own directory):
//
//	{
//	  "out_dir": "docs/slides",
//	  "slide": [
//	    { "src": "../ipm-intro/_ipm/README/100.ipm.svg", "out": "01.ipm.svg" },
//	    { "src": "../ipm-intro/_ipm/README/120.ipm.svg", "out": "02.ipm.svg" }
//	  ]
//	}
//
// With -prune, any *.ipm.svg in out_dir the config does not list is deleted,
// keeping the slide set an exact mirror of the config (out_dir is expected to
// be a dedicated, fully generated directory).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	OutDir string  `json:"out_dir"`
	Slides []slide `json:"slide"`
}

type slide struct {
	Src string `json:"src"`
	Out string `json:"out"`
}

func main() {
	configPath := flag.String("config", "slides.conf.json", "path to the slides config file")
	prune := flag.Bool("prune", false, "delete *.ipm.svg in out_dir that the config does not list")
	check := flag.Bool("check", false, "dry run: validate sources, copy/delete nothing")
	verbose := flag.Bool("verbose", false, "log one line per slide")
	flag.Parse()

	if err := run(*configPath, *prune, *check, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "slides-copy: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, prune, check, verbose bool) error {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", configPath, err)
	}
	if len(cfg.Slides) == 0 {
		return fmt.Errorf("%s lists no slides", configPath)
	}
	if cfg.OutDir == "" {
		return fmt.Errorf("%s: out_dir is required", configPath)
	}

	configAbs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	configDir := filepath.Dir(configAbs)

	outDir := cfg.OutDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(configDir, outDir)
	}

	// Resolve + validate every source first, so a missing file aborts the run
	// before we write or prune anything.
	type job struct{ srcAbs, outAbs string }
	var jobs []job
	keep := map[string]bool{}
	for _, s := range cfg.Slides {
		if s.Src == "" || s.Out == "" {
			return fmt.Errorf("slide entry needs both src and out: %+v", s)
		}
		srcAbs := s.Src
		if !filepath.IsAbs(srcAbs) {
			srcAbs = filepath.Join(configDir, s.Src)
		}
		if _, err := os.Stat(srcAbs); err != nil {
			return fmt.Errorf("missing source %s: %w", s.Src, err)
		}
		jobs = append(jobs, job{srcAbs, filepath.Join(outDir, s.Out)})
		keep[s.Out] = true
	}

	if check {
		for _, j := range jobs {
			fmt.Printf("would copy: %s -> %s\n", j.srcAbs, j.outAbs)
		}
		if prune {
			extra, err := pruneList(outDir, keep)
			if err != nil {
				return err
			}
			for _, p := range extra {
				fmt.Printf("would prune: %s\n", filepath.Join(outDir, p))
			}
		}
		return nil
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, j := range jobs {
		if err := copyFile(j.srcAbs, j.outAbs); err != nil {
			return fmt.Errorf("copy -> %s: %w", j.outAbs, err)
		}
		if verbose {
			fmt.Printf("[slides-copy] %s -> %s\n", j.srcAbs, j.outAbs)
		}
	}

	pruned := 0
	if prune {
		extra, err := pruneList(outDir, keep)
		if err != nil {
			return err
		}
		for _, p := range extra {
			if err := os.Remove(filepath.Join(outDir, p)); err != nil {
				return err
			}
			pruned++
			if verbose {
				fmt.Printf("[slides-copy] pruned %s\n", p)
			}
		}
	}
	fmt.Printf("[slides-copy] done — %d slides copied, %d pruned -> %s\n", len(jobs), pruned, outDir)
	return nil
}

// pruneList returns the *.ipm.svg basenames in outDir that keep does not name.
func pruneList(outDir string, keep map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var extra []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".ipm.svg") && !keep[name] {
			extra = append(extra, name)
		}
	}
	return extra, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
