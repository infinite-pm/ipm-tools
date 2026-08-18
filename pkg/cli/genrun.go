package cli

// genrun holds the generate-and-write core shared by layout-gen and the
// dev tools (cmd-dev/layout-debug, cmd-dev/layout-explain). The three
// mains register the common flags FIRST (RegisterCommon), add their own
// addon flags after, and run ONE load pass — so a flag added to the
// shipping surface here appears in all three for free, with identical
// semantics (docs/dev/layout-gen/layout-debug.md).

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/l7report"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layout7"
)

// Common holds the flags every layout-consuming command shares:
// --in/--out/--pretty. Their help text and defaults ARE layout-gen's
// shipping surface — the dev tools inherit it verbatim.
type Common struct {
	In     string
	Out    string
	Pretty bool
	// Containers runs the engine the way the zoom canvas does (layout7
	// Options.Containers): a composite's sub-grid claims its vertical band
	// exclusively. Off is the flat render.
	Containers bool
	// Shells emits the composite shells as engine output (layout7
	// Options.Shells, implies Containers) — the shells-in-the-core
	// candidate the zoom canvas's engine pipeline runs.
	Shells bool
}

// RegisterCommon registers --in/--out/--pretty on fs and returns the
// Common whose fields they write. Call it BEFORE registering a tool's
// addon flags so the shared surface reads first in --help.
func RegisterCommon(fs *flag.FlagSet) *Common {
	c := &Common{}
	fs.StringVar(&c.In, "in", "-", "input file: .ipmt, .ipm.json (IPM graph), or .layout.json (use - for stdin)")
	fs.StringVar(&c.Out, "out", "-", "output ipm-simple-graph JSON file (use - for stdout)")
	fs.BoolVar(&c.Pretty, "pretty", true, "pretty-print JSON output")
	fs.BoolVar(&c.Containers, "containers", false, "run the engine as the zoom canvas does (layout7 Options.Containers: a composite's sub-grid claims its vertical band exclusively) — the decisions a canvas URL asks about; off is the flat render")
	fs.BoolVar(&c.Shells, "shells", false, "run the engine with composite SHELLS as its own output (layout7 Options.Shells, implies --containers): the shells-in-the-core pipeline a `framecheck --engine --dump-ipmt` state graph is laid out with")
	return c
}

// ReadInput reads a --in path, treating "-" as stdin.
func ReadInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(bufio.NewReader(os.Stdin))
	}
	return os.ReadFile(path)
}

// WriteOutput writes to a --out path, treating "-" as stdout.
func WriteOutput(path string, data []byte) error {
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// readAndDetect reads --in and detects its format — the shared front of
// both load paths.
func readAndDetect(c *Common) (*DetectedInput, error) {
	src, err := ReadInput(c.In)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	return DetectInput(src, c.In)
}

// LoadGraph returns the layout graph WITHOUT tracing: generate for
// ipmt/ipm.json, pass through for an already-laid-out layout.json. This
// is layout-gen's shipping path — no trace machinery, no report.
func LoadGraph(c *Common) (*DetectedInput, *layout.Graph, error) {
	detected, err := readAndDetect(c)
	if err != nil {
		return nil, nil, err
	}
	switch detected.Type {
	case InputTypeLayoutGraph:
		return detected, detected.LayoutGraph, nil
	case InputTypeIPMGraph, InputTypeIPMT:
		g, err := layout7.Generate(detected.IPMGraph)
		if err != nil {
			return nil, nil, fmt.Errorf("generate layout: %w", err)
		}
		return detected, g, nil
	default:
		return nil, nil, fmt.Errorf("unsupported input type")
	}
}

// LoadReport returns a TRACED report — the dev tools' path. A recorded
// .debug.json is decoded and replayed (generation SKIPPED); for an
// ipmt/ipm.json input the engine runs with a collecting trace (every
// decision recorded); a passed-through layout.json yields a graph-only
// report (no events — the pipeline did not run, so --why has nothing to
// narrate, but --table/--edges/--facts still work off the graph).
func LoadReport(c *Common) (*l7report.Report, *DetectedInput, error) {
	src, err := ReadInput(c.In)
	if err != nil {
		return nil, nil, fmt.Errorf("read input: %w", err)
	}
	// A recorded run is a dev-tool input; intercept it before DetectInput,
	// which the shipping tools also call and must never see .debug.json.
	if strings.HasSuffix(strings.ToLower(filepath.Base(c.In)), ".debug.json") {
		rep, err := l7report.DecodeRecording(src)
		if err != nil {
			return nil, nil, err
		}
		return rep, &DetectedInput{Type: InputTypeDebugJSON}, nil
	}
	detected, err := DetectInput(src, c.In)
	if err != nil {
		return nil, nil, err
	}
	switch detected.Type {
	case InputTypeLayoutGraph:
		return &l7report.Report{Graph: detected.LayoutGraph}, detected, nil
	case InputTypeIPMGraph, InputTypeIPMT:
		rep, err := l7report.RunWithOptions(detected.IPMGraph, layout7.Options{Containers: c.Containers, Shells: c.Shells})
		if err != nil {
			return nil, nil, err
		}
		return rep, detected, nil
	default:
		return nil, nil, fmt.Errorf("unsupported input type")
	}
}

// WriteGraph encodes g as layout JSON to --out, honouring --pretty (the
// shipping default: indented, trailing newline).
func WriteGraph(c *Common, g *layout.Graph) error {
	var out []byte
	var err error
	if c.Pretty {
		out, err = json.MarshalIndent(g, "", "  ")
	} else {
		out, err = json.Marshal(g)
	}
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	if c.Pretty {
		out = append(out, '\n')
	}
	return WriteOutput(c.Out, out)
}
