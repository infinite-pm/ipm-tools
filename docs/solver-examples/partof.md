# PartOf — `tPt`, `tPe`, `ePe`

A `--::P-->` (part-of) edge points **part → container**. Legal pairs: `t→t`,
`t→e`, `e→e` — never `e→t`, and never a concept. So PartOf prunes **Concept**
from both ends, leaving `{Event,Thing}`; it forces nothing further. **Event**
appears only when a leads-to elsewhere forces it (and then propagates); otherwise
each grey end defaults to **Thing** — its preferred kind leads the set (`::?te`).

## Part-of, no other signal — `tPt` (both grey)

PartOf removes Concept from each end but nothing forces either to a single kind,
so both stay grey over `{Event,Thing}`, preferring Thing — `::?te`. With defaults
they collapse to Thing (`tPt`); Event would need a leads-to that never arrives.

```ipmt
# given
wheel ::?etc --::P--> car ::?etc
```
```ipmt
# then
wheel ::?te --::P--> car ::?te
```
```ipmt
# then defaults
wheel ::t --::P--> car ::t
```

## Part-of with a leads-to container — `tPe`

The container `process` is a leads-to endpoint, so it is forced to Event and
`finish` with it. The part `step` only loses Concept (via PartOf) and stays grey
over `{Event,Thing}` — `e→e` and `t→e` are both legal, so nothing pins it; it
prefers Thing (`::?te`), and defaults collapse it to Thing.

```ipmt
# given
step ::?etc --::P--> process ::?etc
process --::L--> finish ::?etc
```
```ipmt
# then
step ::?te --::P--> process ::e
process ::e --::L--> finish ::e
```
```ipmt
# then defaults
step ::t --::P--> process ::e
process ::e --::L--> finish ::e
```

## Event part-of event — `ePe`

A sub-process contained in a larger one. Both `chop` and `cook` are leads-to
endpoints (events), and `chop` is part of `cook`. Every node is forced to Event
via leads-to, so nothing stays grey — defaults change nothing.

```ipmt
# given
prep ::?etc --::L--> chop ::?etc
cook ::?etc --::L--> serve ::?etc
prep, chop --::P--> cook
```
```ipmt
# then
prep ::e --::L--> chop ::e
cook ::e --::L--> serve ::e
prep, chop --::P--> cook
```
```ipmt
# then defaults
prep ::e --::L--> chop ::e
cook ::e --::L--> serve ::e
prep, chop --::P--> cook
```
