# Changelog

Changes to the `ipm-tools` commands, to the `ipmt` language and the JSON shapes
they produce, and to the `ipm-rpc` server editors connect to. The VS Code
extension keeps its own changelog in
[`vscode-infinite-pm`](https://github.com/infinite-pm/vscode-infinite-pm/blob/main/CHANGELOG.md).

## 0.6.0 — 2026-11-10

**Breaking: the default node type of `ipmt` is now event.** An unmarked node is an
event; a thing needs `::t`, a concept `::c`; `::e` is optional. Text written for
0.4.x still parses, with no warning, but each unmarked thing is now read as an
event and its arrows change relation. Migrate once, before the first run of the
new tools, in each git repository that holds ipmt text:

```bash
go run github.com/infinite-pm/ipm-tools/cmd-dev/ipmt-migrate@v0.6.0 -root . -write
```

It adds `::t` to every unmarked thing in the tracked `.ipmt` and `.md` files and
changes nothing else; the result means the same under 0.4.x and 0.6.0. Run it
only while the text is still written for the old default. It does not reach ipmt
text inside program strings. See
[`docs/dev-tools/ipmt-migrate.md`](docs/dev-tools/ipmt-migrate.md).

- `ipm-validate` gets IPMV4.6 (warning): a comma inside an unquoted aliased label.
  In an edge line a comma separates targets, so `x::a it creates, links and closes
  items ::e` parsed as two nodes and left stranded ones behind with no hint. The
  finding suggests the quoted form. Quoted labels and ordinary target lists are not
  flagged. See [`docs/ipm-validator-rules.md`](docs/ipm-validator-rules.md).
- `ipm-validate` IPMV2.8: when the container a leads-to edge enters is itself a
  sub-stage, the suggestion now says to put the order on the sub-stages, not on the
  leaf events inside them.
- Layout: a participant is drawn where it is used. `layout-gen` and the flat
  renderer (the path `md-embed` takes) place a copy of a shared participant beside
  each event that uses it, where the alternative is an edge too long to read;
  `--duplicate=false` turns it off. The graph does not change: the parser, the
  validator and every `.ipm.json` still see one node. Each copy carries a place
  badge (2/3) at its bottom right, and `layout.Node.CopyOf` names the node it is
  another place of. Diagrams under 24 nodes are left alone.
- Layout: an edge tooltip (`e1 --"why"--> e2`) reaches the SVG as the edge group's
  `<title>`; it was dropped at the layout boundary before.
- `layout-gen --shells` and `--containers` reach the engine; they were accepted and
  silently ignored outside `--debug-json`.
- `ipmsvg-gen --node-ids` wraps each node in `<g class="node-group"
  data-node-id="…">`, so a host page can find a node by its id.
- The kind tag: `controller (c)` beside `controller ::t` gives one word two kinds
  without a language change. The tag is dropped from the drawn label. See "One
  word, two kinds: the kind tag" in [`docs/ipmt-spec.md`](docs/ipmt-spec.md).
- `ipmt` fence meta `union=false`: the block renders and validates, and tools that
  build a union of all diagrams leave it out.
- New packages for programs that build graphs: `pkg/ipm/graph` (the model under
  one import, with a `Builder`), `pkg/ipm/subgraph` (the cut around a seed set) and
  `pkg/ipm/refs` (a side-car that records what each node and edge was made from).
  See [`docs/graph-ops.md`](docs/graph-ops.md).
- `md-html`: a `head` config block (canonical URL, Open Graph, icons), a meta
  description on every page, and the alt text of an embedded diagram as the
  accessible name of the inlined `<svg>`.
- Tokens: a quoted node title takes its node's kind colour, where it took the
  generic string colour before. `palette.json` gains `fgLight` beside `fg`; every
  value has at least 4.5:1 contrast on its editor background.
- `ipm-rpc` hover text says that an unmarked node is an event and a thing needs `::t`.
- `ipmt-migrate -root` skips tracked symlinks, so a link to a flipped file no longer gets `::t` added to its events on a second pass.
- New dev command `regen-fixtures` (`make regen-fixtures`, `make regen-fixtures-run`):
  regenerates every test fixture in the right order after a grammar change and then
  reports which changed files were expected, which were already stale before the
  change (those are restored), which are new or deleted, and which docs only got
  new `md-embed` hash markers.

- `ipmt-migrate` gets `-flip` (adds `::t` to unmarked things and removes `::e` from
  events, proven by old file under the old default == new file under the event default)
  and `-check-manifest`; the new `ipmt-markers.manifest` lists the docs and fixtures
  that keep explicit markers, and `TestRepoManifest` fails if one loses them.

- This repo's own fixtures and docs were migrated to the event default with
  `ipmt-migrate -flip`, so no diagram here changed meaning. For Go callers,
  `parser.Options.DefaultNodeType` (`model.Thing`) still selects the old grammar; no
  command has a flag for it.

- `ipm-validate` IPMV2.7 reads a PARENT, not a pair of siblings. A parent
  event whose sub-events are all ordered by leads-to (a sequence) and one
  whose sub-events are not ordered at all (a set) are both whole statements
  and both silent; the warning now fires once per parent that is half of
  each, naming the sub-events left outside the order. A loose sub-event that
  carries a near-to to a sibling is declared parallel and is not named — one
  edge per loose part, where the per-pair rule cost n(n-1)/2 near-to edges
  for n parallel parts, a web of dotted lines telling a reader nothing the
  containing box had not already said. Every graph the old reading accepted
  is still accepted (a hand-written `a --- b` still exempts its pair), so
  nothing that passed starts failing; what changed is the message and the
  `suggest:` line. The code stays `IPMV2.7` — see
  [`docs/ipm-validator-rules.md`](docs/ipm-validator-rules.md) §3, and
  [`docs/how-to-model.md`](docs/how-to-model.md) for when a near-to earns its
  place at all.
- Layout: a thing that is part-of MORE chain events than the other things in
  its band takes the flank OPPOSITE them. Two things stacked in one band that
  both tie further down the spine could not both draw clean from one side —
  the higher one fanned across the other's edges (a person part-of every step,
  the shirts part-of two steps each: the person's ties crossed the shirts').
  The band's unique widest-spanning thing now moves to the free right flank;
  equal spans keep the canon (things left, concepts right). Fixture: "the
  chain-spanning thing takes the flank opposite its band rivals" in
  `docs/dev/layout-gen/layout-alg-ext.md`.
