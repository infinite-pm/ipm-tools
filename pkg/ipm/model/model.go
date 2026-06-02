package model

// NodeType is the kind of a graph node: Thing, Event, Concept, or Unresolved.
type NodeType string

const (
	Thing   NodeType = "Thing"
	Event   NodeType = "Event"
	Concept NodeType = "Concept"
	// Unresolved is a non-γ(3,4) kind for a node whose event/thing/concept kind
	// could not be determined. Importers may emit it instead of guessing Thing;
	// it renders neutral/grey. Edges touching it can fall outside the type-pair
	// table, so it marks a node to confirm rather than a settled model.
	Unresolved NodeType = "Unresolved"
)

// ArrowDir is an edge's direction: DirFwd (source→target) or DirUndir (no arrowhead).
type ArrowDir string

const (
	DirFwd   ArrowDir = "DirFwd"
	DirUndir ArrowDir = "DirUndir"
)

// SstLinkType is the semantic relation an edge carries (PartOf, LeadsTo,
// Expresses, or NearTo), as inferred from the endpoint kinds or stated explicitly.
type SstLinkType string

const (
	PartOf    SstLinkType = "PartOf"
	LeadsTo   SstLinkType = "LeadsTo"
	Expresses SstLinkType = "Expresses"
	NearTo    SstLinkType = "NearTo"
)

// EdgeSemOrigin records whether an edge's semantics were inferred from node
// kinds (SemInferred) or stated explicitly in the source (SemExplicit).
type EdgeSemOrigin string

const (
	SemInferred EdgeSemOrigin = "SemInferred"
	SemExplicit EdgeSemOrigin = "SemExplicit"
)

// Node is a single graph node with its kind, optional alias and tooltip.
type Node struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Alias string   `json:"alias"`
	Type  NodeType `json:"type"`
	// Candidates lists the possible kinds of an Unresolved node, ordered
	// most-likely-first. Candidates[0] is the primary (effective) kind used for
	// validation, layout, and rendering. Non-empty iff Type == Unresolved.
	Candidates []NodeType `json:"candidates,omitempty"`
	Tooltip    string     `json:"tooltip,omitempty"`
}

// Edge connects two nodes (by ID) with a direction and semantic link type.
type Edge struct {
	ID          int           `json:"id"`
	Source      int           `json:"source"`
	Target      int           `json:"target"`
	Dir         ArrowDir      `json:"dir"`
	Tooltip     string        `json:"tooltip"`
	SstLinkType SstLinkType   `json:"sstLinkType"`
	SemOrigin   EdgeSemOrigin `json:"semOrigin"`
}

// EdgePos holds the raw source byte-offset spans of an edge's arrow and its
// source/target endpoints.
type EdgePos struct {
	Arrow  [2]int `json:"arrow"`
	Source [2]int `json:"source"`
	Target [2]int `json:"target"`
}

// Positions maps node and edge IDs to their raw source byte-offset spans.
type Positions struct {
	Nodes map[int][2]int  `json:"nodes"`
	Edges map[int]EdgePos `json:"edges"`
}

// Src describes the source a graph was parsed from, including the original text
// and per-node/edge source positions.
type Src struct {
	Type      string    `json:"type"`
	Ipmt      string    `json:"ipmt"`
	Positions Positions `json:"positions"`
	// Lex carries transient source byte-offset spans for syntax lexemes the
	// parser recognizes but that aren't part of the graph structure
	// (comments, and — as the regex-removal work lands — markers, alias,
	// string, and tooltip text). It is json:"-": only the in-memory
	// tokenizer (pkg/ipmtokens, which always parses fresh) consumes it, so
	// it never affects serialized goldens.
	Lex LexSpans `json:"-"`
}

// LexSpans holds [start,end) byte offsets (into Src.Ipmt) for syntactic
// lexemes used by the semantic tokenizer. See [Src.Lex].
type LexSpans struct {
	Comments           [][2]int   // full-line `#` comments (leading whitespace included)
	KindMarkers        []KindSpan // `::e`/`::t`/`::c` markers, with the node's kind
	TypeMarkers        [][2]int   // `::a` and `::tip` markers
	AliasDecls         []KindSpan // alias-name identifiers at declaration sites, with the node's kind
	AliasRefs          []KindSpan // bare references that resolved to an alias, with the node's kind
	Strings            [][2]int   // quoted node titles (incl. quotes)
	Tooltips           [][2]int   // quoted node-tooltip strings (incl. quotes)
	UnresolvedPrefixes [][2]int   // `::?` prefix markers (the grey ipmUnresolved color)
}

// KindSpan is a span tagged with the node kind it belongs to, so the
// tokenizer can pick the right `ipm{Event,Thing,Concept}` token type.
type KindSpan struct {
	Span [2]int
	Type NodeType
}

// IpmGraph is a parsed IPM graph: its nodes, edges, and source provenance.
type IpmGraph struct {
	Version string `json:"version"`
	Src     Src    `json:"src"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
}
