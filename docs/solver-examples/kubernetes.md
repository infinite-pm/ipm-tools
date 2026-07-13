# A worked graph — Kubernetes pod lifecycle + reconciliation

The single-relation examples isolate one type-pair each; this one is a small but
realistic slice of the Kubernetes corpus, so you can see the three kinds emerge
together from a graph that mixes all four relations.

The story, in three layers:

- **A process** — `apply manifest → schedule pod → run pod` — a `--::L-->`
  (leads-to) chain. Leads-to is the *only* thing that forces **Event**, and it
  forces every node on the chain, so the whole lifecycle is events.
- **An entity hierarchy** — `container --::P--> pod --::P--> node`. Part-of can
  never involve a Concept, so each node is `{Event, Thing}` — grey on its own
  (nothing forces it), and **Thing** by default (no leads-to ⇒ not dynamic).
- **The control loop** — `controller --::X--> "compare states" --::X--> "desired
  state"`. Expresses targets can never be Things, so the states are
  `{Event, Concept}`. `"compare states"` is the reconciliation node (SST's
  *"observe the actual state and compare to the desired state"*): it is **both** an
  Expresses target *and* source, so Event and Concept both survive — it stays
  honestly grey in strict mode, and defaults to **Concept**.

```ipmt
# given
"apply manifest" ::?etc --::L--> "schedule pod" ::?etc
"schedule pod" --::L--> "run pod" ::?etc
container ::?etc --::P--> pod ::?etc
pod --::P--> node ::?etc
controller ::?etc --::X--> "compare states" ::?etc
"compare states" --::X--> "desired state" ::?etc
```
```ipmt
# then
"apply manifest" ::e --::L--> "schedule pod" ::e
"schedule pod" ::e --::L--> "run pod" ::e
container ::?te --::P--> pod ::?te
pod ::?te --::P--> node ::?te
controller ::?tec --::X--> "compare states" ::?ce
"compare states" ::?ce --::X--> "desired state" ::?ce
```
```ipmt
# then defaults
"apply manifest" ::e --::L--> "schedule pod" ::e
"schedule pod" ::e --::L--> "run pod" ::e
container ::t --::P--> pod ::t
pod ::t --::P--> node ::t
controller ::t --::X--> "compare states" ::c
"compare states" ::c --::X--> "desired state" ::c
```

Read the `defaults` graph back as English and it is exactly the domain: a
**process** (events) acts on an **entity hierarchy** (things) while a **controller**
(thing) reconciles the cluster's **state** (concepts) — `eLe`, `tPt`, `tXc`, `cXc`,
every edge a legal γ(3,4) pair. The strict graph is the same picture, honest about
what the type-pairs leave open: only the leads-to process is decided; the entities
and states stay grey, leading with their likely kind.
