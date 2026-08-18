# Layout algorithm — composite shells

Cases for `layout7.Options.Shells` (`layout7-engine.md`, Options): the engine
draws a SHELL around every root composite whose members are present — the box
the zoom canvas draws around an open composite — and lays everything else out
against it. The shell is a box of the layout: the spine keeps its row gap from
it, tiling and rings keep their gap from it, other edges route around it,
member edges cross its border, the composite's own aux and every foreign aux
sit outside it (a thing or concept inside a shell it does not belong to would
read as content of the composite). Same executable mechanics as
`layout-alg.md`: `ipmdev-layout-rule` blocks are extracted by
`gl:cmd-dev/sync-test-cases` into `tests/layout-gen-shells-rules/*.dsl` and run
by `go run ./cmd-dev/layout-test-runner --shells --dir tests/layout-gen-shells`
against `tests/layout-gen-shells/*.ipmt`.

The blocks here carry the `# ipmt: embed=false` pragma: `md-embed` renders the flat engine,
and the flat picture of a shells case is not the picture under test. Each case
links its fixture's SVG instead, rendered by `ipmsvg-gen` from the shells
layout the runner writes. A shell is addressable in a rule as `#shell-<alias>`
(the shell node carries the composite's alias with the `shell-` prefix; its ID
is `shell-<id>`).

The constants: `ShellPad` 20 (air inside the shell), `RowGap` 60 (the spine's
gap, kept from the shell), `ColGap` 40 (a band's gap, kept from the shell).

## Cases

### a shell wraps the composite and its members with one pad of air

The composite `eC` and its sub-grid `m1 → m2` are wrapped by `shell-eC`: one
`ShellPad` of air on every side of {composite ∪ members}. The spine keeps its
`RowGap` from the shell, not from the composite's box (`e1` above, `e2`
below), and the composite's own bands (`tA`, `cX` — both on the left flank:
the right is the grid's) keep a `ColGap` from the shell, outside it.

```ipmt
# ipmt: embed=false
e1 ::e --> eC ::e --> e2 ::e
m1 ::e --> m2 ::e
m1, m2 --::P--> eC
eC <-- tA
eC --> cX ::c
```

![a shell wraps the composite and its members with one pad of air](../../../tests/layout-gen-shells/a-shell-wraps-the-composite-and-its-members-with-one-pad-of-air.ipm.svg)

```ipmdev-layout-rule
@scope local
#shell-eC has x=200
#shell-eC has y=240
#shell-eC has size=340x220
#eC has x=220
#eC has y=320
#m1 has x=400
#m1 has y=260
#m2 has y=380
#e1 is above #shell-eC with gap=60
#e2 is below #shell-eC with gap=60
#tA is left-of #shell-eC with gap=40
#cX is left-of #shell-eC with gap=40
edge #e1,#eC is vertical
edge #eC,#e2 is vertical
```

### the spine keeps its row gap from a shell, hangs included

`e1`'s concept fan hangs 100px below `e1`'s row; the shell of `eC` rises 80px
above `eC`'s box (half the grid's extra extent plus the pad). The two are
SUMMED with the row gap between them — the fan's last concept keeps a full
`RowGap` from the shell's top edge (a fan that hung to the shell's very edge
read as the composite's content). The shell's rise is known from the plan when
`eC`'s row is placed, before its members are.

```ipmt
# ipmt: embed=false
e1 ::e --> eC ::e --> e2 ::e
e1 --> cA1 ::c, cA2 ::c, cA3 ::c
m1 ::e --> m2 ::e
m1, m2 --::P--> eC
```

![the spine keeps its row gap from a shell, hangs included](../../../tests/layout-gen-shells/the-spine-keeps-its-row-gap-from-a-shell-hangs-included.ipm.svg)

```ipmdev-layout-rule
@scope local
all #cA1,#cA2,#cA3 have same center-x
#cA3 has y=320
#shell-eC has y=440
#cA3 is above #shell-eC with gap=60
#eC has y=520
#e2 is below #shell-eC with gap=60
edge #e1,#eC is vertical
```

### a member's satellites sit outside the shell, their edges cross it

`tX` (part of the member `m2`) and `cY` (expressed by `m3`) belong to members,
not to the composite: they are not the container's children, so they sit
OUTSIDE the shell — one `ColGap` right of it, on their member's row — and
their edges cross the shell's border to reach the member (a member's edge is
what a shell is transparent for). The members' return edges to `eC` fan onto
its right side in row order.

```ipmt
# ipmt: embed=false
e1 ::e --> eC ::e --> e2 ::e
m1 ::e --> m2 ::e --> m3 ::e
m1, m2, m3 --::P--> eC
m2 <-- tX
m3 --> cY ::c
```

![a member's satellites sit outside the shell, their edges cross it](../../../tests/layout-gen-shells/a-members-satellites-sit-outside-the-shell-their-edges-cross-it.ipm.svg)

```ipmdev-layout-rule
@scope local
#shell-eC has x=40
#shell-eC has size=340x340
#tX is right-of #shell-eC with gap=40
#cY is right-of #shell-eC with gap=40
all #tX,#m2 have same y
all #cY,#m3 have same y
edge #tX,#m2 has max-bends=0
edge #m3,#cY has max-bends=0
edge #m1,#eC has target-side=right
edge #m2,#eC has target-side=right
edge #m3,#eC has target-side=right
edge #m1,#eC does not cross edge #m2,#eC
edge #m2,#eC does not cross edge #m3,#eC
```
