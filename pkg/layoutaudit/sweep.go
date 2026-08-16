package layoutaudit

// The diagram set, and running both engines over it.
//
// Both engines run as SUBPROCESSES, including the new one. The old side has
// to be a subprocess anyway; making the new side one too keeps the two
// symmetric (same loader, same flags, same failure modes), isolates a crash
// on one diagram into one row instead of the whole sweep, and keeps the audit
// binary from linking an engine it could then be accused of comparing against
// itself. It costs nothing measurable: a full corpus sweep is ~1s per engine.

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/mdembed"
)

// Diagram is one thing to compare: an ipmt source with an identity a human
// recognises and a tool can grep for.
type Diagram struct {
	// ID is repo-relative: "tests/layout-gen/foo.ipmt" for a file,
	// "docs/x.md#100" for a block (the id being mdembed's marker key, so the
	// row points at the same artifact `_ipm/docs/x/100.ipm.svg` does).
	ID string
	// Path is what the engines are handed. For a markdown block it is an
	// extracted file under the run's src/ directory.
	Path string
	// Origin is the file a human should open; equals Path for .ipmt inputs.
	Origin string
	Line   int // block position within a .md, 0 for a whole file
	// Aliases are the other places this exact source appears (see dedupe).
	Aliases []string
}

// skipDirs are never walked: generated output, dependencies, and the audit's
// own scratch.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "_ipm": true, "temp": true,
	"bin": true, "out": true, "dist": true,
}

// Collect enumerates diagrams under the given paths (files or directories).
// Markdown blocks are extracted with mdembed, so the set is exactly what
// md-embed would render — fence meta, `# ipmt:` pragmas, includes and the
// `embed=false` / invalid lanes all decided in one place rather than
// re-derived here.
func Collect(root string, paths []string, srcDir string) ([]Diagram, []string, error) {
	var out []Diagram
	var warns []string
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return nil, nil, err
	}

	var files []string
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, p)
		}
		info, err := os.Stat(abs)
		if err != nil {
			warns = append(warns, fmt.Sprintf("skip %s: %v", p, err))
			continue
		}
		if !info.IsDir() {
			files = append(files, abs)
			continue
		}
		_ = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".ipmt") || strings.HasSuffix(path, ".md") {
				files = append(files, path)
			}
			return nil
		})
	}
	sort.Strings(files)

	for _, f := range files {
		rel := relTo(root, f)
		if strings.HasSuffix(f, ".ipmt") {
			out = append(out, Diagram{ID: rel, Path: f, Origin: f})
			continue
		}
		ds, w := blocksOf(root, f, rel, srcDir)
		out = append(out, ds...)
		warns = append(warns, w...)
	}
	return dedupe(out), warns, nil
}

// dedupe collapses diagrams whose SOURCE is byte-identical to one already
// collected, keeping the first (which is the `.ipmt` file, because the walk
// is sorted and `.ipmt` sorts before the `.md` that quotes it).
//
// The fixture corpora carry every case twice — `<case>.ipmt` and the
// generated `<case>.md` that embeds the same block — so without this every
// change is reported twice, and a reader has to work out that the two rows
// are one diagram. Identical source cannot lay out differently: the engine
// is deterministic. The dropped names are kept on the survivor so the row
// still says where else it appears.
func dedupe(in []Diagram) []Diagram {
	seen := map[string]int{} // content hash → index in out
	var out []Diagram
	for _, d := range in {
		data, err := os.ReadFile(d.Path)
		if err != nil {
			out = append(out, d)
			continue
		}
		sum := fmt.Sprintf("%x", sha1.Sum(data))
		if i, ok := seen[sum]; ok {
			out[i].Aliases = append(out[i].Aliases, d.ID)
			continue
		}
		seen[sum] = len(out)
		out = append(out, d)
	}
	return out
}

// blocksOf extracts every renderable ipmt block of one markdown file.
func blocksOf(root, mdPath, rel, srcDir string) ([]Diagram, []string) {
	text, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, []string{fmt.Sprintf("skip %s: %v", rel, err)}
	}
	analysis, err := mdembed.AnalyzeMarkdown(mdPath, string(text), mdembed.AnalyzeOptions{Root: root})
	if err != nil {
		return nil, []string{fmt.Sprintf("skip %s: analyze: %v", rel, err)}
	}
	var out []Diagram
	var warns []string
	for _, br := range analysis.Blocks {
		switch br.Outcome {
		case mdembed.OutcomeUnterminated, mdembed.OutcomeMalformed,
			mdembed.OutcomeMissingSrc, mdembed.OutcomeBadMeta, mdembed.OutcomeNoEmbed:
			continue // md-embed would not render it, so neither do we
		}
		if strings.TrimSpace(br.Content) == "" {
			continue
		}
		id := br.NewMarker.ID
		if id == "" {
			id = fmt.Sprintf("L%d", br.OpenLine+1)
		}
		path := filepath.Join(srcDir, Sanitize(rel+"#"+id)+".ipmt")
		if err := os.WriteFile(path, []byte(br.Content), 0o644); err != nil {
			warns = append(warns, fmt.Sprintf("skip %s#%s: %v", rel, id, err))
			continue
		}
		out = append(out, Diagram{
			ID: rel + "#" + id, Path: path, Origin: mdPath, Line: br.OpenLine + 1,
		})
	}
	return out, warns
}

// Sanitize turns a diagram identity into a filename fragment.
func Sanitize(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", "#", "-", " ", "_", ":", "-")
	return r.Replace(s)
}

func relTo(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}

// Generated is one engine's result for one diagram.
type Generated struct {
	Graph *layout.Graph
	Err   string
	JSON  []byte
}

// RunEngine generates one diagram's layout with one engine.
func RunEngine(bin string, d Diagram) Generated {
	cmd := exec.Command(bin, "--in", d.Path, "--out", "-", "--pretty=true")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Generated{Err: firstLine(msg)}
	}
	var g layout.Graph
	if err := json.Unmarshal(out, &g); err != nil {
		return Generated{Err: "decode layout json: " + err.Error()}
	}
	return Generated{Graph: &g, JSON: out}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Pair is two engines' results for one diagram.
type Pair struct {
	Diagram Diagram
	Old     Generated
	New     Generated
}

// Sweep runs every diagram through both engines with a default worker count.
func Sweep(diagrams []Diagram, oldBin, newBin string) []Pair {
	return SweepN(diagrams, oldBin, newBin, 0)
}

// SweepN is Sweep with an explicit worker count; 0 picks the default.
//
// Worth being able to turn down: a sweep spawns two processes per diagram, so
// a long history runs tens of thousands of them, and a machine that is also
// running an editor and a language server has other work to do.
func SweepN(diagrams []Diagram, oldBin, newBin string, workers int) []Pair {
	pairs := make([]Pair, len(diagrams))
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers > 8 {
			workers = 8
		}
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				d := diagrams[i]
				pairs[i] = Pair{Diagram: d, Old: RunEngine(oldBin, d), New: RunEngine(newBin, d)}
			}
		}()
	}
	for i := range diagrams {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return pairs
}
