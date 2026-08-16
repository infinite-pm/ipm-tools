# The states corpus — every diagram the demo scenes type

Two tools turn a demo recording into a layout corpus of diagrams
**mid-construction**:

| tool | role |
|---|---|
| `gl:cmd-dev/ipm-rpc-tee` | sits between the editor and `ipm-rpc`, forwards everything verbatim, writes down every buffer state that crosses the wire |
| `gl:cmd-dev/states-curate` | turns a raw capture into a deterministic, content-addressed corpus with a regression tripwire |

Driven from the recorder repository, which owns the scenes and the result:

```bash
cd ../vscode-infinite-pm-dev
make states                      # every scene, camera off
make states SCENES=ipmt-preview  # one
make states-curate               # re-curate an existing raw capture
```

## Why capture at all

Every layout gate in this repository judges **finished, authored** diagrams:
the fitness corpora are written by hand to pin a rule, the doc diagrams are
documentation, `examples/` is curated. Nothing covers a diagram *being built* —
which is what a user looks at almost all of the time, since the extension
re-renders on every keystroke.

Those states already exist. The extension sends `textDocument/didChange` per
keystroke with the WHOLE buffer (the server advertises sync=Full), so every
intermediate document is complete and replayable, and it is already on the
wire during a recording. Capturing is a matter of writing it down.

Measured on one scene (`ipmt-preview`, 2026-08-16): **88 states that lay out,
5 the parser rejects**, from a graph of 9 to 14 nodes. 55 of the 88 carry a
universal-invariant finding. State 41 of that scene is the document with
`Ea` on its last line — "Earth", two characters in. No authored fixture
contains anything like it.

## The capture: why a proxy

`ipm-rpc` is a SHIPPING binary and carries no dev-only surface, the same way
`cmd/layout-gen` carries no debug views (`gl:docs/dev/layout-gen/layout-debug.md`).
A proxy keeps it that way, and nothing dev-only can reach a Marketplace
`.vsix`.

It also needs no change anywhere else, because every hook already exists: the
recorder honours `DEMO_IPM_RPC` when it writes `ipm.serverPath`, the extension
spawns the server with no `env` override so it inherits the editor's
environment, and the container run already forwards `-e` variables and mounts
`out/` writable.

```
VS Code ──stdio──► ipm-rpc-tee ──stdio──► ipm-rpc
                        │
                        └──► out/states-raw/session-<pid>.jsonl
```

```bash
IPM_TEE_REAL=/path/to/ipm-rpc IPM_TEE_OUT=/path/to/capture ipm-rpc-tee --stdio
```

Three properties it is built to keep, each with a test that would catch its
loss (`gl:cmd-dev/ipm-rpc-tee/tee_test.go`, which drives a real `ipm-rpc`):

- **Invisible.** A message is forwarded FIRST and observed after, so the
  session's behaviour never depends on the recorder. `initialize` through the
  tee is byte-identical to `initialize` straight to the server.
- **Forgiving of arguments.** LSP clients append their own flags
  (`vscode-languageclient` adds `--stdio` unconditionally). A strict parser
  would exit before the session started — a dead editor, not a missing
  capture — so unknown arguments are passed straight to the proxied server.
- **Never fatal.** A capture that cannot be written logs once and the session
  continues as a plain passthrough.

Each line records the cadence, which is what separates *what the typist saw*
from *what the engine actually ran*:

| cadence | means |
|---|---|
| `open` / `change` | a complete buffer — a state |
| `embed` | a full `ipm.embedBuffer`: the layout engine ran on the state before it |
| `embed-tokens` | a `tokensOnly` call: colouring only, no layout |
| `save` | `ipm.embed` wrote `_ipm/` to disk |
| `diagnostics` | the server's verdict on the state before it, message text included |

A states run also sets `ipm.liveRefreshDebounceMs: 0` (documented and unit
tested in the extension as "render on every change"), so during a capture the
engine really does lay out every keystroke rather than only the pauses.

## The corpus

`states-curate` writes, per scene:

```
<scene>/<hash>.ipmt          states the engine lays out
<scene>/invalid/<hash>.ipmt  states it rejects
<scene>/signatures.txt       state hash → layout hash, nodes, edges, bounds
<scene>/manifest.json        typing order, cadence, source, errors
```

- **Content-addressed**, because a corpus must be stable under re-capture: a
  take that types the same thing produces the same filenames and no diff, so
  `git diff` shows real change rather than the fact that a recording was
  re-run. The timestamps and duplicates stay in the raw capture and never
  reach the corpus.
- **Deduplicated by LAYOUT** by default: typing a label one character at a
  time produces states whose graphs differ but whose layouts do not, and
  keeping them buries the states that differ. `--all` keeps every distinct
  source.
- **Rejected states are kept, separately.** A half-typed line is a state users
  really pass through — `--::P--> Life` is invalid until `Life` gets its
  `::e`. They belong to the parser/validator lane and to a "never panic"
  gate, not to the layout sweep, so they are stored apart and excluded from
  `signatures.txt`.
- Markdown buffers contribute their **blocks**, decided by `gl:pkg/mdembed`
  (the same code the editor renders through), identified by marker id so a
  state matches the `_ipm/<file>/<id>.ipm.svg` it produced.

## Running regressions over it

Three gates, cheapest first.

**1. The signature tripwire.** `signatures.txt` records what each state lays
out to, without storing geometry. Regenerate after an engine change and the
diff names exactly the states that moved — no old engine, no rendering:

```bash
cd ../vscode-infinite-pm-dev && make states-curate
git diff demo/states                     # every state whose layout hash moved
```

Verified by construction: perturbing one engine constant (`layout7.RowGap`
60 → 80) moves all 88 layout hashes while every state hash stays put.

**2. The invariant ratchet**, the same universal invariants the fixture
corpora use:

```bash
go run ./cmd-dev/layout-debug --check --totals ../vscode-infinite-pm-dev/demo/states/ipmt-preview
# 88 files swept, 55 with findings — 0 overlaps, 47 through-node family, 16 crossings
```

**3. The audit**, for what actually changed and how it looks
(`gl:docs/dev-tools/layout-audit.md`):

```bash
go run ./cmd-dev/layout-audit --old <ref> ../vscode-infinite-pm-dev/demo/states/ipmt-preview
```

## What this corpus is not

**It is not a fitness catalogue.** A fixture in `tests/layout-gen*` pins a RULE
authored in `layout-alg.md`; a captured state pins nothing — it is evidence,
not intent. Adding hundreds of unexplained cases there would dilute exactly
the property that makes that corpus worth having.

When a captured state does expose a bug, the existing rule applies
(`gl:CLAUDE.md`: *an example that exposed a rule gets PINNED before the session
ends*): reduce it to an abstract twin (`tA`/`e1`/`cX` — the catalogue never
speaks domain vocabulary), add a failing `ipmdev-layout-rule` case, confirm it
is RED, fix, and the captured state goes back to being an ordinary member of
the sweep.

**It is narrow in model-space.** Those 88 states are one graph of 9–14 nodes
in ~24 distinct shapes. Deep in state-space, narrow in model-space: a superb
source of transitional diagrams and a poor source of diverse ones. Adding
scenes for coverage would corrupt the demo's purpose — every graph there is
one of ours, used verbatim.

**It does not live here.** The corpus is committed in the recorder repository
(`vscode-infinite-pm-dev/demo/states/`), because it belongs to the demo and
changes when a scene changes; ipm-tools is published and must not grow a
dependency on an internal sibling. Every tool above takes it as a path.
