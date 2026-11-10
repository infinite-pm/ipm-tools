# palette-page

`gl:cmd-dev/palette-page` writes one HTML page that shows every colour of the ipmt
palette, `gl:pkg/ipmtokens/palette.json`. Look at it before a changed colour goes
anywhere: into the editor extension, into a page, into a figure.

```bash
make palette-page                                  # writes temp/palette.html
go run ./cmd-dev/palette-page --out some/page.html
```

## The two values of a rule

| Field | Where it shows | Background |
|---|---|---|
| `fg` | a dark editor theme | `#1f1f1f` |
| `fgLight` | everything white: a light editor theme, the pages `md-html` writes, and the figures other tools draw from the palette | `#ffffff` |

`fg` keeps at least 4.5:1 against its background. `fgLight` holds the colours of the
figures and of the diagrams, so the source in a light editor looks like the diagram
beside it. That is a look, not a contrast rule: an orange dark enough for 4.5:1 on white
is a brown. The values are pinned by a test (`TestPalette_LightColoursDoNotMove`), so a
change is a decision about the figures, made on purpose and looked at on this page.

## What the page shows

- A sample of ipmt source, beside the diagram the sample draws:
  first on white, then in its own section as a dark editor shows it. This is the place to see whether an event in the source and an
  event in the picture still look like the same thing.
- A table of every rule with its two values and the contrast of each. A red frame
  marks a value below 4.5:1.
- The diagram's own colours and text weight per node kind, read from `gl:pkg/ipmsvg`
  (`KindStyle`), not from the palette. Only an event is written bold.
