// Command ipmt-name-merge puts the names back into an era's layout JSON.
//
// Some 2025-era engines split the work in two: `ipmt-parse` produced the
// graph, with each node's NAME, and `layout-gen` produced only geometry —
// ids and boxes, no labels. That era's renderer read both files and joined
// them. layout-audit reads one: the engine's stdout.
//
// So a whole era rendered as unlabelled boxes, and — worse, because it is
// silent — the structural diff identifies a node by Type+Label+Alias, so an
// unlabelled side matched NOTHING and every diagram in those columns reported
// as wholly changed. The pictures looked like a different diagram and the
// change counts agreed with them.
//
// This reads the layout JSON on stdin, the parse JSON from argv[1], copies
// each node's name onto the layout node with the same id, and writes the
// result to stdout. It is the join that era's renderer did.
//
//	ipmt-parse --in x.ipmt > parse.json
//	layout-gen --in parse.json --out - | ipmt-name-merge parse.json
//
// gl:docs/dev-tools/ipmt-name-merge.md
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func main() { os.Exit(run()) }

func run() int {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: ipmt-name-merge <parse.json>   (layout JSON on stdin)")
		return 2
	}
	parse, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ipmt-name-merge:", err)
		return 1
	}
	layout, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ipmt-name-merge:", err)
		return 1
	}
	out, err := Merge(layout, parse)
	if err != nil {
		// A merge that cannot be done must not swallow the layout: passing it
		// through unchanged leaves the report exactly as it was without this
		// step, which is a worse picture but not a lost one.
		fmt.Fprintln(os.Stderr, "ipmt-name-merge:", err)
		os.Stdout.Write(layout)
		return 0
	}
	os.Stdout.Write(out)
	return 0
}

// Merge copies node names from parse into layout, by id.
//
// Only nodes with no label of their own are touched: an engine that already
// labels its output is telling the truth about itself, and must be left to.
func Merge(layout, parse []byte) ([]byte, error) {
	names, err := namesByID(parse)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no named nodes in the parse output")
	}

	var doc map[string]any
	if err := decode(layout, &doc); err != nil {
		return nil, fmt.Errorf("layout: %w", err)
	}
	nodes, _ := doc["nodes"].([]any)
	filled := 0
	for _, raw := range nodes {
		n, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if s, _ := n["label"].(string); s != "" {
			continue
		}
		if name, ok := names[idOf(n["id"])]; ok {
			n["label"] = name
			filled++
		}
	}
	if filled == 0 {
		return nil, fmt.Errorf("no node took a name (%d in the parse output)", len(names))
	}
	return json.Marshal(doc)
}

// namesByID indexes the parse output. The name lives under "name" in that
// era; "label" is accepted too, so a later parser needs no new code here.
func namesByID(parse []byte) (map[string]string, error) {
	var doc struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := decode(parse, &doc); err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}
	out := map[string]string{}
	for _, n := range doc.Nodes {
		name, _ := n["name"].(string)
		if name == "" {
			name, _ = n["label"].(string)
		}
		if name != "" {
			out[idOf(n["id"])] = name
		}
	}
	return out, nil
}

// idOf normalises an id to a string: the two files disagree about whether an
// id is a number or a string, and matching on the Go type would silently
// join nothing.
func idOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// decode keeps numbers exactly as written, so coordinates survive the
// round-trip instead of arriving as 1.2e+02.
func decode(data []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(into)
}
