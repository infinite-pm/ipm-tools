# ipmt-spec tables

To cover all ipmt syntax from [specification](ipmt-spec.md) as explicit code block examples. These will be parser and used in tests.

## thing to thing
```ipmt
tA ::t --> tAp1 ::t
```

```ipmt
tAp1 ::t --::P--> tA ::t
```

## thing to event
```ipmt
tAp1 ::t --> e1
```

```ipmt
tAp1 ::t --::P--> e1
```

## event to event (leads to)
```ipmt
e1 --> e2
```

```ipmt
e1 --::L--> e2
```

## event to event (part-of)
```ipmt
e5s1 --::P--> e5
```

## event to event (expresses)
```ipmt
e4 --::X--> e5
```

## thing to concept
```ipmt
tAp1 ::t --> c-X ::c
```

```ipmt
tAp1 ::t --::X--> c-X ::c
```

## event to concept
```ipmt
e1 --> c-X ::c
```

```ipmt
e1 --::X--> c-X ::c
```

## concept to concept
```ipmt
c-X ::c --> c-Y ::c
```

```ipmt
c-X ::c --::X--> c-Y ::c
```

## event to event (near to)
```ipmt
eG --- eH
```

```ipmt
eG --::N-- eH
```

## thing to thing (near to)
```ipmt
tB ::t --- tC ::t
```

```ipmt
tB ::t --::N-- tC ::t
```

## concept to concept (near to)
```ipmt
c-Z ::c --- c-X ::c
```

```ipmt
c-Z ::c --::N-- c-X ::c
```

## thing to event (part of / hosts)
```ipmt
tAp1 ::t --> e1
```

```ipmt
tAp1 ::t --::P--> e1
```

# Reverse explicit arrows

## event contains event (reverse part-of)
```ipmt
e1 <--::P-- e1sub
```

## event contains thing (reverse part-of)
```ipmt
e1 <--::P-- tA ::t
```

## concept expressed by event (reverse expresses)
```ipmt
cJ ::c <--::X-- e1
```

## event follows event (reverse leads-to)
```ipmt
e2 <--::L-- e1
```

# Edge type examples

The 4 SST relations (LeadsTo, PartOf, Expresses, NearTo) are mutually exclusive semantic primitives. Between any two nodes, only ONE base relation type can exist.

## Events

Event to event with LeadsTo:
```ipmt
e1 --> e2
```

Event to event with PartOf (containment):
```ipmt
e1 --::P--> e2
```

Event to event with Expresses:
```ipmt
e1 --::X--> e2
```

Event to event with NearTo:
```ipmt
e1 --::N-- e2
```
