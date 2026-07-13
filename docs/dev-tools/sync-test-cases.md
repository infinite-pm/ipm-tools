# sync-test-cases

Extract test cases from source documentation files.

## Overview

The `sync-test-cases` tool extracts fixture sources per `tests/sources.json`: ` ```ipmt ` and ` ```ipmt-invalid ` blocks from markdown, standalone `.ipmt` files, `ipmdev-layout-rule` blocks (one rule per `.dsl`) and solver `# given` / `# then` groups. It processes files according to the `sources.json` configuration.

## Usage

```bash
go run ./cmd-dev/sync-test-cases [flags]
```

## Flags

- `--check` - Dry-run: verify fixtures are up-to-date without writing files
- `--rm-extra` - Remove extra/orphaned files in destination directories
- `--filter` - Process only a single destination stem (must exist in exactly one `_refs.json`)
- `--max-lines` - Maximum lines per snippet, fail-fast (default: `100`)
- `--v` - Verbose output

## How It Works

1. Reads `sources.json` to find source documentation files
2. Extracts code blocks marked with the appropriate fence type (e.g., `` ```ipmt ``)
3. Generates fixture files (`.ipmt`, `.ipmt-invalid`, `.dsl`, and the solver's `.solved.ipmt` / `.defaults.ipmt`) in the test directories. It does NOT produce `.json`, `.layout.json`, `.ipm.svg` or `.md` — those come from `cmd/ipmt-parse`, `layout-test-runner`, `ipmsvg-gen` and `gen-test-md`
4. Creates `_refs.json`: source locations plus `source_sha1`, per-case `content_sha1`, heading/section/tags, and an `outputs` hash map of the generated siblings (which is why `refs-rehash` — or a second run — is needed after regenerating outputs)

## Example

```bash
# Extract test cases and write fixtures
go run ./cmd-dev/sync-test-cases

# Check coverage without modifying files
go run ./cmd-dev/sync-test-cases --check

# Sync and prune orphaned fixtures (the `make sync-test-cases` target)
go run ./cmd-dev/sync-test-cases --rm-extra
```

## Related Tools

- [gen-test-doc](gen-test-doc.md) - Generate documentation from fixtures

## Implementation

Source: [cmd-dev/sync-test-cases/main.go](../../cmd-dev/sync-test-cases/main.go)
