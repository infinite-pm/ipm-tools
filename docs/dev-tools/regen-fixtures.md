# regen-fixtures

One command that regenerates every test fixture after a grammar or engine change
and reports what changed. Before it existed the order lived in people's heads,
and a wrong order cost two review rounds.

```
make regen-fixtures        # prints the ordered plan, runs nothing
make regen-fixtures-run    # runs it and prints the report
```

## When to use

After a change to the parser, the layout engine, the renderer, the docs that
feed `tests/sources.json`, or the default node type. Run it on a tree whose only
changes are under `tests/` and `docs/` (a half-migrated tree is fine).

## The sequence

Each step runs with `GOWORK=off`; the first non-zero exit stops the run and
names the step (exit status 3).

1. `make gen-test-md` - per-case `.md` pages from `tests/sources.json`.
2. `make gen-invalid-sidecars` - `.parser.error.json` / `.validate.error.json`.
3. `make sync-test-cases` - copy and prune the cases from the docs.
4. `ipmt-parse --in X.ipmt` over `tests/ipmt/*` and `tests/layout-gen*`: the parser goldens `X.json`.
5. `layout-gen --in X.ipmt --out X.layout.json` over `tests/ipmt/*`.
6. `ipmsvg-gen --in X.ipmt --out X.ipm.svg` over `tests/ipmt/*`.
7. `make gen-test-md` again - the pages embed the files made in 4-6.
8. `make refs-rehash` - the `outputs` SHA1 maps in the `_refs.json` files.
9. `make sync-test-cases`, twice (the second run must change nothing).
10. `sync-test-cases --check`.
11. `md-embed --root . --no-prune` - rehashes the `<!-- ipm-svg ... hash= -->`
    markers in the docs. Without this step docs whose fences changed (for
    example the default node type) say STALE in `md-embed --check`. It is a
    rehash; if it also changes an SVG under `_ipm/` the report shows it as OTHER.
12. `golden-identity -root . -base SHA` - only with `-base SHA`.
13. `go test ./cmd-dev/... ./pkg/...` - skipped with `-no-test`.

Files are written only when their content differs, so an unchanged file keeps
its timestamp and does not show in `git status`.

## Flags

- (none) - same as `-dry-run`: print the plan and how to run it.
- `-dry-run` - print the ordered plan, run nothing.
- `-run` - run the sequence. Refuses a tree with changes outside `tests/` and
  `docs/`, and prints the `git status --porcelain` counts first.
- `-keep-stale` - leave STALE-AT-BASE files as regenerated (default: restore to HEAD).
- `-root DIR` - repository root (default `.`).
- `-base SHA` - add the `golden-identity` step against this commit.
- `-no-test` - skip the final `go test`.

Exit 0 on success (also with STALE-AT-BASE files), 3 when a step failed, 2 for a
refused or unreadable tree.

## The report

After the run the tool reads `git status` and sorts every changed path by its
case stem (directory plus file name without `.layout.json`, `.ipm.svg`, `.json`,
`.parser.error.json`, `.validate.error.json`, `.ipmt`, `.md`, `.dsl.json`, ...;
so `tests/ipmt/files-md/a.layout.json` and `tests/layout-gen/a.ipmt` each have
their own stem). Lists are capped at 40 lines.

- EXPECTED - a generated file whose source (same stem `.ipmt`, `.md` or
  `.dsl.json`) is new or changed; also untracked files that share a stem with a
  tracked file, and `_refs.json` files next to expected files. Only the count is
  printed.
- STALE-AT-BASE - a generated file whose source did not change: it was already
  out of date at the base commit. Reported, and restored to HEAD unless
  `-keep-stale` (fix those in their own change, not in a grammar change).
- NEW-CASE - an untracked file whose stem has no tracked sibling.
- DELETED - a file removed by the run.
- DOC-REHASH - a doc outside `tests/` that only got new `md-embed` hash markers.
- OTHER - changed outside `tests/` and not a rehashed doc (for instance an SVG
  that `md-embed` re-rendered): stop and look.
