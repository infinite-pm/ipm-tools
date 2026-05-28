# ipmt-spec tables

To cover all ipmt syntax from [specification](ipmt-spec.md) as explicit code block examples. These will be parser and used in tests.

## thing to thing
```ipmt
tA --> tAp1
```

```ipmt
tAp1 --::P--> tA
```

## thing to event
```ipmt
tAp1 --> e1 ::e
```

```ipmt
tAp1 --::P--> e1 ::e
```

## event to event (leads to)
```ipmt
e1 ::e --> e2 ::e
```

```ipmt
e1 ::e --::L--> e2 ::e
```

## event to event (part-of)
```ipmt
e5s1 ::e --::P--> e5 ::e
```

## event to event (expresses)
```ipmt
e4 ::e --::X--> e5 ::e
```

## thing to concept
```ipmt
tAp1 --> c-X ::c
```

```ipmt
tAp1 --::X--> c-X ::c
```

## event to concept
```ipmt
e1 ::e --> c-X ::c
```

```ipmt
e1 ::e --::X--> c-X ::c
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
eG ::e --- eH ::e
```

```ipmt
eG ::e --::N-- eH ::e
```

## thing to thing (near to)
```ipmt
tB --- tC
```

```ipmt
tB --::N-- tC
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
tAp1 --> e1 ::e
```

```ipmt
tAp1 --::P--> e1 ::e
```

# Reverse explicit arrows

## event contains event (reverse part-of)
```ipmt
e1 ::e <--::P-- e1sub ::e
```

## event contains thing (reverse part-of)
```ipmt
e1 ::e <--::P-- tA
```

## concept expressed by event (reverse expresses)
```ipmt
cJ ::c <--::X-- e1 ::e
```

## event follows event (reverse leads-to)
```ipmt
e2 ::e <--::L-- e1 ::e
```

# Edge type examples

The 4 SST relations (LeadsTo, PartOf, Expresses, NearTo) are mutually exclusive semantic primitives. Between any two nodes, only ONE base relation type can exist.

## Events

Event to event with LeadsTo:
```ipmt
e1 ::e --> e2 ::e
```

Event to event with PartOf (containment):
```ipmt
e1 ::e --::P--> e2 ::e
```

Event to event with Expresses:
```ipmt
e1 ::e --::X--> e2 ::e
```

Event to event with NearTo:
```ipmt
e1 ::e --::N-- e2 ::e
```
