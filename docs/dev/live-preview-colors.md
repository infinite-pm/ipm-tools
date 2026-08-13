# Live colors in the markdown preview

How a `​```ipmt` fence gets its colors while the user is typing, and why the
obvious implementations of that do not work. Written 2026-08-13, after fixing
two symptoms that turned out to be one pipeline: fences stayed black unless
you paused typing, and every keystroke un-colored the block until the next
server round trip.

The server half lives here (`gl:cmd/ipm-rpc/embed_buffer.go`); the client half
lives in the extension repo, `src/extension/liveRefresh.ts` and
`src/extension/previewHighlight.ts`.

## The pipeline

1. The extension calls `ipm.embedBuffer` and caches each block's semantic
   tokens **keyed by the block's exact source text**.
2. VS Code renders the preview; our markdown-it `fence` rule looks tokens up
   by the source text it is rendering. Hit → `<span class="ipm-…">` markup.
   Miss → plain text, no colors. There is deliberately no client-side
   tokenizer: "briefly uncolored" beats "subtly wrong colors".

## Two clocks, and why a debounce cannot bridge them

The preview re-renders on VS Code's own **trailing throttle, hard-coded to
300 ms** (`preview.ts` `#delay = 300` in the `markdown-language-features`
extension). Calls arriving while its timer is pending are *dropped*, not
coalesced, and the render uses the buffer text **at render time**.

Our cache, by contrast, holds the text **at request time**. So any fixed
delay leaves the two describing different revisions, and with exact-source
keying a near-miss is worth exactly as much as no tokens at all. Worse, the
old client used a *resetting* debounce: typing with gaps under 300 ms cleared
the timer every keystroke, so the server was never called at all until the
typist paused.

The fix is not a better delay. It is to make the call cheap enough to run on
**every** change, so that whenever the preview happens to render, the tokens
for that exact text are already cached.

## Why reusing the previous tokens is unsound

Tempting, and wrong: tokens are whole-block derived, so a byte-identical line
legitimately changes color because of *other* lines.

- Any parse error anywhere makes `ipmtokens.Collect` return `nil` for the
  whole block (`gl:pkg/ipmtokens/tokens.go`).
- A node's title token is emitted only at its **first** mention, so deleting
  an earlier mention changes tokens on a textually unchanged later line
  (`gl:pkg/ipm/parser/parse.go`).
- Alias resolution is a post-pass over the whole block and can retro-color an
  earlier occurrence — aliases may be used before they are defined.
- Continuation lines (two leading spaces) are merged into the previous
  logical line before parsing (`gl:pkg/ipm/parser/preprocess.go`).

Kind resolution is otherwise first-mention-wins and forward-only: a later
`Bar ::e` does *not* recolor an earlier `Bar`. Coordinates are block-relative
plus the caller's `lineOffset`, so inserting a line above shifts every later
token's line by one — re-basing a cached block by a line delta is the only
reuse that is safe.

## What we do instead

- **`tokensOnly: true`** on `ipm.embedBuffer` returns source + tokens and
  skips the SVG layout — see [`../ipm-rpc.md`](../ipm-rpc.md).
- **Colors fire on every change**; **diagrams keep the idle debounce**
  (`ipm.liveRefreshDebounceMs`). A stale diagram is fine, a black fence is not.
- **Single-flight per URI**: one call in the air, later changes collapse into
  one follow-up. The rate then paces itself off measured latency instead of a
  constant.
- **Cooldown as long as the previous call** (capped 250 ms) after each
  refresh, which bounds the server's share of wall time near 50% on any
  machine — no setting to tune.
- **No forced `markdown.preview.refresh` on the typing path.** It rebuilds the
  whole webview; VS Code's own refresh picks new tokens up by itself.
- **Two token generations per URI**, rotated per response: bounds the cache
  under per-keystroke firing, and covers a render still working on the
  previous revision.
- **Version guard on SVGs only.** Tokens are content-addressed, so an
  out-of-order response can only re-add tokens for the text they describe;
  SVGs are keyed by block id and would clobber a newer diagram.

## Measurements

Real `ipm-rpc` over the LSP wire, 16-core machine, warm:

| document | full | `tokensOnly` |
| --- | --- | --- |
| 1 block, 11 lines | 1.4 ms / 11 KB | 0.5 ms / 1 KB |
| 6 blocks, 61 lines | 5.5 ms / 70 KB | 2.1 ms / 9 KB |
| `gl:docs/dev/layout-gen/layout-alg.md` (3234 lines, 95 blocks) | 110 ms / 926 KB | 35 ms / 130 KB |

Typing a 122-character block at 11 chars/s without pausing: **19 of 31**
preview renders colored, against **0 of 31** before. The remainder are states
that genuinely do not parse (half-typed quote), which correctly stay black.

Cost while typing continuously: 1.3% of one core on a normal document, 27% on
the 95-block one. A configurable throttle floor was measured and rejected —
floors under ~150 ms changed nothing (single-flight was already slower), and
the first floor that cut cost (200 ms) also halved the colored renders. The
adaptive cooldown costs nothing in those cases and takes a simulated slow
machine from 63% to 46% busy with no colored frames lost.

## Gotchas

- **An unterminated `​```ipmt` fence gets no tokens at all.** The scanner
  reports `unterminated` with no content (`gl:pkg/mdembed/scan.go`) and
  `semanticTokensFull` skips blocks with `EndLine < 0`
  (`gl:cmd/ipm-rpc/tokens.go`). The TextMate grammar still colors it in the
  editor pane, so a fence being written looks colored in the editor and black
  in the preview. Deliberate for embed-on-save — an unterminated block's
  extent is ambiguous — but arguably fixable for coloring alone, since
  CommonMark closes fences at end of document.
- **Saving used to blank the preview**: the save path evicted the whole live
  cache including tokens, and nothing re-fires the server on that path. Evict
  SVGs only — the on-disk renders replace those and nothing else.
- `markdown.preview.refresh` is a full webview reload, not a re-render. Use it
  when the document has settled, never per keystroke.
