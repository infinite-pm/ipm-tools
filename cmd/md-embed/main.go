// Command md-embed renders ```ipmt``` blocks in Markdown files to SVG and
// inserts a marker comment + image link after each block referencing the
// rendered file. Idempotent: running twice on an unchanged source is a no-op.
//
// Doc:  gl:docs/md-embed.md
// Related: gl:pkg/mdembed/ gl:cmd/ipmsvg-gen/main.go gl:pkg/ipmsvg/svg.go
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	ipconfig "github.com/infinite-pm/ipm-tools/pkg/ipm/config"
	iplog "github.com/infinite-pm/ipm-tools/pkg/ipm/log"
	"github.com/infinite-pm/ipm-tools/pkg/mdembed"
)

const (
	toolName    = "md-embed"
	toolVersion = "dev"
)

type config struct {
	root      string
	svgDir    string
	check     bool
	noPrune   bool
	dryRun    bool
	force     bool
	forceMeta bool
	verbose   bool
	quiet     bool
	in        string

	// Derived from noPrune in main(); read by the rest of the program.
	prune bool

	// confRoot is the directory holding the .ipm.conf that was loaded (the
	// repo root, found by walking up from --root). ignore patterns are matched
	// against paths relative to it. Both populated in main() from the config.
	confRoot string
	ignore   []string
}

