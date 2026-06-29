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
)

func main() {
	c := cli.RegisterCommon(flag.CommandLine)
	var debugJSON string
	flag.StringVar(&debugJSON, "debug-json", "", "also record the run — trace events plus the resulting graph — to this path as a throwaway .debug.json; the dev tools (layout-debug, layout-explain) then narrate it with --in <path> WITHOUT re-generating. The flag only installs the trace hook (otherwise nil); it costs nothing when absent")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: layout-gen [--in <file.ipmt|file.ipm.json|file.layout.json|->] [--out <layout.json>|-] [--pretty=true] [--debug-json <path>]")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()

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

	_, graph, err := cli.LoadGraph(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "layout-gen: %v\n", err)
		os.Exit(2)
	}
	if err := cli.WriteGraph(c, graph); err != nil {
		fmt.Fprintf(os.Stderr, "layout-gen: %v\n", err)
		os.Exit(2)
	}
}
