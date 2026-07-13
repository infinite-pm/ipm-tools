# slides-copy

Copy selected ipmt-embedded SVGs to flat, sequentially named slide files —
the tool behind carousel-style galleries (e.g. the infinite.pm landing page,
built in the sibling `infinite-pm-web` checkout).

## Purpose

Embedded diagrams live under a doc's `_ipm/<doc>/<id>.ipm.svg` tree with
three-character marker ids (`100`, `110`, `120`, …) that don't line up with a
carousel's `01..NN` slot names. `slides-copy` reads a small JSON config that
maps each source SVG to its destination slide name and copies them into a
single `out_dir`.

## Config

Paths are resolved relative to the config file's own directory, so a config
in one repo can pull diagrams from a sibling checkout:

```json
{
  "out_dir": "docs/slides",
  "slide": [
    { "src": "../ipm-intro/_ipm/README/100.ipm.svg", "out": "01.ipm.svg" },
    { "src": "../ipm-intro/_ipm/README/120.ipm.svg", "out": "02.ipm.svg" }
  ]
}
```

## Run

```bash
go run ./cmd-dev/slides-copy -config slides.conf.json [-check] [-prune] [-verbose]
```

`-check` is a dry run: it validates every source path and copies/deletes
nothing.

With `-prune`, any `*.ipm.svg` in `out_dir` the config does not list is
deleted, keeping the slide set an exact mirror of the config (`out_dir` is
expected to be a dedicated, fully generated directory).
