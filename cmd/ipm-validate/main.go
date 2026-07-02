package main

// Command ipm-validate runs semantic checks on an ipmt source, graphJSON,
// or markdown file with ```ipmt fenced blocks. Emits findings.
// See docs/ipm-validate.md (how to use) and docs/ipm-validator-rules.md
// (rule catalog).
//
// Usage:
//   ipm-validate [--in <file>] [--json] [--in-json|--in-md] [--list-checks] [--strict-undecided]
//
// Reads from stdin when --in is omitted. The input mode is:
//   - graphJSON when --in-json is set OR --in ends with .json
//   - markdown  when --in-md   is set OR --in ends with .md
//   - ipmt source otherwise
//
// Exit codes:
//   0  no findings
//   1  warnings or info only
//   2  parse / read error
//   3  one or more error-severity findings

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/validate"
	"github.com/infinite-pm/ipm-tools/pkg/ipmtmeta"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// strictUndecided, set from --strict-undecided, makes Unresolved node kinds
// errors (publish gate) rather than warnings (draft). See runValidate.
var strictUndecided bool

// runValidate runs every check with the undecided-kind strictness from the flag.
func runValidate(g *model.IpmGraph) []validate.Finding {
	return runValidateWith(g, strictUndecided)
}

// runValidateWith runs every check with an explicit undecided-kind strictness.
// A per-block `ipmt unresolved` directive forces strict=false for that block so
// its grey nodes stay warnings even under the publish gate.
func runValidateWith(g *model.IpmGraph, strict bool) []validate.Finding {
	return validate.RunChecks(g, validate.AllChecksWith(strict))
}

