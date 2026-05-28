# Inline ipmt highlighting — colors & how to use them

Color words inside Markdown **prose** with the ipmt palette — show `::e`
in orange, paint the word "orange" orange, a node name in its kind
color — using one marker:

```
<!--ipmt:as-token:NAME-->`text`
```

It paints `text` in the style of the token `NAME` (see the table). The
text need not be valid ipmt — `NAME` decides the color/weight, the text
is just what's shown. Rendered by `md-html` and the VS Code markdown
**preview**; in any other Markdown renderer the comment is invisible and
the text shows as plain inline code (graceful fallback).

Companion to [`md-html.md`](./md-html.md).

## How it works

`<!--ipmt:as-token:NAME-->` immediately before a backtick code span makes
that span render as `<span class="ipm-NAME">text</span>` — the ipmt
palette color, no grey code background. The comment itself is consumed
(never shown).

`NAME` is a token style from the table below — single-sourced from
`pkg/ipmtokens/palette.json`.

> **Gotcha:** no source line may start with `<!--`. CommonMark parses a
> line beginning with `<!--` as a block-level HTML comment and swallows
> the rest of the line — and `md-html` refuses to publish such a page,
> listing the offending line numbers. Put a word before the marker, or keep
> the paragraph on one line. (Soft-wrapped continuation lines count too.)

## Token names & colors

Every style `md-html` and the preview can emit. Use any as
`<!--ipmt:as-token:NAME-->` `text`. The swatch column shows the name
painted in its own style.

### Node kinds — e / t / c

The **From** / **Pick** columns show a *valid* ipmt fragment that
contains each style and the slice it is — i.e. exactly what
`as-token:NAME` reproduces. (`From` is always valid ipmt: a full
declaration, never a lone `::e`.)

| `NAME` | What it is | From (valid ipmt) | Pick | hex · weight |
| --- | --- | --- | --- | --- |
| `e-title` | event title (no alias) | `e1 ::e` | `e1` | `#ff8000` · normal |
| `e-marker` | the `::e` symbol | `e1 ::e` | `::e` | `#ff8000` · **bold** |
| `e-alias` | the alias name itself | `e1 ::e n::a` | `n` | `#ff8000` · normal |
| `e-title-aliased` | event title of a node that has an alias | `e1 ::e n::a` | `e1` | `#ffaa55` · normal |
| `t-title` | thing title (no alias) | `t1 ::t` | `t1` | `#4d8a3a` · normal |
| `t-marker` | the `::t` symbol | `t1 ::t` | `::t` | `#4d8a3a` · **bold** |
| `t-alias` | the alias name itself | `t1 ::t n::a` | `n` | `#4d8a3a` · normal |
| `t-title-aliased` | thing title of a node that has an alias | `t1 ::t n::a` | `t1` | `#82b366` · normal |
| `c-title` | concept title (no alias) | `c1 ::c` | `c1` | `#6c8ebf` · normal |
| `c-marker` | the `::c` symbol | `c1 ::c` | `::c` | `#6c8ebf` · **bold** |
| `c-alias` | the alias name itself | `c1 ::c n::a` | `n` | `#6c8ebf` · normal |
| `c-title-aliased` | concept title of a node that has an alias | `c1 ::c n::a` | `c1` | `#9bb4d6` · normal |

### Arrows / relations — L / P / X / N

| `NAME` | What it is | Swatch | hex · weight |
| --- | --- | --- | --- |
| `L` | leads-to arrow (`-->`, explicitly `--::L-->`) | <!--ipmt:as-token:L-->`-->` | `#ff8000` · bold |
| `P` | part-of arrow (`--::P-->`) | <!--ipmt:as-token:P-->`--::P-->` | `#4d8a3a` · bold |
| `X` | expresses arrow (`--::X-->`) | <!--ipmt:as-token:X-->`--::X-->` | `#6c8ebf` · bold |
| `N` | near-to arrow (`---`) | <!--ipmt:as-token:N-->`---` | `#aaaaaa` · bold |
| `relation` | the `::L`/`::P`/`::X`/`::N` letter in an arrow | <!--ipmt:as-token:relation-->`::P` | `#b08fff` · normal |

(Plain untyped `arrow` grey `#d4d4d4` is omitted — the tokenizer always
assigns one of L/P/X/N, so it never occurs.)

### Markers & text

| `NAME` | What it is | Swatch | hex · style |
| --- | --- | --- | --- |
| `type-marker` | a `::a` / `::tip` marker | <!--ipmt:as-token:type-marker-->`::a` | `#b08fff` · normal |
| `tooltip` | a quoted node- or edge-tooltip string (`--"…"-->`, or on a node) | <!--ipmt:as-token:tooltip-->`"tip"` | `#b58900` · italic |
| `string` | any other quoted string (fallback — not a tooltip/comment) | <!--ipmt:as-token:string-->`"text"` | `#ce9178` · normal |
| `comment` | an ipmt comment | <!--ipmt:as-token:comment-->`comment` | `#808080` · italic |
| `unresolved` | an undecided-kind (`::?`) node in prose | <!--ipmt:as-token:unresolved-->`deploy` | `#8a8a8a` · normal |

## Examples

- Legend line: the <!--ipmt:as-token:e-marker-->`::e` marker means an event (<!--ipmt:as-token:e-title-->`orange`), <!--ipmt:as-token:t-marker-->`::t` a thing (<!--ipmt:as-token:t-title-->`green`), <!--ipmt:as-token:c-marker-->`::c` a concept (<!--ipmt:as-token:c-title-->`blue`).
- Relations: a <!--ipmt:as-token:L-->`-->` leads-to arrow is orange; a <!--ipmt:as-token:P-->`--::P-->` part-of arrow is green; <!--ipmt:as-token:N-->`---` near-to is grey.
- A node name in its kind color: the event <!--ipmt:as-token:e-title-->`swapT` leads to the next state.
- Color a plain word: an <!--ipmt:as-token:e-title-->`event` is fast; a <!--ipmt:as-token:t-title-->`thing` is slow.

## See also

- **A whole valid inline ipmt expression** (per-token coloring, not one
  style): use a bare `<!--ipmt-->` before the code, e.g.
  `<!--ipmt-->` `Alice ::e --> bob ::e`.
- **Deriving a style from a fragment in context** (advanced, not
  shipped) — `ipmt:as-from`.

The palette is single-sourced from `pkg/ipmtokens/palette.json`; see the
*Palette* section of [`md-html.md`](./md-html.md).
