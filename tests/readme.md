# Test Files

**DO NOT EDIT FILES IN THIS DIRECTORY DIRECTLY**

All test files in this directory are auto-generated and will be overwritten.

## Regenerating Tests

To regenerate all test files, run:

```bash
make sync-test-cases   # extract + parse fixtures per tests/sources.json
make gen-test-md       # per-case markdown with rendered diagrams
```

## Test Structure

- `ipmt/` - IPMT parser test cases: `ipmt-spec/` (extracted from the spec),
  `files-md/` (examples from docs), `files-ipmt/` (standalone `.ipmt` files),
  and `invalid/` (the ` ```ipmt-invalid ` negative-example lane)
- `layout-gen/`, `layout-gen-ext/` - layout corpora, with their DSL rule pins
  in `layout-gen-rules/` and `layout-gen-ext-rules/`
- `solver/` - node-kind solver round-trip cases

Changes to test cases should be made by:
1. Modifying the source documents (or parser/layout logic)
2. Running `make sync-test-cases && make gen-test-md` to regenerate the outputs

See [docs/dev/test-data-structure.md](../docs/dev/test-data-structure.md) for
the full file-naming and `sources.json` reference.
