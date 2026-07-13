# Undecided (grey) nodes

The solver decides a node's kind only when arc-consistency over the type-pairs
forces a single kind. The only hard-forcing relation is leads-to (`eLe`), which
pins both endpoints to **Event**; that Event then propagates along NearTo/PartOf/
Expresses. Nothing structural ever *uniquely* forces Thing or Concept, so a node
with no leads-to pressure stays **Unresolved** (grey) with its surviving
candidate set — it never guesses. Splits never occur: an event that is also an
expresses-target is just a single Event (`eXe`).

Each grey set is written **preferred-kind first**: that lead letter is what
`--defaults` collapses to. Event is never a default — it appears only where
leads-to forces it. Everything else defaults to **Thing** (`::?tec` for a
no-signal / isolated node), except an Expresses **target**, which defaults to
**Concept** (`::?ce`).

## A node with no edges stays grey

Nothing constrains it, so all three kinds remain candidates. With no leads-to
and no expresses pressure it defaults to **Thing**.

```ipmt
# given
mystery ::?etc
```
```ipmt
# then
mystery ::?tec
```
```ipmt
# then defaults
mystery ::t
```

## An event that is also an expresses-target — eXe, not a split

`compare` is a leads-to endpoint (so it is forced to **Event**) *and* the target
of an expresses. `eXe` is a legal type-pair, so the Event satisfies the expresses
edge directly — there is no conflict and nothing to split. `compare` stays a
single Event node. Because `compare` is now a fixed Event, the incoming
`goal --::X--> compare` only admits an Event source (`eXe`), so `goal` is forced
to **Event** too. Everything is already decided, so `--defaults` changes nothing.

```ipmt
# given
start ::?etc --::L--> compare ::?etc
goal ::?etc --::X--> compare
```
```ipmt
# then
start ::e --::L--> compare ::e
goal ::e --::X--> compare ::e
```
```ipmt
# then defaults
start ::e --::L--> compare ::e
goal ::e --::X--> compare ::e
```

## Both an expresses target and source — stays grey

A node that is both an expresses-**target** and an expresses-**source** loses
only Thing on its target side, but nothing forces it to a single kind. With no
leads-to anywhere in the chain, every node stays grey: the bare expresses
source `controller` keeps all three candidates (Thing-preferred, `::?tec`), and
the expresses targets drop Thing to {Concept,Event} (Concept-preferred, `::?ce`).
The solver does not guess — but `--defaults` collapses each grey node to its
lead kind: `controller` to **Thing**, the two Expresses targets to **Concept**.

```ipmt
# given
controller ::?etc --::X--> behaviour ::?etc
behaviour --::X--> state ::?etc
```
```ipmt
# then
controller ::?tec --::X--> behaviour ::?ce
behaviour ::?ce --::X--> state ::?ce
```
```ipmt
# then defaults
controller ::t --::X--> behaviour ::c
behaviour ::c --::X--> state ::c
```

## Partial resolution — Event forced, the rest stays grey

Leads-to forces `u0` and `ev` to **Event**. `ev` then expresses `beh`: the Event
source is fine, and the target `beh` loses only Thing → {Concept,Event}. `beh`
in turn expresses `prop`, which likewise drops Thing → {Concept,Event}. Neither
target is forced to a single kind, so the `# then` is *partly grey*: the Events
are decided, the expresses targets stay `::?ce`. This is the honest output the
BDD format is designed to assert. `--defaults` then collapses each Expresses
target to its preferred **Concept**, leaving the leads-to Events untouched.

```ipmt
# given
u0 ::?etc --::L--> ev ::?etc
ev --::X--> beh ::?etc
beh --::X--> prop ::?etc
```
```ipmt
# then
u0 ::e --::L--> ev ::e
ev ::e --::X--> beh ::?ce
beh ::?ce --::X--> prop ::?ce
```
```ipmt
# then defaults
u0 ::e --::L--> ev ::e
ev ::e --::X--> beh ::c
beh ::c --::X--> prop ::c
```
