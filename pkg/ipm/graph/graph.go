// Package graph is the model by its base names — Graph, Node, Edge, the four
// kinds, the four links — so an application imports one package for the graph
// and the ops on it, plus the Builder every programmatic producer needs: a
// shared id space for nodes and edges (the parser numbers nodes then edges),
// one edge per node pair (the four SST relations are mutually exclusive per
// pair, so a second is dropped and the graph stays valid), and the JSON the
// whole ecosystem reads.
//
// It came up from ipm-k8s-case's pkg/ipmgraph, which mirrored the model struct
// for struct; the mirror is gone and this is what it imports. Graph ops live
// here, beside the model; what serves them lives in ipm-server.
package graph

import (
	"encoding/json"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// Version is the graph JSON version programmatic producers stamp.
const Version = "25.09"

// The model, by its base names.
type (
	Graph    = model.IpmGraph
	Node     = model.Node
	Edge     = model.Edge
	NodeType = model.NodeType
	Link     = model.SstLinkType
)

const (
	Thing      = model.Thing
	Event      = model.Event
	Concept    = model.Concept
	Unresolved = model.Unresolved

	PartOf    = model.PartOf
	LeadsTo   = model.LeadsTo
	Expresses = model.Expresses
	NearTo    = model.NearTo
)

// Builder accumulates nodes and edges with a single shared id space.
type Builder struct {
	g     Graph
	next  int
	pairs map[[2]int]bool
}

// New returns a Builder for a programmatically produced graph.
func New() *Builder {
	return &Builder{
		g:     Graph{Version: Version, Src: model.Src{Type: "programmatic"}},
		next:  1,
		pairs: map[[2]int]bool{},
	}
}

func (b *Builder) id() int { i := b.next; b.next++; return i }

// Node adds a node and returns its id.
func (b *Builder) Node(t NodeType, name, alias, tooltip string) int {
	id := b.id()
	b.g.Nodes = append(b.g.Nodes, Node{ID: id, Name: name, Alias: alias, Type: t, Tooltip: tooltip})
	return id
}

// Edge adds a directed edge with the given SST link type. A second edge between
// an already-linked pair is dropped: the four relations are mutually exclusive
// per pair, and the drop keeps every emitted graph valid under ipm-validate.
func (b *Builder) Edge(source, target int, link Link, tooltip string) {
	key := [2]int{source, target}
	if key[0] > key[1] {
		key[0], key[1] = key[1], key[0]
	}
	if b.pairs[key] {
		return
	}
	b.pairs[key] = true
	dir := model.DirFwd
	if link == NearTo {
		dir = model.DirUndir // near-to is undirected
	}
	b.g.Edges = append(b.g.Edges, Edge{
		ID: b.id(), Source: source, Target: target,
		Dir: dir, Tooltip: tooltip, SstLinkType: link, SemOrigin: model.SemExplicit,
	})
}

// Graph returns the accumulated graph.
func (b *Builder) Graph() *Graph { return &b.g }

// JSON returns the indented graph JSON.
func (b *Builder) JSON() ([]byte, error) { return Marshal(&b.g) }

// Marshal returns the indented graph JSON, newline-terminated.
func Marshal(g *Graph) ([]byte, error) {
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Load reads a graph JSON file.
func Load(data []byte) (*Graph, error) {
	var g Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	return &g, nil
}
