// Command gen-invalid-sidecars (re)generates the rejection sidecars for the
// ipmt-invalid corpus (tests/ipmt/invalid/*.ipmt-invalid).
//
// Each negative example must be REJECTED by the toolchain. This tool records at
// which layer, one sidecar per case:
//
//   - <stem>.parser.error.json  — the parser rejects it (a ParseError). Records
//     {error, message, start, end}, matching the historical hand-authored shape.
//   - <stem>.validate.error.json — it parses, but ipm-validate finds ≥1
//     error-severity finding (e.g. self-loop, IPMV1.1). Records the expected
//     error {codes} plus the full {errors} for reference.
//
// A case that neither fails to parse nor yields a validate error is NOT actually
// invalid and is reported as an error (exit 1). Byte offsets shift whenever the
// source text changes, so this tool is the source of truth — run it after
// editing the corpus or the invalid examples in docs/ipmt-spec.md.
//
// Usage:
//
//	gen-invalid-sidecars [--dir tests/ipmt/invalid] [--check]
//
// --check writes nothing and exits 1 if any sidecar is missing/stale.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/validate"
)

// parserErrorSidecar mirrors the historical tests/ipmt/invalid/*.parser.error.json
// shape (error == message, both the ParseError text).
type parserErrorSidecar struct {
	End     int    `json:"end"`
	Error   string `json:"error"`
	Message string `json:"message"`
	Start   int    `json:"start"`
}

// validateErrorSidecar records a parse-clean but validation-rejected case. codes
// is the assertion key (unique, sorted error-severity codes); errors carries the
// full findings for human reference.
type validateErrorSidecar struct {
	Codes  []string           `json:"codes"`
	Errors []validate.Finding `json:"errors"`
}

func main() {
	dir := flag.String("dir", filepath.Join("tests", "ipmt", "invalid"), "corpus directory")
	check := flag.Bool("check", false, "dry-run: exit 1 if any sidecar is missing or stale, write nothing")
	flag.Parse()

	entries, err := os.ReadDir(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read dir:", err)
		os.Exit(2)
	}

	stale := false
	var failures []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ipmt-invalid") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".ipmt-invalid")
		src, err := os.ReadFile(filepath.Join(*dir, e.Name()))
		if err != nil {
			fmt.Fprintln(os.Stderr, "read:", err)
			os.Exit(2)
		}
		parserPath := filepath.Join(*dir, stem+".parser.error.json")
		validatePath := filepath.Join(*dir, stem+".validate.error.json")

		_, perr := parser.ParseIPMTBytes(src, stem)
		switch {
		case perr != nil:
			// Parser-layer rejection.
			want := parserErrorSidecar{Error: perr.Error(), Message: perr.Error()}
			var pe *parser.ParseError
			if errors.As(perr, &pe) {
				want.Start, want.End = pe.Start, pe.End
			}
			if reconcile(parserPath, want, *check) {
				stale = true
			}
			if removeIfPresent(validatePath, *check) {
				stale = true
			}
		default:
			// Parses clean — must be a validate-layer rejection.
			g, err := parser.ParseIPMTBytes(src, stem)
			if err != nil { // unreachable (perr was nil), guard anyway
				failures = append(failures, fmt.Sprintf("%s: reparse failed: %v", stem, err))
				continue
			}
			var errFindings []validate.Finding
			for _, f := range validate.RunChecks(g, validate.AllChecks()) {
				if f.Severity == validate.SeverityError {
					errFindings = append(errFindings, f)
				}
			}
			if len(errFindings) == 0 {
				failures = append(failures, fmt.Sprintf("%s: parses clean AND produces no validate error — not an invalid example", stem))
				continue
			}
			want := validateErrorSidecar{Codes: uniqueSortedCodes(errFindings), Errors: errFindings}
			if reconcile(validatePath, want, *check) {
				stale = true
			}
			if removeIfPresent(parserPath, *check) {
				stale = true
			}
		}
	}

	for _, f := range failures {
		fmt.Fprintln(os.Stderr, "ERROR:", f)
	}
	if len(failures) > 0 {
		os.Exit(1)
	}
	if *check && stale {
		os.Exit(1)
	}
}

// reconcile marshals want to indented JSON and, unless check-only, writes path
// when the on-disk content differs. Returns true if the file was (or would be)
// changed.
func reconcile(path string, want any, check bool) bool {
	data, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(2)
	}
	data = append(data, '\n')
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return false
	}
	if check {
		fmt.Fprintln(os.Stderr, "STALE:", path)
		return true
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(2)
	}
	fmt.Println("wrote", path)
	return true
}

func removeIfPresent(path string, check bool) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	if check {
		fmt.Fprintln(os.Stderr, "STALE (should be removed):", path)
		return true
	}
	if err := os.Remove(path); err != nil {
		fmt.Fprintln(os.Stderr, "remove:", err)
		os.Exit(2)
	}
	fmt.Println("removed", path)
	return true
}

func uniqueSortedCodes(fs []validate.Finding) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, f := range fs {
		if _, ok := seen[f.Code]; ok {
			continue
		}
		seen[f.Code] = struct{}{}
		out = append(out, f.Code)
	}
	sort.Strings(out)
	return out
}
