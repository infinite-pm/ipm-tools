# Expresses — `tXc`, `eXc`, `cXc` (and the legal `eXe`)

A `--::X-->` (expresses) edge constrains its endpoints by type-pair only. The
one hard fact the pairs encode is that an expresses-**target** is never a Thing,
so the target's `Thing` candidate is pruned to `{Event, Concept}`. The **source**
is otherwise unconstrained and keeps all three kinds. Nothing about a bare
Expresses edge forces Event (or any single kind) — Event only ever appears when
leads-to forces an endpoint to it. By the default rule everything resolves to
**Thing**, except an Expresses **target** (never a Thing) resolves to **Concept**
— so the grey candidate sets are unchanged but the primary letter now leads with
the preferred kind: `::?ce` for an Expresses-target, `::?tec` otherwise.

## Thing expresses concept — `tXc`

The target loses only `Thing` (an expresses-target is never a Thing), leaving
`{Event, Concept}`; the source has no constraint at all, so it stays
`{Event, Thing, Concept}`. Neither end is forced — both stay grey. By default the
unconstrained source falls to its preferred **Thing** and the target to its
preferred **Concept**.

```ipmt
# given
server ::?etc --::X--> reliability ::?etc
```
```ipmt
# then
server ::?tec --::X--> reliability ::?ce
```
```ipmt
# then defaults
server ::t --::X--> reliability ::c
```

## Event expresses concept — `eXc`

A leads-to forces the source to Event (the only way Event ever appears), and that
propagates along the chain. The expresses-target still only loses `Thing`, so it
stays grey `{Event, Concept}` — the source being an Event does not pin the target
down. By default the grey target falls to its preferred **Concept**.

```ipmt
# given
deploy ::?etc --::L--> rollout ::?etc
rollout --::X--> safety ::?etc
```
```ipmt
# then
deploy ::e --::L--> rollout ::e
rollout ::e --::X--> safety ::?ce
```
```ipmt
# then defaults
deploy ::e --::L--> rollout ::e
rollout ::e --::X--> safety ::c
```

## Concept expresses concept — `cXc`

A bare Expresses chain forces nothing. `metric` is both an expresses-**target**
(from `dashboard`) and an expresses-**source** (to `goal`), but neither role
forces a single kind: each target merely loses `Thing`. With no leads-to
anywhere, no node is forced to Event; `dashboard` keeps all three kinds and the
two targets stay `{Event, Concept}` — the whole chain is grey. By default the
unconstrained `dashboard` falls to **Thing** and both Expresses-targets to
**Concept**.

```ipmt
# given
dashboard ::?etc --::X--> metric ::?etc
metric --::X--> goal ::?etc
```
```ipmt
# then
dashboard ::?tec --::X--> metric ::?ce
metric ::?ce --::X--> goal ::?ce
```
```ipmt
# then defaults
dashboard ::t --::X--> metric ::c
metric ::c --::X--> goal ::c
```

## An event can express — `eXe`

The γ(3,4) table allows `e→e` expresses, and the solver **does emit it**. An
expresses-target only forbids `Thing`; `Event` is still in its domain. So when a
node is both an event (e.g. a leads-to endpoint) and an expresses-target, the
Event survives both constraints and the edge resolves to `eXe`. No split, no
extra Concept node — a single Event satisfies everything. See the worked case in
[undecided-and-split.md](undecided-and-split.md).
