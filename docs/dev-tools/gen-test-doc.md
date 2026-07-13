# gen-test-doc

Generate comprehensive test documentation from sources.json.

## Overview

The `gen-test-doc` tool creates markdown documentation files for each test directory, including:
- IPMT source code
- Links to all generated files (JSON, layout, SVG)
- Embedded SVG previews
- Organized by source file with table of contents

## Usage

```bash
go run ./cmd-dev/gen-test-doc [flags]
```

## Flags

- `--sources-config` - Path to sources.json config file (default: `tests/sources.json`)
- `--out-dir` - Output directory for markdown files (default: `temp/test-docs`)

## Output

Generates one `<test-dir>.md` per top-level destination defined in
`sources.json` (the first path segment of each entry's `destination`),
plus a `README.md` index. The exact set therefore tracks `sources.json`
and changes when it does. At the time of writing the destinations are,
for example:
- `README.md` - Index of all test documentation
- `ipmt.md` - IPMT test cases
- `layout-gen.md` - Layout generation test cases
- `layout-gen-rules.md` - always empty (0 cases): the rule corpora hold `.dsl` pins and no `.ipmt` sources
- `layout-gen-ext.md` / `layout-gen-ext-rules.md` - extended layout cases
- `solver.md` - node-kind solver round-trip examples

Each generated file includes:
- Compact metadata line with source and output file links
- IPMT code blocks
- SVG diagram previews (when available)

## Example

```bash
# Generate all test documentation
go run ./cmd-dev/gen-test-doc

# Use custom paths
go run ./cmd-dev/gen-test-doc --sources-config tests/sources.json --out-dir temp/test-docs
```

## Related Tools

- [sync-test-cases](sync-test-cases.md) - Extract test cases from source docs

## Implementation

Source: [cmd-dev/gen-test-doc/main.go](../../cmd-dev/gen-test-doc/main.go)