func main() {
	inFile := flag.String("in", "", "input file (default: stdin)")
	jsonOut := flag.Bool("json", false, "emit findings as JSON")
	inJSON := flag.Bool("in-json", false, "treat input as graphJSON (already-parsed IpmGraph)")
	inMD := flag.Bool("in-md", false, "treat input as markdown with ```ipmt fenced blocks")
	listChecks := flag.Bool("list-checks", false, "list registered checks and exit")
	strict := flag.Bool("strict-undecided", false, "treat Unresolved (grey) node kinds as errors — the publish gate")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: ipm-validate [--in <file>] [--json] [--in-json|--in-md] [--list-checks] [--strict-undecided]")
		fmt.Fprintln(os.Stderr, "Reads from stdin when --in is omitted.")
		fmt.Fprintln(os.Stderr, "Input mode: graphJSON if --in-json or .json extension; markdown if --in-md or .md extension; otherwise ipmt source.")
		fmt.Fprintln(os.Stderr, "\nExit codes:")
		fmt.Fprintln(os.Stderr, "  0  no findings")
		fmt.Fprintln(os.Stderr, "  1  warnings or info only")
		fmt.Fprintln(os.Stderr, "  2  parse / read error")
		fmt.Fprintln(os.Stderr, "  3  one or more error-severity findings")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()
	strictUndecided = *strict

	if *listChecks {
		for _, c := range validate.AllChecks() {
			fmt.Printf("%s\t%s\n", c.Code(), c.Description())
		}
		return
	}

	var (
		data []byte
		err  error
		name string
	)
	if *inFile == "" {
		data, err = io.ReadAll(bufio.NewReader(os.Stdin))
		if err != nil {
			fmt.Fprintln(os.Stderr, "read stdin:", err)
			os.Exit(2)
		}
		name = "<stdin>"
	} else {
		data, err = os.ReadFile(*inFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read file:", err)
			os.Exit(2)
		}
		name = filepath.Base(*inFile)
	}

	lowName := strings.ToLower(*inFile)
	useJSON := *inJSON || strings.HasSuffix(lowName, ".json")
	useMD := *inMD || strings.HasSuffix(lowName, ".md")

	if useJSON && useMD {
		fmt.Fprintln(os.Stderr, "input mode ambiguous: both --in-json and --in-md set, or extension conflicts")
		os.Exit(2)
	}

	var findings []validate.Finding
	switch {
	case useJSON:
		findings, err = validateGraphJSON(data, name)
	case useMD:
		findings, err = validateMarkdown(data, name)
	default:
		findings, err = validateIPMT(data, name)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	for i := range findings {
		if findings[i].File == "" {
			findings[i].File = name
		}
	}

	if *jsonOut {
		b, err := json.MarshalIndent(findings, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "encode JSON:", err)
			os.Exit(2)
		}
		fmt.Println(string(b))
	} else {
		for _, f := range findings {
			fmt.Fprintln(os.Stderr, f.String())
		}
		if len(findings) > 0 {
			fmt.Fprintf(os.Stderr, "\n%d finding(s).\n", len(findings))
		}
	}

	if validate.HasErrors(findings) {
		os.Exit(3)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
}

func validateIPMT(data []byte, name string) ([]validate.Finding, error) {
	// The `# ipmt:` first-line pragma is the flag channel for a standalone .ipmt
	// file. A misplaced/unknown pragma is a finding; `unresolved`/`defaults` opt
	// the file out of the strict-undecided publish gate.
	meta, merr := ipmtmeta.MetaFromContent(string(data))
	if merr != nil {
		return []validate.Finding{{
			Code:     "IPMV0",
			Severity: validate.SeverityError,
			Message:  "invalid ipmt metadata: " + merr.Error(),
			File:     name,
			Line:     1,
			Column:   1,
		}}, nil
	}
	g, err := parser.ParseIPMTBytes(data, name)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	strict := strictUndecided &&
		!ipmtmeta.Contains(meta, ipmtmeta.FlagUnresolved) &&
		!ipmtmeta.Contains(meta, ipmtmeta.FlagDefaults)
	return runValidateWith(g, strict), nil
}

func validateGraphJSON(data []byte, _ string) ([]validate.Finding, error) {
	g := &model.IpmGraph{}
	if err := json.Unmarshal(data, g); err != nil {
		return nil, fmt.Errorf("parse graphJSON: %w", err)
	}
	if g.Src.Positions.Nodes == nil {
		g.Src.Positions.Nodes = map[int][2]int{}
	}
	if g.Src.Positions.Edges == nil {
		g.Src.Positions.Edges = map[int]model.EdgePos{}
	}
	return runValidate(g), nil
}

// validateMarkdown extracts every ```ipmt fenced block from the source,
// parses each as its own graph, validates each independently, and
// adjusts finding line numbers so they point at the markdown file rather
// than at the block-internal line.
//
// Each block parses independently per the ipmt grammar; cross-block
// validation (e.g. name collisions across blocks) is out of scope here.
func validateMarkdown(data []byte, name string) ([]validate.Finding, error) {
	_, blocks := markdown.ScanIPMTBlocks(string(data))
	// A markdown file with no ipmt blocks is a normal case: nothing to
	// validate, no findings. Composable with `find ... | xargs`.
	if len(blocks) == 0 {
		return nil, nil
	}
	var out []validate.Finding
	for _, b := range blocks {
		// Effective flags = fence tokens ∪ `# ipmt:` pragma in the block content.
		// A misplaced/unknown flag is a finding at the fence line.
		meta, merr := ipmtmeta.EffectiveMeta(b.Meta, b.Content)
		if merr != nil {
			out = append(out, validate.Finding{
				Code:     "IPMV0",
				Severity: validate.SeverityError,
				Message:  fmt.Sprintf("block %d invalid ipmt metadata: %v", b.Index, merr),
				File:     name,
				Line:     b.StartLine + 1,
				Column:   1,
			})
			continue
		}
		g, err := parser.ParseIPMTBytes([]byte(b.Content), fmt.Sprintf("%s [block %d]", name, b.Index))
		if err != nil {
			// Surface parse errors as a synthetic finding pointing at the
			// fence line, then move on to the next block.
			out = append(out, validate.Finding{
				Code:     "IPMV0",
				Severity: validate.SeverityError,
				Message:  fmt.Sprintf("block %d failed to parse: %v", b.Index, err),
				File:     name,
				Line:     b.StartLine + 1, // 1-based file line of opening fence
				Column:   1,
			})
			continue
		}
		// An `unresolved`/`defaults` block (via fence meta or pragma) opts out of
		// the grey-node publish gate: its Unresolved nodes stay warnings even
		// under --strict-undecided, since it is authored with grey (::?…) nodes
		// for the solver to resolve.
		blockStrict := strictUndecided &&
			!ipmtmeta.Contains(meta, ipmtmeta.FlagUnresolved) &&
			!ipmtmeta.Contains(meta, ipmtmeta.FlagDefaults)
		fs := runValidateWith(g, blockStrict)
		// Adjust intra-block 1-based line numbers to file 1-based line numbers.
		// block.StartLine is the 0-based fence line; content line 1 is at
		// file 1-based line block.StartLine + 2.
		for i := range fs {
			if fs[i].Line > 0 {
				fs[i].Line = b.StartLine + 1 + fs[i].Line
			}
			if fs[i].File == "" {
				fs[i].File = name
			}
		}
		out = append(out, fs...)
	}
	return out, nil
}
