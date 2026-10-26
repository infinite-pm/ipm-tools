# golden-identity

Proof that a regeneration of the test fixtures changed no graph.

## golden-identity

When the parser default or the source text of the fixtures changes, every
parser golden (`.json`) changes too, because it embeds the source text and the
byte offsets. `golden-identity` shows that this is all that changed: the nodes,
edges, types and layout coordinates are equal to the ones at a base commit.

### Usage

```bash
go run ./cmd-dev/golden-identity -root . -base <sha>
```

It takes every file under `tests/` that differs between `<sha>` and the working
tree (`git diff --name-status`, plus untracked files) and checks it by the table
below. It prints the `FAIL path: what` lines and the `DEDUPED`, `RENUMBERED`,
`DELETED-SOURCE`, `NEW-SOURCE` and `NEW-CASE` lines (sorted), a count line
`compared N, renumbered N, deduped N, new source N, deleted source N, not compared N, new case N`,
then `golden-identity ok` (exit 0) or `golden-identity FAIL n` (exit 1). An
unreadable root or a bad `-base` exits 2.

### Rules

Dropped before comparing, at every level: `ipmt` (source text), `positions`
(the offset maps under `src`), `source_sha1`, `content_sha1`, `outputs`.

The comparison is by content, per group: the kind (parser golden json,
`.layout.json`, `.ipm.svg`) over all directories. The generator numbers and
merges cases (`-02`, `-03`) and dedups across directories, so a path can be
deleted or hold another case, or a case can move to another directory, without
any graph changing.

| File | Rule |
|---|---|
| parser golden `.json` (top-level `nodes`/`edges`), `.layout.json`, `.ipm.svg` (bytes) at a path that exists at base and now | equal normalized content: compared |
| a base file whose path is deleted | content exists now: its content must exist in a current file of the same kind, in any directory: `DEDUPED path`, else `FAIL path: content lost` |
| a base file whose content at its path changed | content exists now (as above) and content existed at base: the current content must exist in a base file of the same kind, in any directory: `RENUMBERED path`, else `FAIL path: content lost` or `FAIL path: new content` |
| a new or untracked golden | content existed at base: its content must exist in a base file of the same kind, in any directory: `RENUMBERED path`, else `FAIL path: new content` |
| `.parser.error.json` | by path: `error` and `message` equal to base (start and end offsets ignored), else `FAIL path: error message changed (old: ... new: ...)`; deleted or new is FAIL |
| a new or untracked golden (parser `.json`, `.layout.json`, `.ipm.svg`) whose stem has a new or untracked `.ipmt` in the same directory | `NEW-CASE path` (a valid fence never extracted before; fixtures owed), not a FAIL; without that sibling the row above applies |
| `.validate.error.json` | by path: the sorted list of (`code`, `message`) of `errors` equal to base (line, column and other fields ignored), else `FAIL path: findings changed (...)`; deleted or new is FAIL |
| `_refs.json`, `.md`, `.ipmt`, `.ipmt-invalid`, `.dsl`, `.solved.ipmt`, `.defaults.ipmt` | never FAIL: deleted `DELETED-SOURCE path`, new or untracked `NEW-SOURCE path` (the reviewed list for the commit body), changed counted as "not compared" |
| unreadable json, json that is neither parser golden nor layout, any other file | FAIL |

The kinds never mix: a `.layout.json` content equal to a parser golden content
does not match it.

A fixture change the tool cannot explain stops the run; it is reported, not
forced.
