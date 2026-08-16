# Changelog

Changes to the `ipm-tools` commands, to the `ipmt` language and the JSON shapes
they produce, and to the `ipm-rpc` server editors connect to. The VS Code
extension keeps its own changelog in
[`vscode-infinite-pm`](https://github.com/infinite-pm/vscode-infinite-pm/blob/main/CHANGELOG.md).

## 0.4.3 — unreleased

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
