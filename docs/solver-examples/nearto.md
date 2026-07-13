# NearTo — same-kind and propagation

A `--::N--` (near-to) edge is undirected and constrains its two endpoints to the
**same kind**. It carries *no* kind signal of its own, so it never forces a kind
on its own — it only ties two domains together. An isolated near-to pair stays
grey; a near-to next to a kind-forcing edge makes both ends agree on that kind.

Under the constraint-faithful solver near-to **never** drops. Event is in every
node's structural domain, so two near-to nodes can always satisfy "same kind" by
both being Event. A drop would require the two ends to be forced to *different*
singleton kinds — something only markers (not structure) could do.

Event is produced ONLY where a leads-to edge forces it (and what that propagates
to through same-kind near-to and part-of/expresses chains); it is never a
default. So a grey near-to node defaults to **Thing** (`::t`) — a near-to source
carries no signal of its own. Only an Expresses target would default to Concept.

## Isolated near-to → stays grey

Neither node has any kind signal, so both remain undecided (the near-to only
says "same kind", which is satisfied by all three). The grey set leads with
**Thing** — the preferred default for a no-signal near-to node.

```ipmt
# given
foo ::?etc --::N-- bar ::?etc
```
```ipmt
# then
foo ::?tec --::N-- bar ::?tec
```
```ipmt
# then defaults
foo ::t --::N-- bar ::t
```

## Near-to propagates a forced kind

`fire` is a leads-to endpoint (Event); `spark` is near-to `fire`, so it must be
the same kind → Event.

```ipmt
# given
trigger ::?etc --::L--> fire ::?etc
fire --::N-- spark ::?etc
```
```ipmt
# then
trigger ::e --::L--> fire ::e
fire ::e --::N-- spark ::e
```
```ipmt
# then defaults
trigger ::e --::L--> fire ::e
fire ::e --::N-- spark ::e
```

## Near-to propagates a forced kind across the graph

`liftoff` is forced to Event by leads-to. The near-to ties `rocket` to the same
kind, pulling it to Event too — not to Thing, because part-of alone never forces
a kind. That `rocket ::e --::P--> stage` is then an Event part-of (`ePe`), which
forces `stage` to Event. So one leads-to cascades through the near-to and the
part-of to decide the whole graph; the near-to is **kept**.

```ipmt
# given
launch ::?etc --::L--> liftoff ::?etc
rocket ::?etc --::P--> stage ::?etc
liftoff --::N-- rocket
```
```ipmt
# then
launch ::e --::L--> liftoff ::e
rocket ::e --::P--> stage ::e
liftoff ::e --::N-- rocket ::e
```
```ipmt
# then defaults
launch ::e --::L--> liftoff ::e
rocket ::e --::P--> stage ::e
liftoff ::e --::N-- rocket ::e
```
