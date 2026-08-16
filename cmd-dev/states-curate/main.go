// Command states-curate turns a raw ipm-rpc-tee capture into a corpus:
// deterministic, content-addressed ipmt files plus the two artifacts that
// make them a regression gate.
//
//	states-curate --in out/states --out demo/states
//
// The raw capture is a recording — timestamped, duplicated, ordered by wall
// clock. A corpus has to be the opposite: stable under re-capture, so that a
// git diff shows what genuinely changed rather than the fact that a take was
// re-run. So each kept state is named by the HASH OF ITS SOURCE, which means
// a re-capture that types the same thing produces the same filenames and no
// diff at all.
//
// Three things come out per scene:
//
//   - <scene>/<hash>.ipmt         — states the engine lays out
//   - <scene>/invalid/<hash>.ipmt — states it rejects, with the server's own
//     diagnostic recorded in the manifest (a parser/validator corpus, and a
//     "never panic" gate; they are NOT layout material)
//   - <scene>/signatures.txt      — one line per state: source hash, layout
//     hash, size, bounds. THE TRIPWIRE: regenerate, `git diff`, and every
//     state whose layout moved is named. No old engine needed, no geometry
//     stored. Same discipline as the corpora's _refs.json output maps.
//   - <scene>/manifest.json       — typing order, cadence and provenance,
//     which the content-addressed filenames deliberately do not carry.
//
// gl:docs/dev-tools/states-corpus.md
package main

import (
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layout7"
	"github.com/infinite-pm/ipm-tools/pkg/mdembed"
)

const toolName = "states-curate"