type runStats struct {
	files         int
	blocks        int
	insertMarker  int
	rehash        int
	rerender      int
	metaSkipped   int
	unterminated  int
	malformed     int
	missingSrc    int
	noEmbed       int
	badMeta       int
	cleanups      int
	prunedOrphans int
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.root, "root", ".", "repo root (directory walked for .md files; SVGs land under <root>/<svg-dir>)")
	flag.StringVar(&cfg.svgDir, "svg-dir", "_ipm", "directory under <root> for generated SVGs")
	flag.BoolVar(&cfg.check, "check", false, "read-only: exit 1 if any block would be inserted, rehashed, or re-rendered (CI / pre-commit mode)")
	flag.BoolVar(&cfg.noPrune, "no-prune", false, "do NOT remove SVG files in <svg-dir> that no marker references (default: prune enabled)")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "show what would change without writing")
	flag.BoolVar(&cfg.force, "force", false, "re-render every block's SVG even when the marker hash + SVG file are already fresh (bypasses the freshness check)")
	flag.BoolVar(&cfg.forceMeta, "force-meta", false, "with --force, also rewrite SVGs whose only difference from disk is the generated-by provenance stamp (default: such meta-only re-renders are skipped to avoid churn)")
	flag.BoolVar(&cfg.verbose, "verbose", false, "print one line per block (insert / rehash / rerender / ok) with file, line, kind, id, svg-path")
	flag.BoolVar(&cfg.quiet, "quiet", false, "suppress non-error logs (implies !verbose)")
	flag.StringVar(&cfg.in, "in", "", "process only this single file (default: walk --root for .md files)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [--root <dir>] [--svg-dir _ipm] [--check] [--no-prune] [--dry-run] [--force] [--in <file.md>]\n", toolName)
		fmt.Fprintln(os.Stderr, "\nExit codes:")
		fmt.Fprintln(os.Stderr, "  0  no changes needed (or all changes applied successfully)")
		fmt.Fprintln(os.Stderr, "  1  --check found stale or missing markers / SVGs")
		fmt.Fprintln(os.Stderr, "  2  read / parse / render error")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()

	cfg.prune = !cfg.noPrune

	// Configure structured logging early so anything from now on (including
	// the warnIfSVGDirGitIgnored warning below) goes through pkg/ipm/log.
	iplog.SetupCLI(cfg.verbose, cfg.quiet)

	if cfg.check && cfg.dryRun {
		fmt.Fprintln(os.Stderr, toolName+": --check implies dry-run; do not combine")
		os.Exit(2)
	}
	if cfg.check && cfg.force {
		fmt.Fprintln(os.Stderr, toolName+": --force re-renders everything, which defeats --check; do not combine")
		os.Exit(2)
	}

	// Which flags did the caller set on the command line? Used to decide
	// whether config-file values should fill in defaults (CLI always wins).
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	// Auto-detect repo root when --root was not given. The detection walks
	// up from cwd looking for `.git` or `.ipm.conf`, so running the
	// tool from any subdirectory of a repo Just Works.
	if !explicit["root"] {
		cwd, err := os.Getwd()
		if err != nil {
			fatalf("get cwd: %v", err)
		}
		cfg.root = ipconfig.FindRepoRoot(cwd)
	}

	rootAbs, err := filepath.Abs(cfg.root)
	if err != nil {
		fatalf("resolve --root: %v", err)
	}
	cfg.root = rootAbs

	// Load .ipm.conf, searching upward from --root to the repo root (the same
	// discovery the LSP uses), and fill in flag defaults for anything the
	// caller didn't pass on the command line. embed-ignore patterns are matched
	// relative to the directory the config was found in.
	cfg.confRoot = ipconfig.FindRepoRoot(cfg.root)
	if fileCfg, present, err := ipconfig.Load(cfg.confRoot); err != nil {
		fatalf("load config: %v", err)
	} else if present {
		if !cfg.quiet {
			fmt.Printf("loaded %s\n", relTo(cfg.root, filepath.Join(cfg.confRoot, ipconfig.FileName)))
		}
		if fileCfg.SVGDir != nil && !explicit["svg-dir"] {
			cfg.svgDir = *fileCfg.SVGDir
		}
		cfg.ignore = fileCfg.EmbedIgnore
	}

	files, err := collectFiles(cfg)
	if err != nil {
		fatalf("collect files: %v", err)
	}
	files, skipped := filterIgnored(cfg, files)
	if len(skipped) > 0 && !cfg.quiet {
		fmt.Printf("embed-ignore: skipped %d file(s): %s\n", len(skipped), strings.Join(skipped, ", "))
	}
	if len(files) == 0 {
		if !cfg.quiet {
			fmt.Println("no .md files found")
		}
		return
	}

	warnIfSVGDirGitIgnored(rootAbs, cfg.svgDir)

	keepSVG := map[string]struct{}{}
	stats := runStats{}
	staleFound := false

	for _, mdAbs := range files {
		fr, err := processFile(cfg, mdAbs)
		if err != nil {
			fatalf("process %s: %v", relTo(rootAbs, mdAbs), err)
		}
		stats.files++
		stats.blocks += fr.blocks
		stats.insertMarker += fr.insertMarker
		stats.rehash += fr.rehash
		stats.rerender += fr.rerender
		stats.metaSkipped += fr.metaSkipped
		stats.unterminated += fr.unterminated
		stats.malformed += fr.malformed
		stats.missingSrc += fr.missingSrc
		stats.noEmbed += fr.noEmbed
		stats.badMeta += fr.badMeta
		stats.cleanups += fr.cleanups
		for p := range fr.svgsTouched {
			keepSVG[p] = struct{}{}
		}
		if fr.staleCount() > 0 {
			staleFound = true
			if cfg.check && !cfg.quiet {
				rel := relTo(rootAbs, mdAbs)
				fmt.Printf("STALE %s (insert=%d rehash=%d rerender=%d missing-src=%d cleanup=%d)\n",
					rel, fr.insertMarker, fr.rehash, fr.rerender, fr.missingSrc, fr.cleanups)
			}
		}
	}

	// Prune only when we walked the whole tree. With --in <file> we processed a
	// single file, so keepSVG covers only that file's SVGs; pruning would treat
	// every OTHER file's SVGs as orphans and delete them. Skip it (and say so)
	// rather than destroy unrelated output.
	if cfg.prune && cfg.in != "" && !cfg.check && !cfg.dryRun && !cfg.quiet {
		fmt.Println("note: --in single-file mode — skipping prune (cannot see other files' SVGs)")
	}
	if cfg.prune && cfg.in == "" && !cfg.check && !cfg.dryRun {
		removed, err := pruneOrphans(cfg.root, cfg.svgDir, keepSVG)
		if err != nil {
			fatalf("prune: %v", err)
		}
		stats.prunedOrphans = removed
	}

	if !cfg.quiet {
		fmt.Printf("done: files=%d blocks=%d insert=%d rehash=%d rerender=%d meta-skipped=%d cleanup=%d unterminated=%d malformed=%d missing-src=%d no-embed=%d bad-meta=%d pruned=%d\n",
			stats.files, stats.blocks, stats.insertMarker, stats.rehash, stats.rerender, stats.metaSkipped,
			stats.cleanups, stats.unterminated, stats.malformed, stats.missingSrc, stats.noEmbed, stats.badMeta, stats.prunedOrphans)
	}

	if cfg.check && staleFound {
		os.Exit(1)
	}
}

