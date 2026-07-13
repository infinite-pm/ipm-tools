# LeadsTo — `eLe`

A `--::L-->` (leads-to) edge is only legal between two **events** (`e→e`). So it
hard-forces both endpoints to Event — the strongest signal in the solver. This is
the *only* way Event appears: Event is never a default. Everything not reached by
leads-to defaults to Thing (an Expresses target defaults to Concept).

## Two undecided nodes joined by leads-to → both Event

```ipmt
# given
compile ::?etc --::L--> test ::?etc
```
```ipmt
# then
compile ::e --::L--> test ::e
```
```ipmt
# then defaults
compile ::e --::L--> test ::e
```

## A chain of leads-to → every node an Event

```ipmt
# given
edit ::?etc --::L--> compile ::?etc
compile --::L--> test ::?etc
test --::L--> ship ::?etc
```
```ipmt
# then
edit ::e --::L--> compile ::e
compile ::e --::L--> test ::e
test ::e --::L--> ship ::e
```
```ipmt
# then defaults
edit ::e --::L--> compile ::e
compile ::e --::L--> test ::e
test ::e --::L--> ship ::e
```
