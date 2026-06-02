package parser_test

// Validate-layer rejection cases for the ipmt-invalid corpus.
//
// A ```ipmt-invalid example must be rejected by the toolchain. Most are rejected
// by the parser (asserted in the internal TestParseInvalidCases). Some parse
// cleanly but are rejected by ipm-validate — e.g. a self-loop (IPMV1.1). Those
// carry a <stem>.validate.error.json sidecar listing the expected error codes.
//
// This lives in the EXTERNAL parser_test package because validate imports
// parser: an internal `package parser` test importing validate would be an
// import cycle.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/validate"
	"github.com/infinite-pm/ipm-tools/pkg/testconfig"
)

func TestValidateRejectCases(t *testing.T) {
	sourcesPath := filepath.Join("..", "..", "..", "tests", "sources.json")
	configs, err := testconfig.Load(sourcesPath)
	if err != nil {
		t.Skipf("Cannot load sources.json: %v", err)
	}

	var invalidConfigs []testconfig.SourceConfig
	for _, cfg := range configs {
		if cfg.Params == nil {
			continue
		}
		if cfg.Params.ParserName == "ipmt-invalid-md" || cfg.Params.ParserName == "ipmt-invalid-file" {
			invalidConfigs = append(invalidConfigs, cfg)
		}
	}
	if len(invalidConfigs) == 0 {
		t.Skip("No ipmt-invalid configurations found")
	}

	baseDir := filepath.Join("..", "..", "..", "tests")
	total := 0
	for _, cfg := range invalidConfigs {
		destDir := filepath.Join(baseDir, cfg.Destination)
		entries, err := os.ReadDir(destDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ipmt-invalid") {
				continue
			}
			testName := strings.TrimSuffix(entry.Name(), ".ipmt-invalid")
			sidecarPath := filepath.Join(destDir, testName+".validate.error.json")
			sidecar, statErr := os.ReadFile(sidecarPath)
			if statErr != nil {
				continue // parser-layer case (covered by TestParseInvalidCases)
			}
			total++

			t.Run(cfg.Destination+"/"+testName, func(t *testing.T) {
				var want struct {
					Codes []string `json:"codes"`
				}
				if err := json.Unmarshal(sidecar, &want); err != nil {
					t.Fatalf("bad validate.error.json: %v", err)
				}
				if len(want.Codes) == 0 {
					t.Fatalf("%s lists no expected error codes", sidecarPath)
				}

				src, err := os.ReadFile(filepath.Join(destDir, entry.Name()))
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				// A validate-layer case MUST parse cleanly (otherwise it would be
				// a parser-layer case with a .parser.error.json sidecar).
				g, err := parser.ParseIPMTBytes(src, testName)
				if err != nil {
					t.Fatalf("expected clean parse (validate-layer case), got parse error: %v", err)
				}

				gotCodes := map[string]bool{}
				for _, f := range validate.RunChecks(g, validate.AllChecks()) {
					if f.Severity == validate.SeverityError {
						gotCodes[f.Code] = true
					}
				}
				if len(gotCodes) == 0 {
					t.Fatalf("expected ≥1 validate error finding, got none (case is not invalid)")
				}
				for _, code := range want.Codes {
					if !gotCodes[code] {
						t.Errorf("expected validate error code %q not produced (got %v)", code, keys(gotCodes))
					}
				}
			})
		}
	}

	if total == 0 {
		t.Skip("No validate-layer invalid cases found")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