type fileResult struct {
	blocks       int
	insertMarker int
	rehash       int
	rerender     int
	metaSkipped  int // re-rendered but only the generated-by stamp drifted; left as-is
	unterminated int
	malformed    int
	missingSrc   int
	noEmbed      int // embed=false: valid but illustrative, not rendered
	badMeta      int // invalid # ipmt: pragma / fence token
	cleanups     int // stale blank lines that would be removed on apply
	svgsTouched  map[string]struct{}
}

// staleCount counts conditions that should cause --check to exit non-zero.
// "Stale" here covers both "the file needs an idempotent rewrite" (insert /
// rehash / rerender / cleanups) and "the file is structurally broken in a
// way the user must fix" (missing src= file, two blocks with the same id=).
func (r fileResult) staleCount() int {
	return r.insertMarker + r.rehash + r.rerender + r.missingSrc + r.badMeta + r.cleanups
}

func processFile(cfg config, mdAbs string) (fileResult, error) {
	out := fileResult{svgsTouched: map[string]struct{}{}}

	rawBytes, err := os.ReadFile(mdAbs)
	if err != nil {
		return out, err
	}
	text := string(rawBytes)

	analysis, err := mdembed.AnalyzeMarkdown(mdAbs, text, mdembed.AnalyzeOptions{
		Root:   cfg.root,
		SVGDir: cfg.svgDir,
	})
	if err != nil {
		return out, err
	}
	out.blocks = len(analysis.Blocks)
	if !analysis.HasIPMT() {
		return out, nil
	}

	// For each block, refine the outcome by consulting the SVG file on disk
	// (the analyzer can't see it). And in non-check / non-dry-run mode,
	// render + write.
	for i, br := range analysis.Blocks {
		switch br.Outcome {
		case mdembed.OutcomeUnterminated:
			out.unterminated++
			fmt.Fprintf(os.Stderr, "%s: block %d has unterminated ```ipmt fence (line %d)\n",
				relTo(cfg.root, mdAbs), br.Index, br.AnchorLine+1)
			continue
		case mdembed.OutcomeMalformed:
			out.malformed++
			fmt.Fprintf(os.Stderr, "%s: block %d %s (line %d)\n",
				relTo(cfg.root, mdAbs), br.Index, br.SkipReason, br.AnchorLine+1)
			continue
		case mdembed.OutcomeMissingSrc:
			out.missingSrc++
			fmt.Fprintf(os.Stderr, "%s: block %d %s (line %d)\n",
				relTo(cfg.root, mdAbs), br.Index, br.SkipReason, br.AnchorLine+1)
			continue
		case mdembed.OutcomeNoEmbed:
			// embed=false: valid but illustrative — skip silently (not an error).
			out.noEmbed++
			continue
		case mdembed.OutcomeBadMeta:
			out.badMeta++
			fmt.Fprintf(os.Stderr, "%s: block %d invalid ipmt metadata: %s (line %d)\n",
				relTo(cfg.root, mdAbs), br.Index, br.SkipReason, br.AnchorLine+1)
			continue
		}
		// Track which SVG file is "expected" for this block, so a later --prune
		// keeps it regardless of whether we render now.
		out.svgsTouched[br.SVGPath] = struct{}{}

		fresh, err := mdembed.SVGIsFresh(br.SVGPath, br.SrcHash)
		if err != nil {
			return out, fmt.Errorf("inspect svg %s: %w", br.SVGPath, err)
		}
		if br.Outcome == mdembed.OutcomeOK && (!fresh || cfg.force) {
			// Marker is current but the SVG file is missing or out of date —
			// or --force was given to re-render regardless of freshness.
			br.Outcome = mdembed.OutcomeRerender
			analysis.Blocks[i] = br
		}

		switch br.Outcome {
		case mdembed.OutcomeOK:
			// nothing to do
		case mdembed.OutcomeInsertMarker:
			out.insertMarker++
		case mdembed.OutcomeRehash:
			out.rehash++
		case mdembed.OutcomeRerender:
			out.rerender++
		}

		if cfg.verbose && !cfg.quiet {
			printBlockLine(cfg.root, mdAbs, br)
		}

		if cfg.check || cfg.dryRun {
			continue
		}
		if br.Outcome == mdembed.OutcomeOK {
			continue
		}
		wrote, err := mdembed.RenderAndWriteSVG(cfg.root, analysis.Path, br,
			fmt.Sprintf("%s@%s", toolName, toolVersion), cfg.forceMeta)
		if err != nil {
			return out, fmt.Errorf("render block %d: %w", br.Index, err)
		}
		if !wrote {
			// Re-rendered, but the only drift from the on-disk SVG is the
			// generated-by provenance — left untouched. Reclassify the tally
			// from rerender to meta-skipped.
			out.rerender--
			out.metaSkipped++
		}
	}

	// Always run ApplyMarkers, even when all blocks are OK — it also performs
	// the stale-blank cleanup pass that mutates the .md without changing any
	// marker text.
	newLines := mdembed.ApplyMarkers(analysis.Lines, analysis.Blocks)
	newText := strings.Join(newLines, "\n")
	if newText != text && out.insertMarker == 0 && out.rehash == 0 && out.rerender == 0 {
		// The only changes are whitespace cleanups.
		out.cleanups = 1
	}
	if cfg.check || cfg.dryRun {
		return out, nil
	}
	if newText == text {
		return out, nil
	}
	if err := os.WriteFile(mdAbs, []byte(newText), 0o644); err != nil {
		return out, err
	}
	if !cfg.quiet {
		fmt.Printf("updated %s\n", relTo(cfg.root, mdAbs))
	}
	return out, nil
}

