# ipm-rpc

`ipm-rpc` is the **infinite.pm language server**: a single Go binary that speaks the [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) over stdio. Editors connect to it for diagnostics, hover, semantic tokens, and the `ipm.embed` / `ipm.embedBuffer` / `ipm.check` workspace commands that drive the SVG refresh flows (on save and live-while-typing).

Built primarily for the [`vscode-infinite-pm`](https://github.com/infinite-pm/vscode-infinite-pm) extension; works with any LSP client (Neovim's built-in LSP, Helix, eglot, Zed) because the protocol is editor-agnostic.

Implementation: [`cmd/ipm-rpc/`](../cmd/ipm-rpc/).

## Install

```bash
# From a clone of ipm-tools:
go install ./cmd/ipm-rpc

# Or, with explicit GOBIN:
GOBIN=/usr/local/bin go install ./cmd/ipm-rpc
```

The binary is pure-Go and statically linked (no cgo, no platform build tags). Cross-compilation works for all five supported targets out of the box:

```bash
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o dist/darwin-arm64/ipm-rpc ./cmd/ipm-rpc
```

CI produces a build for each target as an artifact on every push; see [`.github/workflows/ci.yml`](../.github/workflows/ci.yml).

## What it does

| LSP method | What it returns |
| --- | --- |
| `initialize` | Conservative capabilities: full-text sync, hover, semantic tokens (custom legend + modifiers), execute-command (`ipm.embed`, `ipm.embedBuffer`, `ipm.check`) |
| `textDocument/didOpen` / `didChange` / `didClose` | Maintains a per-document state map; full-text sync (gopls trick #3-style "snapshot per change" is in place but at v1 it's a simple replacement) |
| `textDocument/publishDiagnostics` | Parse errors + validator findings, plus `# ipmt:` pragma errors (unknown flag, or a pragma-shaped line that is not the first non-empty line — see [ipmt-unresolved.md](ipmt-unresolved.md)). For `.md` files, scans `​```ipmt` fences and offsets positions to whole-file coordinates |
| `textDocument/hover` | Markdown content explaining the token under the cursor (type markers, all arrow forms, relation letters) |
| `textDocument/semanticTokens/full` | Custom token types: `ipmEvent`, `ipmThing`, `ipmConcept`, `ipmUnresolved`, `ipmRelation`, `ipmArrow`, `ipmTypeMarker`, `ipmComment`, `ipmString`, `ipmTooltip`. Modifier bits: `alias`, `leadsTo`, `partOf`, `expresses`, `nearTo`, `hasAlias`, `marker`. |
| `workspace/executeCommand` | `ipm.embed` (write SVGs + update markers), `ipm.embedBuffer` (live in-memory render + tokens), `ipm.check` (read-only check) |
| `shutdown` / `exit` | Graceful termination |

## Workspace commands

### `ipm.embed`

```json
{
  "command": "ipm.embed",
  "arguments": [{ "file": "file:///path/to/x.md" }]
}
```

Runs the same logic [`cmd/md-embed`](md-embed.md) runs headlessly: scans `​```ipmt` fences (and `<!-- ipm-include src=... -->` lines), parses each, writes the rendered SVG to `_ipm/<rel>/<id>.ipm.svg`, updates the marker hash in the `.md`. Returns an `embedResult`:

```jsonc
{
  "file": "/path/to/x.md",
  "changed": true,
  "stats": {
    "blocks": 3,
    "insertMarker": 1,
    "rehash": 1,
    "rerender": 0,
    "missingSrc": 0,
    "noEmbed": 0,
    "badMeta": 0,
    "unterminated": 0,
    "malformed": 0
  },
  "warnings": [],
  "staleSummary": "blocks=3 insert=1 rehash=1"
}
```

### `ipm.embedBuffer`

```json
{
  "command": "ipm.embedBuffer",
  "arguments": [{ "uri": "file:///path/to/x.md" }]
}
```

Same scan + render logic as `ipm.embed`, but renders against the LSP's **in-memory buffer** (the latest `textDocument/didChange` content) and **never writes to disk**. Returns each block's rendered SVG as base64, plus the block-relative semantic tokens used by the markdown preview to highlight the code-fence text. The document MUST have been opened (`textDocument/didOpen`) first; the server returns `document not open in server` otherwise.

```jsonc
{
  "uri": "file:///path/to/x.md",
  "blocks": [
    {
      "index": 1,
      "id": "01",
      "hash": "9ebf5118",
      "source": "inner ::e --::P--> outer ::e",
      "svgBase64": "PHN2Zy…",
      "outcome": "ok",
      "tokens": [
        { "line": 0, "col": 0,  "len": 5,  "type": "ipmEvent" },
        { "line": 0, "col": 6,  "len": 3,  "type": "ipmEvent", "mods": 64 },
        { "line": 0, "col": 10, "len": 2,  "type": "ipmArrow", "mods": 4 },
        { "line": 0, "col": 12, "len": 3,  "type": "ipmRelation" },
        { "line": 0, "col": 15, "len": 3,  "type": "ipmArrow", "mods": 4 },
        { "line": 0, "col": 19, "len": 5,  "type": "ipmEvent" },
        { "line": 0, "col": 25, "len": 3,  "type": "ipmEvent", "mods": 64 }
      ]
    }
  ],
  "warnings": []
}
```

Modifier bits in `mods` follow the semantic-token modifier order (`alias=1`, `leadsTo=2`, `partOf=4`, `expresses=8`, `nearTo=16`, `hasAlias=32`, `marker=64`). Tokens are block-relative (line=0 means the first line *of the block*, not the markdown file).

Used by the [`vscode-infinite-pm`](https://github.com/infinite-pm/vscode-infinite-pm) extension's live-refresh path to paint the markdown preview without touching disk: the preview's fence renderer caches the tokens by URI + block source and applies the same `<span class="ipm-…">` markup the editor pane gets.

### `ipm.check`

Same as `ipm.embed` but read-only: never writes files. Returns the same shape, with `changed: false`. Used by editor status-bar widgets for staleness detection.

## Custom semantic token legend

Advertised in `initialize`'s `SemanticTokensProvider.legend.tokenTypes`. Order is stable (part of the protocol contract once a client caches it):

| Index | Type | Applies to |
| --- | --- | --- |
| 0 | `ipmEvent` | Event node names and `::e` kind marker (default: orange; lighter `#ffaa55` with `hasAlias`; bold with `marker`) |
| 1 | `ipmThing` | Thing node names and `::t` kind marker (default: green) |
| 2 | `ipmConcept` | Concept node names and `::c` kind marker (default: blue) |
| 3 | `ipmRelation` | `::L` / `::P` / `::X` / `::N` inside an explicit arrow |
| 4 | `ipmArrow` | `-`/`<`/`>` character runs inside an edge's arrow span. Embedded tooltips and relation markers are NOT covered — the arrow span splits around them so `::P` and `"text"` get their own non-bold tokens. |
| 5 | `ipmTypeMarker` | `::a` / `::tip` (kind-neutral markers) |
| 6 | `ipmComment` | `#` lines |
| 7 | `ipmString` | Quoted strings that aren't tooltips |
| 8 | `ipmTooltip` | Edge tooltips (`--"text"-->` or `--ident-->`) and node tooltips (`"text" ::tip`) |
| 9 | `ipmUnresolved` | `::?` undecided-kind prefix markers and Unresolved-kind node titles (default: grey `#8a8a8a`) |

Modifier bits (stable bit positions; clients hold on to them once cached):

| Bit | Modifier | Applies to |
| --- | --- | --- |
| 0 | `alias` | The short identifier portion of an aliased node |
| 1 | `leadsTo` | Arrow span with SST relation Leads-to |
| 2 | `partOf` | Arrow span with SST relation Part-of |
| 3 | `expresses` | Arrow span with SST relation Expresses |
| 4 | `nearTo` | Arrow span with SST relation Near-to |
| 5 | `hasAlias` | The long-form label token of a node that also has an alias (themes typically lighten it) |
| 6 | `marker` | The `::e`/`::t`/`::c` syntactic marker (vs. the identifier of the same kind) |

The legend is the *index* mapping; the corresponding colors are theme-configurable. The VS Code extension ships default color customizations generated from `pkg/ipmtokens/palette.json` that bind events to orange, things to green, concepts to blue, arrows + kind markers bold.

## Flags

```bash
ipm-rpc [--verbose | --quiet] [--stdio] [--version]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--verbose` | off | Log at DEBUG level (every LSP method call, every state change) |
| `--quiet` | off | Log at ERROR level only |
| `--stdio` | off (no-op) | Accepted for compatibility with LSP clients that unconditionally append it (vscode-languageclient does). stdio is the only transport, so the flag is silently ignored. |
| `--version` | off | Print `ipm-rpc <version>` to stdout and exit 0. Used by the `vscode-ipm` extension's "Show Server Info" command. |

Logs go to stderr in human-readable text via `pkg/ipm/log`. LSP traffic goes on stdin/stdout, so the two streams don't collide.

## Editor integration recipes

### VS Code

Install [`vscode-infinite-pm`](https://github.com/infinite-pm/vscode-infinite-pm); the extension spawns `ipm-rpc` and handles everything.

### Neovim (built-in LSP, ≥0.8)

```lua
vim.lsp.start({
  name = "ipm-rpc",
  cmd = { "ipm-rpc" },
  root_dir = vim.fs.dirname(vim.fs.find({ ".git", ".ipm.conf" }, { upward = true })[1]),
  filetypes = { "ipmt", "markdown" },
})
```

Add an autocmd for `.ipmt` files if you want to register them as `ipmt` filetype:

```lua
vim.filetype.add({ extension = { ipmt = "ipmt" } })
```

### Helix

In `~/.config/helix/languages.toml`:

```toml
[[language]]
name = "ipmt"
scope = "source.ipmt"
file-types = ["ipmt"]
language-servers = ["ipm-rpc"]

[language-server.ipm-rpc]
command = "ipm-rpc"
```

### Emacs (eglot)

```elisp
(add-to-list 'eglot-server-programs '(ipmt-mode . ("ipm-rpc")))
```

## Architecture notes

Highlights:

- **Pure LSP** — no custom JSON-RPC methods beyond standard LSP commands. The two `ipm.*` workspace commands are dispatched through standard `workspace/executeCommand`.
- **`go.lsp.dev/protocol`** for types only; **`creachadair/jrpc2`** for the transport. Server loop is hand-rolled following gopls patterns.
- **One server per VS Code window**, handles all workspace folders inside it (matches `vscode-languageclient` defaults).
- **gopls patterns wired from day one**: cancellation contexts on every handler, conservative capability negotiation, structured logging via `pkg/ipm/log`, graceful shutdown, UTF-16 ↔ UTF-8 position mapper, markdown content in hover.

## Limitations (current)

- Parser is fail-fast — when a document has a syntax error, only the first error is surfaced. Recovery is deferred.
- `textDocument/completion`, `textDocument/codeAction`, `textDocument/codeLens` aren't yet implemented. Method surface in `cmd/ipm-rpc/server.go::assignedHandlers` lists what's live.
