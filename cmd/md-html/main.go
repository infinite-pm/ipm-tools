// md-html — exports `.md` files with ipmt embeds to static HTML.
//
// Pipeline:
//
//	read config  →  resolve [[file]] / [[glob]] mappings
//	            →  for each (src .md, out .html):
//	                  read markdown
//	                  inline ipmt code blocks (syntax-highlighted)
//	                  inline `<!-- ipm-svg --> ![](…)` pairs
//	                  rewrite relative .md links to .html
//	                  copy non-ipmt images to out_dir
//	                  wrap with page header + palette CSS
//	            →  write
//
// See pkg/mdhtml for the library; this binary is just the CLI shell.
//
// Sibling to cmd/md-embed:
//   - md-embed refreshes on-disk SVGs + marker hashes (mutates the repo).
//   - md-html publishes those committed sources to static HTML (writes
//     out_dir; never touches the source tree).
//
// Future: vscode-ipm may grow "Watch _site" / "Refresh _site" commands
// that spawn this binary, for VS Code extension integration.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/infinite-pm/ipm-tools/pkg/mdhtml"
)

func main() {
	configPath := flag.String("config", "md-html.conf.json", "path to the md-html config file")
	check := flag.Bool("check", false, "dry run: resolve mappings + validate sources, write no files")
	verbose := flag.Bool("verbose", false, "log one line per file processed")
	flag.Parse()

	if err := run(*configPath, *check, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "md-html: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, check, verbose bool) error {
	cfg, err := mdhtml.LoadConfig(configPath)
	if err != nil {
		return err
	}
	configAbs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	configDir := filepath.Dir(configAbs)

	mappings, err := mdhtml.ResolveMappings(cfg, configDir)
	if err != nil {
		return err
	}

	if check {
		var missing int
		for _, m := range mappings {
			if _, err := os.Stat(m.SrcAbs); err != nil {
				fmt.Fprintf(os.Stderr, "missing source: %s\n", m.SrcAbs)
				missing++
			} else {
				fmt.Printf("would write: %s\n", m.OutAbs)
			}
		}
		if missing > 0 {
			return fmt.Errorf("%d source file(s) missing", missing)
		}
		return nil
	}

	totalBlocks := 0
	totalSVGs := 0
	for _, m := range mappings {
		res, err := mdhtml.RenderPage(cfg, m.SrcAbs, m.OutAbs, m.SrcRel)
		if err != nil {
			return fmt.Errorf("render %s: %w", m.SrcRel, err)
		}
		totalBlocks += res.IpmtBlocks
		totalSVGs += res.SVGEmbeds
		if verbose {
			fmt.Printf("[md-html] %s -> %s (%d blocks, %d SVGs)\n",
				m.SrcRel, m.OutAbs, res.IpmtBlocks, res.SVGEmbeds)
		}
	}
	fmt.Printf("[md-html] done — %d files, %d ipmt code blocks, %d SVG embeds\n",
		len(mappings), totalBlocks, totalSVGs)
	return nil
}
