# examples

The worked `ipmt` examples the [README](../README.md) and the tool docs run their commands
against — `deploy-incident`, `murder-full`, `murder-simple`, `two-containers`.

| File | Written by | What it is |
| --- | --- | --- |
| `<stem>.ipmt` | by hand | the example itself: source of truth, edit freely |
| `<stem>.layout.json` | `cmd/layout-gen` | **generated** — the engine's current layout for that example, committed so an engine change shows up as a diff on a real diagram. Do not hand-edit. |

Regenerate one snapshot:

```bash
go run ./cmd/layout-gen --in examples/murder-simple.ipmt --out examples/murder-simple.layout.json
```

The whole set is refreshed in one go by the dev distribution tooling, which also commits it on its
own (as `examples: refresh the layout snapshots (generated)`) rather than mixing it into
hand-written changes.

## They are fixture sources too

`tests/sources.json` globs `**/*.ipmt` into `tests/ipmt/files-ipmt`, where the parsed JSON, layout,
SVG and per-case `.md` are generated and committed. After editing an `.ipmt` here, refresh those —
the order matters, see [`docs/dev/testing.md`](../docs/dev/testing.md):

```bash
make sync-test-cases && make layout-test && make gen-test-md && make refs-rehash
go run ./cmd-dev/sync-test-cases --check   # must exit 0
```

The examples are also swept by the layout audit (`make layout-audit`, `AUDIT_PATHS`), so a change
to the engine reports what it did to each of them.
