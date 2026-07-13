# Testing Guide

## Overview

The project includes comprehensive Go tests that leverage the `sources.json` configuration to automatically test all generated test cases. This provides automatic coverage, regression detection, and consistency across the test suite.

## Test Structure

### Parser Tests (`pkg/ipm/parser/parser_test.go`)

The parser package includes several test functions:

1. **`TestParseValidCases`** - Tests all valid ipmt files from `sources.json`
   - Discovery is by `parser_name`: every config whose parser is `ipmt-md` or `ipmt-file` (the `ipmt-solver-md` and `ipmdev-layout-rule-md` lanes are not parser-tested)
   - Parses each `.ipmt` file and compares output with corresponding `.json` file
   - Validates structural correctness and JSON equivalence

2. **`TestParseInvalidCases`** - Tests that invalid ipmt files produce errors
   - Tests all cases from the ` ```ipmt-invalid ` lane (`parser_name`
     `ipmt-invalid-md` / `ipmt-invalid-file`; stored as `.ipmt-invalid` files)
   - Verifies parse errors are generated and match `.parser.error.json`
   - Cases that parse but fail validation carry a `.validate.error.json`
     sidecar and are asserted by `TestValidateRejectCases` in
     `validate_reject_test.go`
   - Generate both sidecar kinds with `make gen-invalid-sidecars`; a case with
     NEITHER sidecar is silently skipped

3. **`TestParseErrorTypes`** - Tests specific error conditions
   - Duplicate edges
   - Self loops (subtest present but skipped — self-loops are rejected at the
     validate layer instead, see `tests/ipmt/invalid/self-loop.validate.error.json`)
   - Unterminated tooltips
   - Other syntax and semantic errors

4. **`BenchmarkParse`** - Performance benchmark for parser

### Layout Tests

Layout behavior is validated by the DSL rule runner (`cmd-dev/layout-test-runner`)
over TWO corpora — `tests/layout-gen` + `tests/layout-gen-rules/*.dsl` and
`tests/layout-gen-ext` + `tests/layout-gen-ext-rules/*.dsl` — driven by
`make layout-fitness` / `make layout-test` (each runs the runner twice, printing
one score per corpus; `--dir` selects a corpus and `-all` surveys every failing
rule instead of stopping at the first). Go unit tests live alongside the
implementations: `pkg/layout7/*_test.go` (the engine), `pkg/layout/*_test.go`
(VPSC + edge stubs), `pkg/dsltorules/*_test.go` (rules/DSL) and
`pkg/layoutcheck/`.

See [Layout Regression Testing](#layout-regression-testing) below for the
fitness-score mechanism and DSL details.

### Test Configuration (`pkg/testconfig/testconfig.go`)

The test configuration uses structured extraction parameters:

```go
type SourceConfig struct {
    Name        string   `json:"name"`
    Destination string   `json:"destination"`
    SourceFiles []string `json:"source_files"`
    SourcesGlob []string `json:"sources_glob"`
    Method      string   `json:"method"` // "parser" or "ignore"
    Params      *Params  `json:"params,omitempty"`
}

type Params struct {
    ParserName      string                 `json:"parser_name,omitempty"`      // e.g., "ipmt-md", "ipmt-invalid-md", "ipmt-file"
    ParserParams    map[string]interface{} `json:"parser_params,omitempty"`    // e.g., {"filter": "valid"}
    TransformName   string                 `json:"transform_name,omitempty"`   // alias for ParserName
    TransformParams map[string]interface{} `json:"transform_params,omitempty"` // alias for ParserParams
}
```

## Running Tests

### Run all tests
```bash
go test ./...
```

### Run parser tests only
```bash
go test ./pkg/ipm/parser -v
```

### Run layout tests only
```bash
go test ./pkg/layout7 ./pkg/layout ./pkg/layoutcheck -v
```

### Run specific test
```bash
go test ./pkg/ipm/parser -run TestParseValidCases -v
```

### Run with coverage
```bash
go test ./pkg/ipm/parser -cover
```

### Generate coverage report
```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Run benchmarks
```bash
go test ./pkg/ipm/parser -bench=.
```

Run `make help` for the Makefile shortcuts for testing tasks.

## Layout Regression Testing

The layout regression testing framework validates layout algorithm behavior using a DSL-based approach.

### Design Philosophy

**Single Source of Truth**: Test rules are embedded directly in the specification (gl:docs/dev/layout-gen/layout-alg.md) using `ipmdev-layout-rule` code blocks. This ensures:
- Specification and test rules never drift apart
- The specification itself is machine-readable and executable
- Documentation serves as living, validated constraints
- No separate test files to maintain

**Progress Measurement**: Implementation progress is measured by **the line number of the first failing test**. Tests run in specification line order (top to bottom), so:
- Higher fitness score = more complete implementation
- Score never decreases (no regressions allowed)
- Clear metric for tracking development progress
- Example: Score 89 means implementation covers spec lines 1-88 completely

**Cumulative Testing**: Rules accumulate as tests progress through the specification:
- A case collects every rule from line 1 up to the NEXT case's spec line — so it also sees its own local rules, which follow it
- Ensures implementing feature C doesn't break features A and B
- Rules are reusable across test cases
- Tests become more comprehensive as they progress

### DSL Rule Language

Rules are authored in ```ipmdev-layout-rule blocks in the spec docs and pinned to generated `.dsl` files (never hand-edited).

For complete DSL reference, see [DSL Reference](./layout-gen/dsl-reference.md).

**Basic Examples:**

```dsl
# Type-based sizing
each type=event has width=120
each type=event has height=60

# Conditional sizing based on text length
each type=event text-len>72 has height>=140

# ID-based exact sizing
#1 has size=120x60

# Alignment
all #S,#e1,#E have same center-x

# Positional constraints
#e1 is below #S with gap=60
```

**Rule Compaction:**

When multiple rules apply to the same node and property, the system uses **rule compaction** to determine which rule wins:

1. **Specificity**: More specific rules override general ones
   - ID selector (`#1`) beats type with conditions (`type=event text-len>72`)
   - Type with conditions beats plain type (`type=event`)

2. **Recency**: When specificity is equal, later rules (higher line numbers) win

3. **Comparison Merging**: For comparison operators (`>=`, `<=`), stricter constraints win
   - `height>=60` + `height>=140` → `height>=140`

**Example:**
```dsl
# Line 36: General rule for all events
each type=event has width=120
each type=event has height=60

# Line 57: Specific rule for events with long text
each type=event text-len>72 has height>=140
```

For an event with 79 characters:
- Both width and height=60 rules apply from line 36
- The height>=140 rule from line 57 is more specific (has condition)
- Final validation: width=120, height>=140

**Selectors:**
- `each type=X` - Select all nodes of a specific type (e.g., `each type=event`)
- `each type=X text-len>N` - Type with text length condition
- `all #id1,#id2,...` - Select nodes by comma-separated IDs (e.g., `all #S,#e1,#E`)
- `first type=X` - Select the first node of a type (e.g., `first type=thing`)
- `#id` - Select a single node by ID (e.g., `#e1`)

**Assertions:**
- `has property=value` - Assert a property has a specific value
- `has property>=value` - Assert minimum value (comparisons)
- `have same property` - Assert multiple nodes share a property value
- `is above|below|left-of|right-of #id with gap=N`, `is vertically-centered-between #a,#b`, `is horizontally-centered-between #a,#b`; for edges `is horizontal` / `is vertical`

**Properties:**
- `size` - Node size as `WxH` (e.g., `120x60`)
- `x`, `y`, `center-x`, `center-y` - Position coordinates
- `width`, `height` - Individual dimensions
- (`type` is an EDGE property — `edge #a,#b has type=leadsto`; as a node
  selector the kinds are event, thing, concept, unresolved, boundary)

### Rule Extraction

Rules are extracted from specification markdown files using special code blocks:

~~~~md
```ipmdev-layout-rule
each type=event has size=120x60
all #thing1,#thing2 have same x
```
~~~~

Extraction is performed by:
```bash
make sync-test-cases
# or
go run ./cmd-dev/sync-test-cases --rm-extra
```

Rules are extracted to `tests/layout-gen-rules/*.dsl` and `tests/layout-gen-ext-rules/*.dsl` as part of the test-case synchronization process.

### Running Layout Tests

```bash
# Show fitness score only (default)
make layout-fitness

# Run with verbose output showing all rule applications
make layout-test

# Or use the tool directly
go run ./cmd-dev/layout-test-runner -v
```

**Fitness Score:**
The score is a SPEC LINE NUMBER, not a test count:
- on failure it is the spec line of the first failing case (the runner stops there);
- on success it is the spec line of the LAST case — so it grows as the catalogue
  grows and shrinks when the doc gets shorter, even with no behaviour change.

**Example Output** (`-v`, all passing):
```
✓ Test 1/94: one-event (line 61, 10 rules)
✓ Test 2/94: one-event-with-long-text (line 109, 5 rules)
✓ Test 3/94: two-events-connected (line 147, 5 rules)
...
FITNESS SCORE: 3182 (all 94 tests passed)
```

On the first failure it stops and prints:
```
FITNESS SCORE: <spec-line> (failed at test 12/94: <case-name>)

First failure at spec line <spec-line>:
Test: <case-name>
Error: rule 6 (line 67): event e1 has height=40, expected 80
```

**Debugging Failures:**
When a test fails, the output provides:
- the spec line where the case is defined, and the case name
- the failing rule's own spec line and its assertion message
- the regenerated `tests/<corpus>/<case>.layout.json` to inspect (rewritten on
  every run); `layout-debug --why/--facts` explains the placement
- Path to generated layout file for inspection
- Clear action: read spec at indicated line, implement that feature

### Rule Registry and Cumulative Testing

Rules are applied **cumulatively** as they are encountered:
1. Load all `.dsl` rule files in order
2. For each layout test:
   - Apply all rules registered up to that point
   - Report which rules pass/fail
   - Continue to next test
3. When a new rule is encountered (via its line number), add it to the registry
4. All subsequent tests are validated against the growing rule set

This approach ensures:
- Earlier tests validate basic behavior with fewer constraints
- Later tests validate complex scenarios with accumulated rules
- New rules don't retroactively invalidate earlier test cases

### Test Documentation

Generated markdown files (`.md`) include rule validation results:

```bash
make gen-test-md
# or
go run ./cmd-dev/gen-test-md --sources-config tests/sources.json
```

Each test's `.md` file shows:
- ✅ Rules that pass
- ⚠️ Rules that fail with error details
- `gl:` links to rule definitions in specs
- `gl:` links to extracted `.dsl` files

### Package Structure

**`pkg/layouttest`** - Layout validation framework
- `types.go` - Layout data structures (Point, Node, Edge, Layout)
- `rule.go` - Rule interface definition
- `registry.go` - RuleRegistry for cumulative rule management
- `selector.go` - Node selection logic (TypeSelector, IDSelector, etc.)

**`pkg/dsltorules`** - DSL parser and rule implementations
- `parser.go` - Parse DSL text into Rule objects
- `rules.go` - Rule implementations (PropertyRule, SizeRule, AlignmentRule, etc.)

**`cmd-dev/layout-test-runner`** - Test orchestration
- Loads test cases and rule files
- Applies rules cumulatively
- Reports fitness score and detailed results

### Development Workflow

Typical development cycle when implementing layout features:

1. **Run tests** to see current fitness score:
   ```bash
   make layout-fitness
   ```

2. **Identify next failure** - run verbose output to see which test fails:
   ```bash
   make layout-test
   ```

3. **Read specification** at the failing line number (shown in error output)

4. **Implement the feature** in the layout generator

5. **Re-run tests** to verify improvement:
   ```bash
   make layout-fitness
   ```

6. **Track progress** - fitness score should increase with each feature

**Adding New Rules:**

When extending the specification with new layout rules:

1. Edit `docs/dev/layout-gen/layout-alg.md` and add `ipmdev-layout-rule` blocks
2. Run `make sync-test-cases` to extract new rules
3. Run `make layout-test` to see if existing implementation satisfies new rules
4. Implement any missing features to pass the new rules

**Rule Syntax Evolution:**

Start with simple rules, extend as needed:
- MVP: `each type=X has property=value`, `all #id1,#id2 have same property`
- Extended: Add conditional selectors when specification requires them
- Keep rules declarative and readable - they are documentation
- Add new Rule types to `pkg/dsltorules` only when existing patterns insufficient

## Test Philosophy

### Automatic Coverage
All test cases defined in `sources.json` are automatically tested. Adding a new test case to the configuration immediately includes it in the test suite.

### Regression Detection
Changes that break parsing or layout generation are caught immediately by comparing against expected output files. Layout regression tests provide an additional layer of validation for algorithm behavior.

### Consistency
The same test data used by the generation tools (`sync-test-cases`, `gen-test-md`, `gen-test-doc`) is used by automated tests, ensuring consistency between development and validation.

### Documentation as Tests
Tests serve as executable examples of parser and layout behavior, providing living documentation. DSL rules in specifications serve as both documentation and executable constraints.

## Test Maintenance

### Updating Expected Outputs

When parser or layout behavior changes intentionally, regenerate in THIS order —
each step feeds the next, and the `_refs.json` hash maps only settle at the end:

```bash
make sync-test-cases          # 1. extract fixture text (.ipmt/.ipmt-invalid/.dsl) + _refs.json
make gen-invalid-sidecars     # 2. invalid lane: .parser.error.json / .validate.error.json
make layout-test              # 3. rewrites every corpus .layout.json
make gen-test-md              # 4. per-case .md
make refs-rehash              # 5. refresh the _refs.json "outputs" hashes
go run ./cmd-dev/sync-test-cases --check   # must exit 0
go test ./...
```

What each step does NOT do — the usual source of a half-regenerated tree:
- `sync-test-cases` writes only extracted fixture TEXT plus `_refs.json`. It does
  **not** parse: the `<case>.json` graph goldens come from `cmd/ipmt-parse`
  (`ipmt-parse --in <case>.ipmt > <case>.json`) and are only *compared* by
  `TestParseValidCases`.
- `.ipm.svg` files come from `cmd/ipmsvg-gen`; doc diagrams from `md-embed`.
- `refs-rehash` refreshes output hashes only; it defaults to the two layout
  corpora, so pass other dirs explicitly when they changed.
- A `.ipmt-invalid` case with NEITHER sidecar is silently skipped by
  `TestParseInvalidCases` — always run step 2 after adding one.

### Verifying Test Coverage

```bash
go run ./cmd-dev/sync-test-cases --check    # fixtures + _refs.json in sync (DIFF/EXTRA → exit 1)
go run ./cmd-dev/refs-rehash --check        # output hashes current
go run ./cmd-dev/gen-invalid-sidecars -check # invalid-lane sidecars current
```

Note `--check` verifies that extracted fixtures and `_refs.json` agree; it does
not assert that every case has a complete output set.

### Universal-invariant gate (layout-check)

Beyond the per-case rules, `make layout-check` sweeps both layout corpora with
`cmd-dev/layout-debug --check` for universal invariants (no node overlap, no edge
through a box, badge/chip clearances) and RATCHETS them against
`tests/layout-check-baseline.txt`: it fails if any file's finding count grows.
When a change legitimately reduces findings, run `make layout-check-baseline` and
commit the tightened baseline.

## Related Documentation

- [`docs/dev/test-data-structure.md`](./test-data-structure.md) - Test data organization and structure
- [`docs/dev/parser.md`](./parser.md) - Parser implementation details
- [`docs/layout-gen.md`](../layout-gen.md) - Layout algorithm details
- [`docs/ipmt-parser.md`](../ipmt-parser.md) - Parser capabilities and JSON schema
