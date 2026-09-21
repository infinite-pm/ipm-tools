// Package refs is the side-car that answers "where did this come from" for a
// graph: for every node and edge, the references it was made from — a fragment
// of ipmt at a line, a source line at a pinned version, a log line in a run, a
// coverage block, an ontology entry, a tool's candidate with its evidence and
// confidence, a person's curated mark, the merge that joined it to the same
// name elsewhere — and the build record that says which tool made the graph,
// from which inputs, when.
//
// One shape for every producer. A cut of a graph carries its refs re-keyed
// through the cut's id map; a union of graphs carries each source's refs under
// the union's ids, plus one ref per merge. The file sits beside the graph as
// <graph>.refs.json.
package refs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

// Version of the side-car format.
const Version = "1"

// The kinds a ref can be. New kinds are added here, with a sentence.
const (
	KindFragment   = "fragment"    // an ipmt block: repo:path#Lnn
	KindSourceLine = "source-line" // a line of source at a pinned version: module@version/path:line
	KindSource     = "source"      // a source artefact without a line: a function, a file, a package
	KindLoop       = "loop"        // a loop the scanner found: module@version/path#func#Ln, with its body hash in Hash
	KindLogLine    = "log-line"    // a captured log line of a run: dataset path + the message
	KindLogRef     = "log-ref"     // the source site a log line bound to (golog-refs, by message)
	KindCoverage   = "coverage"    // a coverage count: the block that ran, how often
	KindCandidate  = "candidate"   // a tool's proposal: Why is the evidence, Conf the confidence, Signal the rung
	KindCurated    = "curated"     // a person's mark on a candidate: Mark is [x] or [ ]
	KindDocRef     = "docref"      // a documentation URL the ontology or a flow cites
	KindGlossary   = "glossary"    // the docs' own concept markup: a glossary term id at a place in a page
	KindOntology   = "ontology"    // an ontology entry: config path # concept id
	KindFlowStep   = "flow-step"   // a step of a documented flow: config path # flow / step
	KindUnionMerge = "union-merge" // the same name in another fragment, merged into this node
	KindCut        = "cut"         // this node is a copy from a bigger graph: At names it, the ids map through
	KindPage       = "page"        // a docs page section a thing was read from
)

// Ref is one reference. At is a locator in one of the ecosystem's three forms:
// repo:path#Lnn for a file in a repository, module@version/path:line for pinned
// source, or a URL. By says who: "author" (a person wrote the source), a tool
// ("tool:loopmap"), or "person" (a curated decision).
type Ref struct {
	Kind   string  `json:"kind"`
	At     string  `json:"at"`
	By     string  `json:"by,omitempty"`
	Why    string  `json:"why,omitempty"`    // evidence, in words
	Signal string  `json:"signal,omitempty"` // a candidate's rung
	Conf   float64 `json:"conf,omitempty"`   // a candidate's confidence
	Mark   string  `json:"mark,omitempty"`   // a curated decision
	Hash   string  `json:"hash,omitempty"`   // a loop's body hash
	Count  int     `json:"count,omitempty"`  // a coverage count
}

// Input is one input of a build, with its content hash.
type Input struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
}

// Build is the audit record: which tool made the artefact, from what, when.
type Build struct {
	Tool   string   `json:"tool"`
	Commit string   `json:"commit,omitempty"` // the tool's own VCS revision, when the binary carries it
	Args   []string `json:"args,omitempty"`
	Inputs []Input  `json:"inputs,omitempty"`
	At     string   `json:"at"`               // RFC 3339
	Parent string   `json:"parent,omitempty"` // the refs file of the artefact this was made from
}

// Refs is the side-car.
type Refs struct {
	Version string        `json:"version"`
	Build   Build         `json:"build"`
	Nodes   map[int][]Ref `json:"nodes"`
	Edges   map[int][]Ref `json:"edges"`
}

// New starts a side-car for a tool. Args are the command's, inputs are hashed
// now (a missing file gets no hash rather than an error: the record is about
// what was used, and a path that was not there says so by its absence).
func New(tool string, args []string, inputs ...string) *Refs {
	r := &Refs{Version: Version, Nodes: map[int][]Ref{}, Edges: map[int][]Ref{},
		Build: Build{Tool: tool, Args: args, At: time.Now().UTC().Format(time.RFC3339), Commit: vcsRevision()}}
	for _, p := range inputs {
		in := Input{Path: p}
		if data, err := os.ReadFile(p); err == nil {
			sum := sha256.Sum256(data)
			in.SHA256 = hex.EncodeToString(sum[:])
		}
		r.Build.Inputs = append(r.Build.Inputs, in)
	}
	return r
}

// Node records a ref for a node. A nil *Refs records nothing, so a builder
// that takes an optional side-car needs no branch.
func (r *Refs) Node(id int, ref Ref) {
	if r == nil || id == 0 {
		return
	}
	r.Nodes[id] = append(r.Nodes[id], ref)
}

// Edge records a ref for an edge. Nil-safe, as Node is.
func (r *Refs) Edge(id int, ref Ref) {
	if r == nil || id == 0 {
		return
	}
	r.Edges[id] = append(r.Edges[id], ref)
}

// Rekey carries refs across an id map (new → original), as a cut does: the
// piece's node ids look up the whole's refs. Edges get a fresh map; a caller
// re-records them if it knows them. The cut itself is noted on every node.
func (r *Refs) Rekey(toOrig map[int]int, from string, build Build) *Refs {
	out := &Refs{Version: Version, Build: build, Nodes: map[int][]Ref{}, Edges: map[int][]Ref{}}
	for newID, orig := range toOrig {
		out.Nodes[newID] = append(append([]Ref(nil), r.Nodes[orig]...), Ref{Kind: KindCut, At: from, Why: "node " + itoa(orig) + " of the whole"})
	}
	return out
}

// Write saves the side-car, sorted and indented.
func (r *Refs) Write(path string) error {
	data, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Read loads a side-car.
func Read(path string) (*Refs, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Refs
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.Nodes == nil {
		r.Nodes = map[int][]Ref{}
	}
	if r.Edges == nil {
		r.Edges = map[int][]Ref{}
	}
	return &r, nil
}

// Path is the side-car's path for a graph file: <graph>.refs.json, with a
// .json suffix replaced rather than doubled.
func Path(graphPath string) string {
	return strings.TrimSuffix(graphPath, ".json") + ".refs.json"
}

// Kinds lists the kinds present, for a report.
func (r *Refs) Kinds() []string {
	set := map[string]bool{}
	for _, rs := range r.Nodes {
		for _, x := range rs {
			set[x.Kind] = true
		}
	}
	for _, rs := range r.Edges {
		for _, x := range rs {
			set[x.Kind] = true
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	// built from a tree with changes the commit does not hold: the record
	// says so, or it names a revision the graph was not made by
	if rev != "" && dirty {
		rev += "+dirty"
	}
	return rev
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