// rawState mirrors ipm-rpc-tee's capture line.
type rawState struct {
	Seq         int      `json:"seq"`
	TS          string   `json:"ts"`
	Cadence     string   `json:"cadence"`
	URI         string   `json:"uri"`
	Lang        string   `json:"lang,omitempty"`
	Version     int      `json:"version,omitempty"`
	Text        string   `json:"text,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// entry is one curated state.
type entry struct {
	Order   int    `json:"order"`
	File    string `json:"file"`
	Hash    string `json:"hash"`
	Cadence string `json:"cadence"`
	Source  string `json:"source"`          // repo-relative origin, e.g. "life.md#100"
	Block   string `json:"block,omitempty"` // marker id for a markdown block
	Status  string `json:"status"`          // laid-out | rejected
	Error   string `json:"error,omitempty"` // parse/validate message
	Layout  string `json:"layoutHash,omitempty"`
	Nodes   int    `json:"nodes,omitempty"`
	Edges   int    `json:"edges,omitempty"`
	Bounds  string `json:"bounds,omitempty"`
	// Rendered marks the states the engine was actually asked to lay out
	// during the take (a full ipm.embedBuffer followed this state), as
	// opposed to the ones the typist merely passed through.
	Rendered bool `json:"rendered,omitempty"`
}

func main() {
	os.Exit(run())
}

func run() int {
	var in, out string
	var all, quiet bool
	flag.StringVar(&in, "in", "out/states", "directory of raw ipm-rpc-tee captures (*.jsonl)")
	flag.StringVar(&out, "out", "demo/states", "corpus directory to write (each scene's directory is replaced)")
	flag.BoolVar(&all, "all", false, "keep every distinct SOURCE; by default a state whose layout is identical to one already kept is dropped")
	flag.BoolVar(&quiet, "quiet", false, "only print the summary")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s --in <raw capture dir> --out <corpus dir> [--all]\n", toolName)
		fmt.Fprintln(os.Stderr, "\nTurns ipm-rpc-tee captures into a content-addressed ipmt corpus")
		fmt.Fprintln(os.Stderr, "with signatures.txt (the regression tripwire) and manifest.json.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	return curate(in, out, all, quiet)
}

// curate is the whole job, minus flag parsing: raw capture directory in,
// corpus directory out. Separated so the tests drive the real thing rather
// than a re-implementation of it.
func curate(in, out string, all bool, quiet ...bool) int {
	silent := len(quiet) > 0 && quiet[0]
	raw, err := readCaptures(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", toolName, err)
		return 2
	}
	if len(raw) == 0 {
		fmt.Fprintf(os.Stderr, "%s: no capture lines under %s\n", toolName, in)
		return 1
	}

	scenes := groupByScene(raw)
	names := make([]string, 0, len(scenes))
	for name := range scenes {
		names = append(names, name)
	}
	sort.Strings(names)

	totalKept, totalInvalid := 0, 0
	for _, name := range names {
		kept, invalid, err := curateScene(filepath.Join(out, name), name, scenes[name], all)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s: %v\n", toolName, name, err)
			return 2
		}
		totalKept += kept
		totalInvalid += invalid
		if !silent {
			fmt.Printf("%-20s %4d states, %3d rejected\n", name, kept, invalid)
		}
	}
	fmt.Printf("%s: %d scene(s), %d states, %d rejected → %s\n",
		toolName, len(names), totalKept, totalInvalid, out)
	return 0
}

// readCaptures loads every *.jsonl under dir, in filename then sequence
// order, so a scene's states come back in the order they were typed.
func readCaptures(dir string) ([]rawState, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)

	var out []rawState
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var s rawState
			if err := json.Unmarshal([]byte(line), &s); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %s:%d: %v\n", toolName, f, i+1, err)
				continue
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// groupByScene splits states by the workspace they belong to.
//
// A demo scene runs in its own throwaway workspace (out/workspaces/<scene>/),
// so the URI already says which scene a state came from and nothing has to be
// told or threaded through the recorder.
func groupByScene(raw []rawState) map[string][]rawState {
	out := map[string][]rawState{}
	for _, s := range raw {
		out[sceneOf(s.URI)] = append(out[sceneOf(s.URI)], s)
	}
	return out
}

func sceneOf(uri string) string {
	p := pathOf(uri)
	parts := strings.Split(filepath.ToSlash(filepath.Dir(p)), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "workspaces" || parts[i] == "workspace" {
			return parts[i+1]
		}
	}
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return "unknown"
}

func pathOf(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		return u.Path
	}
	return uri
}

// curateScene writes one scene's corpus directory and returns the counts.
func curateScene(dir, scene string, states []rawState, keepAll bool) (kept, invalid int, err error) {
	// The directory is REPLACED: a state that is no longer typed must
	// disappear, or the corpus accumulates diagrams no scene produces.
	if err := os.RemoveAll(dir); err != nil {
		return 0, 0, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "invalid"), 0o755); err != nil {
		return 0, 0, err
	}

	sort.SliceStable(states, func(i, j int) bool { return states[i].Seq < states[j].Seq })

	// Buffers by path, so a markdown include can be resolved from what was in
	// the editor rather than from a file that may not exist on this machine.
	buffers := map[string][]byte{}
	for _, s := range states {
		if s.Text != "" {
			buffers[pathOf(s.URI)] = []byte(s.Text)
		}
	}

	var entries []entry
	seenSource := map[string]bool{}
	seenLayout := map[string]string{}
	order := 0

	for i, s := range states {
		if s.Text == "" {
			continue // a marker line (embed / save / diagnostics), not a state
		}
		for _, src := range sourcesOf(s, buffers) {
			hash := shortHash([]byte(src.text))
			if seenSource[hash] {
				continue
			}
			seenSource[hash] = true

			e := entry{
				Hash: hash, Cadence: s.Cadence, Source: src.name, Block: src.block,
				Rendered: renderedAfter(states, i, s.URI),
			}
			g, lerr := generate(src.text)
			switch {
			case lerr != nil:
				e.Status = "rejected"
				e.Error = firstLine(lerr.Error())
				e.File = filepath.Join("invalid", hash+".ipmt")
				invalid++
			default:
				sig := shortHash(mustJSON(g))
				if !keepAll {
					if prev, ok := seenLayout[sig]; ok {
						// Same layout as a state already kept: nothing a
						// layout corpus can learn from it.
						_ = prev
						continue
					}
				}
				seenLayout[sig] = hash
				e.Status = "laid-out"
				e.Layout = sig
				e.Nodes, e.Edges = len(g.Nodes), len(g.Edges)
				e.Bounds = fmt.Sprintf("%dx%d", g.Meta.Bounds.Width, g.Meta.Bounds.Height)
				e.File = hash + ".ipmt"
				kept++
			}
			order++
			e.Order = order
			if err := os.WriteFile(filepath.Join(dir, e.File), []byte(src.text), 0o644); err != nil {
				return kept, invalid, err
			}
			entries = append(entries, e)
		}
	}

	if err := writeManifest(dir, scene, entries); err != nil {
		return kept, invalid, err
	}
	return kept, invalid, writeSignatures(dir, scene, entries)
}

// source is one ipmt document extracted from a buffer state.
type source struct {
	text  string
	name  string // "40.ipmt" or "life.md#100"
	block string
}

// sourcesOf turns a buffer into the ipmt document(s) the engine would see:
// a `.ipmt` buffer IS one, a `.md` buffer contributes every block md-embed
// would render — decided by pkg/mdembed, not re-derived here, so fence meta,
// pragmas, includes and the invalid lane behave exactly as they do in the
// editor that produced the capture.
func sourcesOf(s rawState, buffers map[string][]byte) []source {
	p := pathOf(s.URI)
	base := filepath.Base(p)
	if strings.HasSuffix(strings.ToLower(p), ".ipmt") {
		if strings.TrimSpace(s.Text) == "" {
			return nil
		}
		return []source{{text: s.Text, name: base}}
	}
	if !strings.HasSuffix(strings.ToLower(p), ".md") {
		return nil
	}
	analysis, err := mdembed.AnalyzeMarkdown(p, s.Text, mdembed.AnalyzeOptions{
		Root: filepath.Dir(p),
		SrcReader: func(abs string) ([]byte, error) {
			if b, ok := buffers[abs]; ok {
				return b, nil
			}
			return os.ReadFile(abs)
		},
	})
	if err != nil {
		return nil
	}
	var out []source
	for _, br := range analysis.Blocks {
		switch br.Outcome {
		case mdembed.OutcomeUnterminated, mdembed.OutcomeMalformed,
			mdembed.OutcomeMissingSrc, mdembed.OutcomeBadMeta, mdembed.OutcomeNoEmbed:
			continue
		}
		if strings.TrimSpace(br.Content) == "" {
			continue
		}
		id := br.NewMarker.ID
		if id == "" {
			id = fmt.Sprintf("L%d", br.OpenLine+1)
		}
		out = append(out, source{text: br.Content, name: base + "#" + id, block: id})
	}
	return out
}

// renderedAfter reports whether a full (non-tokensOnly) embedBuffer for this
// URI followed this state before the next edit — i.e. whether the layout
// engine actually ran on it during the take.
func renderedAfter(states []rawState, i int, uri string) bool {
	for j := i + 1; j < len(states); j++ {
		s := states[j]
		if s.URI != uri {
			continue
		}
		switch s.Cadence {
		case "embed", "save":
			return true
		case "change", "open":
			return false
		}
	}
	return false
}

func generate(src string) (*layout.Graph, error) {
	doc, err := parser.Parse([]byte(src), parser.Options{Filename: "state.ipmt"})
	if err != nil {
		return nil, err
	}
	return layout7.Generate(doc)
}

func writeManifest(dir, scene string, entries []entry) error {
	type manifest struct {
		Scene  string  `json:"scene"`
		Tool   string  `json:"tool"`
		States []entry `json:"states"`
	}
	data, err := json.MarshalIndent(manifest{Scene: scene, Tool: toolName, States: entries}, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o644)
}

// writeSignatures records what each state LAYS OUT TO, without storing the
// geometry: regenerate after an engine change and `git diff` names exactly
// the states that moved. Sorted by file so the diff is stable.
func writeSignatures(dir, scene string, entries []entry) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# layout signatures for scene %q — <state> <layout> <nodes> <edges> <bounds>\n", scene)
	b.WriteString("# regenerate: states-curate --in <capture> --out <corpus>\n")
	b.WriteString("# a changed layout hash means the engine moved that state; review it with\n")
	b.WriteString("# layout-audit (gl:docs/dev-tools/layout-audit.md), then commit as generated.\n")
	rows := make([]entry, 0, len(entries))
	for _, e := range entries {
		if e.Status == "laid-out" {
			rows = append(rows, e)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].File < rows[j].File })
	for _, e := range rows {
		fmt.Fprintf(&b, "%s %s %d %d %s\n", e.Hash, e.Layout, e.Nodes, e.Edges, e.Bounds)
	}
	return os.WriteFile(filepath.Join(dir, "signatures.txt"), []byte(b.String()), 0o644)
}

func shortHash(b []byte) string { return fmt.Sprintf("%x", sha1.Sum(b))[:12] }

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
