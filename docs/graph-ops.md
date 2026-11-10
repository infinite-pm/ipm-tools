# Graph ops: the model, the cut, the refs

Three packages under `pkg/ipm/` that a program building or reading graphs
imports together. They are the Go base every application in the ecosystem
shares: a producer (a miner, a code scanner, a converter) writes graph JSON with
its refs beside it; a consumer (a server, a page generator) reads the graph,
cuts what it needs and can say where every node came from. What *serves* a
graph — the canvas, the board, a docs page — lives in the canvas server and reads
these; nothing here knows about HTTP or HTML.

| package | what | for |
| --- | --- | --- |
| [`pkg/ipm/graph`](../pkg/ipm/graph/) | the model by its base names (`Graph`, `Node`, `Edge`, the four kinds, the four links), `Load`/`Marshal`, and the **Builder** | a programmatic producer: a shared id space for nodes and edges, one edge per node pair, the JSON the ecosystem reads |
| [`pkg/ipm/subgraph`](../pkg/ipm/subgraph/) | **`Cut`**: the neighbourhood of seed nodes under a size cap, re-id'd into a fresh valid graph with the map back; `FindByName`, `Concepts`, `FileSpace`; the older name-seeded `Neighborhood` | a readable piece of a big graph whose rendering can still point into the whole (a canvas addresses nodes by the original ids) |
| [`pkg/ipm/refs`](../pkg/ipm/refs/) | the side-car `<graph>.refs.json`: a **build record** and, per node and edge id, the **refs** it was made from | "where did this come from" — for a chip on a page, an inspector, an audit |

## The model and the Builder

`graph.Graph` is `model.IpmGraph` under its base name; the package adds nothing
to the struct and every tool that reads graph JSON reads what the Builder
writes. A Builder numbers nodes and edges from one counter (the parser numbers
nodes then edges, so an id names one element), drops a second edge between the
same pair (the four SST relations are mutually exclusive per pair, and a
graph with two would not validate), and `Marshal`s with the version stamp.
`Load` is the reverse. A producer that used to mirror the struct imports this
instead.

## The cut

`subgraph.Cut(g, seeds, Options{Depth, Containers, Cap, Drop})`:

- **Depth** — undirected hops from the seeds kept. 1 is "the concept and what
  expresses it"; 2 pulls in neighbourhoods.
- **Containers** — part-of ancestors kept for every kept Thing, so a loop comes
  with its function and package instead of floating.
- **Cap** — the most nodes; past it the farthest go first, seeds never.
- **Drop** — a predicate for nodes to leave out even when reached (`FileSpace`
  drops the module-path names above a package).

The `Piece` carries the new graph, `ToOrig`/`ToNew` id maps and the seeds' new
ids. Edges between kept nodes come along; the piece validates on its own.

## The refs side-car

One shape for every producer, beside its graph as `<graph>.refs.json`
(`refs.Path(graphPath)`):

```
{ "version": "1",
  "build": { "tool": "…", "commit": "…", "args": […], "inputs": [{"path": …, "sha256": …}],
             "at": "RFC 3339", "parent": "<the refs file this was made from>" },
  "nodes": { "<id>": [ {"kind": "…", "at": "<locator>", "by": "…", "why": "…", …} ] },
  "edges": { "<id>": [ … ] } }
```

A **ref** is `{kind, at, by, why, signal, conf, mark, hash, count}`; only `kind`
and `at` are always set. `at` is a locator in one of three forms — `repo:path#Lnn`
for a file in a repository, `module@version/path:line` for pinned source, or a
URL. `by` says who: `author` (a person wrote the source), `tool:<name>`, or
`person` (a curated decision). The kinds, each with a sentence in the source:
`fragment`, `source-line`, `source`, `loop`, `log-line`, `log-ref`, `coverage`,
`candidate`, `curated`, `docref`, `glossary`, `ontology`, `flow-step`,
`union-merge`, `cut`, `page`.

`refs.New(tool, args, inputs…)` starts a side-car with the build record (inputs
hashed now; the tool's VCS revision when the binary carries it), `Node`/`Edge`
record, `Write`/`Read` move it, `Rekey(toOrig, from, build)` carries refs
across a cut's id map and notes the cut on every node.

**How they chain.** A union keeps, on every node, the fragments it merged and the
node's id in each; a consumer turns those into `union-merge` refs and follows the
original id into the producer's side-car. A page's build record names the union's
side-car as `parent`, which names each fragment's. No database: the id maps
chain, and every file sits beside the graph it describes.

## Working rules for these packages

- **One shape.** A new kind of provenance is a new `Kind` constant with a
  sentence, not a new file format.
- **Ids are the join.** A cut, a union and a page all carry id maps; a ref is
  keyed by the id of the graph it sits beside, and moves only through a map.
- **A producer writes refs beside its graph, or none.** A graph without a
  side-car is a graph whose origin is unknown, and a consumer says so rather
  than guessing.
- **Serving is not an op.** Anything that needs a port, a page or a picture is
  the canvas server's; these packages stay importable by a command-line tool.
