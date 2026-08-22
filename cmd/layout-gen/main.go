package main

// Command layout-gen generates layout JSON from IPM input — the SHIPPING
// tool the VS Code extension's stack calls (ipm-rpc renders through it).
// It is pure ipmt/ipm.json/layout.json → layout.json (--in/--out/--pretty);
// the dev views (--why/--facts/--table/--edges) live in
// cmd-dev/layout-debug, and the narrated report in cmd-dev/layout-explain
// (the debug↔explain split, docs/dev/layout-gen/layout-debug.md).
//
// gl:docs/layout-gen.md

import (
	"flag"
	"fmt"
	"os"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/l7report"
	"github.com/infinite-pm/ipm-tools/pkg/layoutdupe"
)

func main() {
	c := cli.RegisterCommon(flag.CommandLine)
	var debugJSON string
	var duplicate bool
	var dupFactor float64
	var dupMinPx float64
	flag.Float64Var(&dupFactor, "duplicate-factor", 0, "with --duplicate: how many times the diagram's MEDIAN edge length counts as too long to read from here (default 2)")
	flag.Float64Var(&dupMinPx, "duplicate-min-px", 0, "with --duplicate: the floor under that, in px (default 320) — in a tight diagram twice the median is still readable")
	flag.BoolVar(&duplicate, "duplicate", false, "draw a participant more than once when its users are too far apart to read it from where they are (pkg/layoutdupe): the copies carry their local edges, the graph is unchanged. Off by default — a placement policy, measured per corpus")
	flag.StringVar(&debugJSON, "debug-json", "", "also record the run — trace events plus the resulting graph — to this path as a throwaway .debug.json; the dev tools (layout-debug, layout-explain) then narrate it with --in <path> WITHOUT re-generating. The flag only installs the trace hook (otherwise nil); it costs nothing when absent")
	version := cli.VersionFlag(flag.CommandLine, "layout-gen")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: layout-gen [--in <file.ipmt|file.ipm.json|file.layout.json|->] [--out <layout.json>|-] [--pretty=true] [--debug-json <path>]")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()
	if version(os.Stdout) {
		return
	}

	// Recording path: run with the trace, write the layout as usual, and
	// dump the recorded run for the dev tools to replay.
	if debugJSON != "" {
		rep, _, err := cli.LoadReport(c)
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-gen: %v\n", err)
			os.Exit(2)
		}
		if err := cli.WriteGraph(c, rep.Graph); err != nil {
			fmt.Fprintf(os.Stderr, "layout-gen: %v\n", err)
			os.Exit(2)
		}
		data, err := l7report.NewRecording(rep, l7report.Source{File: c.In}).JSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-gen: encode debug.json: %v\n", err)
			os.Exit(2)
		}
		if err := os.WriteFile(debugJSON, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "layout-gen: write debug.json: %v\n", err)
			os.Exit(2)
		}
		return
	}

	detected, graph, err := cli.LoadGraph(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "layout-gen: %v\n", err)
		os.Exit(2)
	}
	if duplicate {
		if detected.IPMGraph == nil {
			fmt.Fprintln(os.Stderr, "layout-gen: --duplicate needs an .ipmt or .ipm.json input (a laid-out graph has no participants left to place)")
			os.Exit(2)
		}
		res, derr := layoutdupe.Generate(detected.IPMGraph, layoutdupe.Options{
			LongFactor: dupFactor, MinLongPx: dupMinPx,
		})
		if derr != nil {
			fmt.Fprintf(os.Stderr, "layout-gen: duplicate: %v\n", derr)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "layout-gen: %s\n", res.Describe())
		graph = res.Graph
	}
	if err := cli.WriteGraph(c, graph); err != nil {
		fmt.Fprintf(os.Stderr, "layout-gen: %v\n", err)
		os.Exit(2)
	}
}