// printBlockLine emits one line per block in --verbose runs. Format:
//
//	OUTCOME  rel/path.md:LINE  kind  id=ID  → rel/svg/path
//
// Lines are aligned-ish but not strictly tabular — they're meant to be
// readable in a scrolling terminal, not parsed.
func printBlockLine(rootAbs, mdAbs string, br mdembed.BlockResult) {
	rel := relTo(rootAbs, mdAbs)
	svgRel := relTo(rootAbs, br.SVGPath)
	id := br.NewMarker.ID
	if id == "" {
		id = br.OldMarker.ID
	}
	switch br.Outcome {
	case mdembed.OutcomeOK:
		fmt.Printf("OK       %s:%d  %s  id=%s\n", rel, br.AnchorLine+1, br.Kind, id)
	case mdembed.OutcomeInsertMarker:
		fmt.Printf("INSERT   %s:%d  %s  id=%s  → %s\n", rel, br.AnchorLine+1, br.Kind, id, svgRel)
	case mdembed.OutcomeRehash:
		fmt.Printf("REHASH   %s:%d  %s  id=%s  (hash %s → %s)\n",
			rel, br.MarkerLine+1, br.Kind, id, br.OldMarker.Hash, br.NewMarker.Hash)
	case mdembed.OutcomeRerender:
		fmt.Printf("RERENDER %s:%d  %s  id=%s  → %s\n", rel, br.AnchorLine+1, br.Kind, id, svgRel)
	}
}