- Layout: a band member's ties fan from its FACING side. A thing on the row
  of an event it is part-of meets that event on the horizontal; its other
  part-of ties to events in the same direction now leave the same side and
  land on the events' facing sides, instead of dropping off its bottom onto
  the events' top corners (Patrick's second and third ties left his bottom
  and landed top-right of the events while the first left his side). Each
  join keeps the 150° cap and a clean straight, and a side takes at most
  two such arrivals; a steeper or third tie keeps its vertical exit. Nodes
  with no on-row event edge keep the previous vertical unification. Same
  fixture as above, plus "a band member's ties up the chain fan from its
  facing side too".
- Layout: a fan sharing one border keeps its approach order on both sides
  of the pinned (flow / on-row) port — the displaced end used to be nudged
  one step down regardless of which side of the pinned end it belonged
  to, so ties up a chain crossed their on-row edge at the exit.

## 0.4.2 — 2026-08-13

- `ipm.embedBuffer` accepts `tokensOnly: true`, which returns each block's
  `source` and `tokens` without laying out an SVG (110 ms → 35 ms on a
  3234-line, 95-block document; 926 KB → 130 KB). An editor coloring a live
  buffer needs tokens for the text as it stands on this keystroke, while the
  diagram can wait for a pause — this lets a client run the two on separate
  cadences. Every other field is unchanged, and a server that predates the
  flag ignores it and renders in full.
- Every shipping command answers `--version`, printing `<tool> <version>` taken
  from the build information the Go toolchain embeds — so a binary unpacked from
  a release archive can say what it is. Previously only `ipm-rpc` could be asked,
  because it is the one the VS Code extension interrogates; the other six exited
  with `flag provided but not defined: -version`.
- Note that the provenance stamp `md-embed` writes into generated SVGs stays
  `md-embed@dev` and is deliberately not this version: it marks which tool
  produced a diagram, and tying it to the release would put a version bump into
  the diff of every committed SVG.

## 0.4.1 — 2026-08-10

First public release. The code is considerably older than the tag and there is
no earlier release to compare against, so this entry describes what the release
contains rather than what changed in it.

- Seven commands ship as one toolset: `ipmt-parse`, `ipm-validate`, `layout-gen`,
  `ipmsvg-gen`, `md-embed`, `md-html` and `ipm-rpc`. What each does, and the
  pipeline they form, is in [`README.md`](README.md); the reference pages are
  indexed in [`docs/readme.md`](docs/readme.md).
- A tag publishes the whole toolset: one archive per platform holding all seven
  binaries — linux amd64/arm64, darwin amd64/arm64, windows amd64 — with
  `SHA256SUMS` and build-provenance attestations, on the
  [releases page](https://github.com/infinite-pm/ipm-tools/releases). The module
  is tagged too, so `go install github.com/infinite-pm/ipm-tools/cmd/<name>@v0.4.1`
  builds any single command. Go 1.25.4 or newer.
- `ipm-rpc` speaks plain LSP over stdio — diagnostics (parse errors and `IPMV*`
  validator findings), hover, semantic tokens, and the `ipm.embed` /
  `ipm.embedBuffer` / `ipm.check` workspace commands. It is not VS Code-specific;
  any LSP client can drive it. See [`docs/ipm-rpc.md`](docs/ipm-rpc.md).
- `md-embed --check` is the gate for a repository that keeps its diagrams in git:
  it exits non-zero when a committed SVG has drifted from the `ipmt` block it was
  rendered from, so a stale diagram fails CI instead of shipping.
