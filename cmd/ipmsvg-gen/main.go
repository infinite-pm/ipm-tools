package main

// Command ipmsvg-gen renders a minimal SVG preview from layout JSON.
//
// Spec: gl:docs/ipmsvg-gen.md
// Layout algorithm: gl:docs/dev/layout-gen/layout-alg.md
// Related: gl:cmd/layout-gen/main.go gl:pkg/ipmsvg/svg.go

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/ipmsvg"
)

func main() {
	var inPath string
	var outPath string

	flag.StringVar(&inPath, "in", "-", "input file: .ipmt, .ipm.json (IPM graph), or .layout.json (use - for stdin)")
	flag.StringVar(&outPath, "out", "-", "output SVG file (use - for stdout)")
	version := cli.VersionFlag(flag.CommandLine, "ipmsvg-gen")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: ipmsvg-gen [--in <file.ipmt|file.ipm.json|file.layout.json|->] [--out <diagram.ipm.svg>|-]")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()
	if version(os.Stdout) {
		return
	}

	src, err := readInput(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ipmsvg-gen: read input: %v\n", err)
		os.Exit(2)
	}

	// Detect input format
	detected, err := cli.DetectInput(src, inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ipmsvg-gen: %v\n", err)
		os.Exit(2)
	}

	var svg []byte
	switch detected.Type {
	case cli.InputTypeLayoutGraph:
		// Direct rendering from layout
		svg, err = ipmsvg.Render(detected.LayoutGraph)
	case cli.InputTypeIPMGraph, cli.InputTypeIPMT:
		// Run layout internally
		svg, err = ipmsvg.GenerateFromIPM(detected.IPMGraph)
	default:
		// Defensive guard: cli.DetectInput only returns the three concrete types
		// above on a nil error, so this is unreachable today. It exists so a future
		// InputType added to DetectInput fails loudly here instead of silently.
		fmt.Fprintf(os.Stderr, "ipmsvg-gen: unsupported input type\n")
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "ipmsvg-gen: render SVG: %v\n", err)
		os.Exit(2)
	}

	if err := writeOutput(outPath, svg); err != nil {
		fmt.Fprintf(os.Stderr, "ipmsvg-gen: write output: %v\n", err)
		os.Exit(2)
	}
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(bufio.NewReader(os.Stdin))
	}
	return os.ReadFile(path)
}

func writeOutput(path string, data []byte) error {
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
