# ipm-validate

Command-line tool that runs graph validations against an ipm
graph. Use it to catch structural problems in your `.ipmt`
files, in markdown docs that embed `ipmt` blocks, or in graphs
produced by other tools (graphJSON).

For the full catalog of rules with severities and rationale, see
[ipm-validator-rules.md](ipm-validator-rules.md). For heuristic
modelling-tip checks that are *not* part of this strict tool,
see [ipm-modeling-tips.md](ipm-modeling-tips.md).

## What it checks

13 graph-validation rules (mix of error, warning, info). The
full list is in
[ipm-validator-rules.md](ipm-validator-rules.md); a quick
summary:

- Structural invariants (errors): self-loop, duplicate edge per
  pair, SST type-pair conformance.
- Implicit-relation redundancy (warning): parent thing's
  part-of-event edge when a sub-thing already participates.
- Acyclicity (errors): PartOf is a DAG, LeadsTo is a DAG,
  happens-before (the composition of the two) is acyclic.
- Temporal / shape (warnings): sibling sub-events of the same
  parent should declare a leads-to ordering; a leads-to edge
  should not dip into a container that the predecessor is
  unrelated to; leads-to connects WHOLE (outermost) events, not
  a part-of sub-part (IPMV2.9).
- Hygiene (info): stranded thing, stranded event.
- Undecided kinds (IPMV1.5): Unresolved (grey) node kinds are
  flagged as a warning by default, promoted to an error under
  `--strict-undecided` (the publish gate).

Every rule is a deterministic graph predicate. False positives
are not expected; if you hit one, please file an issue.

## Install / build

`ipm-validate` is part of this repository. Build or run with
the usual Go workflow:

```bash
go build -o ./bin/ipm-validate ./cmd/ipm-validate
# or run directly:
go run ./cmd/ipm-validate --help
```

## Usage

```
ipm-validate [--in <file>] [--json] [--in-json|--in-md] [--list-checks] [--strict-undecided]
```

Reads from stdin when `--in` is omitted.

### Input modes

The tool auto-detects the input mode from the file extension,
or you can force it with a flag.

| Mode | Detected by | What it does |
|---|---|---|
| **ipmt** (default) | no flag, no recognised extension | Parse with `pkg/ipm/parser`, run checks. |
| **markdown** | `--in-md` or `.md` extension | Extract every ` ```ipmt ` fenced block, parse and validate each independently, adjust finding line numbers so they point at the markdown file. |
| **graphJSON** | `--in-json` or `.json` extension | `json.Unmarshal` into an `IpmGraph` (e.g. output from `cmd/ipmt-parse`), run checks. |

### Output

By default, findings print to stderr in compiler format:

```
demo.md:42:17: warning: IPMV2.7: sibling sub-events "edit-loop" and "compile-loop" of "workflow-001" have no declared leads-to order
  suggest: add "edit-loop" --> "compile-loop" (or reverse), or mark them parallel with "edit-loop" --- "compile-loop"
```

With `--json`, findings emit as a JSON array suitable for
editor integration:

```bash
ipm-validate --in foo.ipmt --json | jq '.[] | select(.severity == "error")'
```

### Exit codes

| Code | Meaning |
|---|---|
| 0 | No findings. |
| 1 | Warnings or info only. |
| 2 | Parse or read error (could not run the checks). |
| 3 | At least one error-severity finding. |

CI integration: treat exit codes 0 and 1 as "tool ran
successfully" and inspect the JSON for errors, OR use the exit
code directly to fail builds only on errors (`!= 0 && != 1`).

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--in <file>` | _stdin_ | Input file path. |
| `--in-json` | false | Force graphJSON input mode (overrides extension). |
| `--in-md` | false | Force markdown input mode (overrides extension). |
| `--json` | false | Emit findings as JSON instead of compiler-style text. |
| `--list-checks` | false | Print one line per registered check (code + description) and exit. |
| `--strict-undecided` | false | Treat Unresolved (grey) node kinds as errors (publish gate). An `unresolved` or `defaults` flag (fence meta or `# ipmt:` pragma) opts that block/file out of the promotion. |

## Examples

### Validate a single `.ipmt` file

```bash
ipm-validate --in examples/murder-simple.ipmt
```

### Validate every `.ipmt` example in the repo

```bash
find examples docs -name '*.ipmt' -print0 \
  | xargs -0 -n1 ipm-validate --in
```

### Validate a markdown doc with embedded `ipmt` blocks

```bash
ipm-validate --in docs/example.md
```

The tool extracts each ` ```ipmt ` block, parses it
independently (so name re-use across blocks is fine), and
reports any findings with line numbers pointing at the
markdown file's line — the same way a typical linter
integrates with editor `quickfix` lists.

### Round-trip ipmt → graphJSON → validate

```bash
ipmt-parse --in foo.ipmt --pretty=false \
  | ipm-validate --in-json
```

Useful when a separate tool produced JSON and you want to
verify it before consuming it.

### Validate JSON from any source

```bash
ipm-validate --in graph.json
ipm-validate --in graph.json --json | jq '. | length'
```

### List registered checks

```bash
ipm-validate --list-checks
```

Output is `code<TAB>description`, one per line. Machine-readable
and stable.

### Pipe shell-script integration

```bash
if ! ipm-validate --in foo.ipmt > /dev/null; then
  case $? in
    1) echo "ipmt has warnings (non-blocking)" ;;
    2) echo "ipmt failed to parse" ;;
    3) echo "ipmt has hard errors" ;;
  esac
fi
```

## What it doesn't do

By design, `ipm-validate` does **not** run:

- **Heuristic / fuzzy checks** — see
  [ipm-modeling-tips.md](ipm-modeling-tips.md). Those are
  catalogued separately so this tool can stay
  false-positive-free.
- **Cross-block / cross-file checks** — name collisions
  across multiple ipmt blocks or files belong in the
  `ipm-collection` tool, not here.
- **Parser-only syntactic rules** — identifier shape, tooltip
  quoting, three-dash near-to, etc. The parser enforces these
  at parse time; no point in re-running them.
- **Layout validation** — positional / grid-alignment checks on
  rendered SVG layouts are out of scope; this tool validates graph
  semantics only.

## Library use

The validator is also available as a Go package for
programmatic use:

```go
import (
    "github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
    "github.com/infinite-pm/ipm-tools/pkg/ipm/validate"
)

g, _ := parser.ParseIPMTBytes(src, "in-memory")
findings := validate.Run(g)
if validate.HasErrors(findings) {
    // ...
}
```

Each individual check is exposed as a type implementing
`validate.Check` (`Code() / Description() / Run(*IpmGraph)`).
`validate.AllChecks()` returns the canonical list.

## References

- [ipm-validator-rules.md](ipm-validator-rules.md) — the rule
  catalog with severities and rationale.
- [ipm-modeling-tips.md](ipm-modeling-tips.md) — heuristic
  modelling checks that are NOT in this tool by design.
- [ipmt-spec.md](ipmt-spec.md) — the ipmt language spec.
- [ipmt-parser.md](ipmt-parser.md) — what the parser enforces
  (and the validator does not need to re-enforce).