// filterIgnored drops files whose path (relative to cfg.confRoot, where
// .ipm.conf was found) matches an embed-ignore glob. Returns the kept files
// and the --root-relative paths of the skipped ones, for reporting. Applies to
// both the directory walk and an explicit --in target.
func filterIgnored(cfg config, files []string) (kept, skipped []string) {
	if len(cfg.ignore) == 0 {
		return files, nil
	}
	c := ipconfig.Config{EmbedIgnore: cfg.ignore}
	for _, f := range files {
		if rel, err := filepath.Rel(cfg.confRoot, f); err == nil && c.IsIgnored(rel) {
			skipped = append(skipped, relTo(cfg.root, f))
			continue
		}
		kept = append(kept, f)
	}
	return kept, skipped
}

func collectFiles(cfg config) ([]string, error) {
	if cfg.in != "" {
		abs, err := filepath.Abs(cfg.in)
		if err != nil {
			return nil, err
		}
		return []string{abs}, nil
	}
	files := []string{}
	svgDirAbs := filepath.Join(cfg.root, cfg.svgDir)
	err := filepath.WalkDir(cfg.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			if path == svgDirAbs {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func pruneOrphans(rootAbs, svgDir string, keep map[string]struct{}) (int, error) {
	base := filepath.Join(rootAbs, svgDir)
	info, err := os.Stat(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("--svg-dir %s is not a directory", base)
	}
	removed := 0
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".svg") {
			return nil
		}
		if _, ok := keep[path]; ok {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, err
	}
	// Remove any now-empty subdirectories under base.
	if err := removeEmptyDirs(base); err != nil {
		return removed, err
	}
	return removed, nil
}

func removeEmptyDirs(root string) error {
	// Walk leaves up; collect dirs, then remove if empty.
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

func relTo(root, abs string) string {
	r, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(r)
}

// warnIfSVGDirGitIgnored runs `git check-ignore` on the SVG output directory
// and emits a warning via pkg/ipm/log if git would ignore it. Best-effort:
// silent failures when git isn't installed or the tree isn't a git repo.
//
// Per an earlier design note: a user who
// .gitignores the SVG dir would end up with rendered diagrams that never get
// committed, so GitHub renders the .md without the images. Flagging this
// once at the start of every run catches the silent failure mode early.
//
// --quiet need not be threaded in: iplog.SetupCLI already dropped the level to
// ERROR for --quiet, so the Warn() call below is silently filtered out.
func warnIfSVGDirGitIgnored(rootAbs, svgDir string) {
	// `git check-ignore` exits 0 if a path is ignored, 1 if not ignored,
	// 128 if not a git repo, 127 (or similar) if git isn't on PATH.
	// We only care about 0 (ignored).
	relSVG := svgDir
	if !strings.HasSuffix(relSVG, "/") {
		relSVG += "/"
	}
	cmd := exec.Command("git", "check-ignore", "-q", relSVG)
	cmd.Dir = rootAbs
	if err := cmd.Run(); err != nil {
		// Not ignored, not a git repo, git missing, or another error — all
		// silent. The only "true" signal we care about is exit-zero, below.
		return
	}
	iplog.Default().Warn(
		"svg output dir is git-ignored; rendered diagrams won't appear on GitHub",
		"svg-dir", svgDir,
		"hint", "remove the ignore rule, or set svg-dir in .ipm.conf to a non-ignored path",
	)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, toolName+": "+format+"\n", args...)
	os.Exit(2)
}
