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
	"bytes"
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
	// Hash is the source's content hash. A report is a snapshot of a MOVING
	// target — the corpus it swept changes underneath it — and this is what
	// lets a later run say which diagrams were added, dropped or edited since.
	Hash string
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
	return CollectExcluding(root, paths, srcDir, nil)
}

// CollectExcluding is Collect with the corpus's outlier filter (Corpus.
// Outlier): a diagram the filter names is left out and reported in the
// warnings as "outlier skipped: <id> — <why>", so a sweep never silently
// narrows.
func CollectExcluding(root string, paths []string, srcDir string, exclude func(path string) (string, bool)) ([]Diagram, []string, error) {
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
		if exclude != nil {
			if why, skip := exclude(rel); skip {
				warns = append(warns, fmt.Sprintf("outlier skipped: %s — %s", rel, why))
				continue
			}
		}
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
// Fingerprint identifies a whole diagram set: which diagrams, at which
// contents. Two runs with the same fingerprint swept the same corpus.
func Fingerprint(ds []Diagram) map[string]string {
	out := make(map[string]string, len(ds))
	for _, d := range ds {
		out[d.ID] = d.Hash
	}
	return out
}

// DiffSets says how a corpus moved between two runs: what was added, what
// went away, and what was edited in place. The last is the one that matters —
// an edited diagram makes every earlier column's picture of it a picture of
// something else.
func DiffSets(was, now map[string]string) (added, removed, edited []string) {
	for id, h := range now {
		old, ok := was[id]
		switch {
		case !ok:
			added = append(added, id)
		case old != h:
			edited = append(edited, id)
		}
	}
	for id := range was {
		if _, ok := now[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(edited)
	return added, removed, edited
}

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
		d.Hash = sum[:12]
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
	analysis, err := mdembed.AnalyzeMarkdown(mdPath, string(text),
		mdembed.AnalyzeOptions{Root: analysisRoot(root, mdPath)})
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

// analysisRoot is the root md-embed should read a file against.
//
// Usually the corpus root — but a corpus may name SIBLING CHECKOUTS, and
// md-embed refuses a file outside its root ("is outside root"). Every diagram
// in every sibling repository was silently skipped that way, the report simply
// reporting a smaller corpus than asked for.
//
// An external file is read against ITS OWN repository, which is also how
// md-embed would run there: the root decides where _ipm/ artifacts and
// relative references resolve, and a foreign repo's answers are its own.
func analysisRoot(root, mdPath string) string {
	if rel, err := filepath.Rel(root, mdPath); err == nil && !strings.HasPrefix(rel, "..") {
		return root
	}
	dir := filepath.Dir(mdPath)
	if root := RepoRootOf(mdPath); root != "" {
		return root
	}
	return dir // no repository above it; the file's own directory will do
}

// RepoRootOf is the nearest ancestor of path holding a .git, or "".
func RepoRootOf(path string) string {
	at := filepath.Dir(path)
	for {
		if _, err := os.Stat(filepath.Join(at, ".git")); err == nil {
			return at
		}
		up := filepath.Dir(at)
		if up == at {
			return ""
		}
		at = up
	}
}

// RepoRelative names a file the way its OWN repository does.
//
// A diagram's id is relative to the corpus root, which for a sibling checkout
// means "../<other>/docs/x.md" — correct as an identity, useless as a
// location. Nobody works in the corpus root when they open that file; they
// work in that repository, where it is "docs/x.md". So a location is resolved
// against the first .git above the file, whichever repository that is.
func RepoRelative(path string) string {
	root := RepoRootOf(path)
	if root == "" {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// relTo names a diagram.
//
// Inside the corpus root, that is the path as its own repository writes it:
// "docs/x.md". Outside it — a sibling checkout, which is a first-class corpus
// here since the engine is this repo's and the diagrams can come from anywhere
// — the name is "repo:path", as in "other-repo:docs/notes.md".
//
// Not "../<other>/docs/...": a "../" path is only meaningful from one
// directory, and it is not the directory anyone opens the file in. The repo
// name plus the path THAT repo uses is true from anywhere, and reads as what
// it is. An absolute path would be true too, and would put the whole machine
// into every id, page name and row.
//
// It stays unique: one repository name, one path within it.
func relTo(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	if repo := RepoRootOf(p); repo != "" {
		if rel, err := filepath.Rel(repo, p); err == nil {
			return filepath.Base(repo) + ":" + filepath.ToSlash(rel)
		}
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
	forEachDiagram(len(diagrams), workers, func(i int) {
		d := diagrams[i]
		pairs[i] = Pair{Diagram: d, Old: RunEngine(oldBin, d), New: RunEngine(newBin, d)}
	})
	return pairs
}

// SweepOne runs ONE engine over every diagram.
//
// A timeline compares CONSECUTIVE engines, so engine k is the "new" side of its
// own column and the "old" side of the next. Sweeping a pair at a time ran
// every engine over the whole corpus twice — about half of all the work in a
// run, spent re-deriving an answer computed one column earlier. Keeping the
// result instead needs no cache and no key: it is the same binary over the same
// bytes, inside one process, and RunEngine holds no state between calls.
func SweepOne(diagrams []Diagram, bin string, workers int) []Generated {
	out := make([]Generated, len(diagrams))
	forEachDiagram(len(diagrams), workers, func(i int) {
		out[i] = RunEngine(bin, diagrams[i])
	})
	return out
}

// PairUp joins two sweeps of the SAME diagram list into the pairs a report
// reads. Index i is diagrams[i] in both, because both came from that slice.
func PairUp(diagrams []Diagram, old, next []Generated) []Pair {
	pairs := make([]Pair, len(diagrams))
	for i := range diagrams {
		p := Pair{Diagram: diagrams[i]}
		if i < len(old) {
			p.Old = old[i]
		}
		if i < len(next) {
			p.New = next[i]
		}
		pairs[i] = p
	}
	return pairs
}

// forEachDiagram runs fn over every index, on a bounded pool. One process per
// diagram is deliberate — see the note at the top of this file on crash
// isolation — so the pool bounds how many exist at once.
func forEachDiagram(n, workers int, fn func(int)) {
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
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

// Deterministic reports whether an engine gives the same answer twice for one
// diagram.
//
// The whole premise of a timeline is that a cell means THE ENGINE changed the
// picture. An engine that does not agree with itself breaks that silently: its
// columns report a different set of moved diagrams on every run, and nothing
// in the report says so. Some genuinely do not — the 2025 `25.09-layout-v2`
// layout-gen returns four distinct outputs in five runs on one input, from map
// iteration order — and that is a fact about the history, unfixable now, which
// makes saying it the only honest option.
//
// Two executions of one diagram, so the probe costs nothing worth measuring.
func Deterministic(bin string, d Diagram) bool {
	a := RunEngine(bin, d)
	if a.Err != "" || len(a.JSON) == 0 {
		return true // nothing to disagree about; a failure is reported elsewhere
	}
	b := RunEngine(bin, d)
	return bytes.Equal(a.JSON, b.JSON)
}
