# Invalid ipmt

Some invalid cases.

## Things

### INVALID: Short syntax

```ipmt-invalid
tB -> tC ::t
tB -- t
```

### INVALID: Base combinations (violate mutual exclusivity)

**Invalid** - PartOf and NearTo are different base types:
```ipmt-invalid
tB ::t --::P--> tC ::t
tB ::t --- tC ::t
```
```ipmt-invalid
tB ::t --- tC ::t
tB ::t --::P--> tC ::t
```

**Invalid** - LeadsTo (default arrow) and NearTo are different base types:
```ipmt-invalid
tB ::t --> tC ::t
tB ::t --::N-- tC ::t
```

```ipmt-invalid
tB ::t --::N-- tC ::t
tB ::t --> tC ::t
```

### INVALID: Opposite part-of edge (violates mutual exclusivity)

**Invalid** - PartOf and NearTo are different base types:
```ipmt-invalid
tC ::t <--::P-- tB ::t
tB ::t --- tC ::t
```

```ipmt-invalid
tB ::t --- tC ::t
tC ::t <--::P-- tB ::t
```

**Invalid** - PartOf (default arrow) and NearTo are different base types:
```ipmt-invalid
tC ::t <-- tB ::t
tB ::t --::N-- tC ::t
```

```ipmt-invalid
tB ::t --::N-- tC ::t
tC ::t <-- tB ::t
```


### INVALID: Opposite part-of with the same near to edge (violates mutual exclusivity)

**Invalid** - PartOf and NearTo are different base types:
```ipmt-invalid
tC ::t --::P--> tD ::t
tD ::t --- tC ::t
```
```ipmt-invalid
tD ::t --- tC ::t
tC ::t --::P--> tD ::t
```

**Invalid** - LeadsTo (default arrow) and NearTo are different base types:
```ipmt-invalid
tC ::t --> tD ::t
tD ::t --::N-- tC ::t
```

```ipmt-invalid
tD ::t --::N-- tC ::t
tC ::t --> tD ::t
```

## Concepts

### Short syntax

```ipmt-invalid
c-X ::c --> c-Y ::c
c-X --- c-Y
```

### INVALID: Base combinations (violate mutual exclusivity)

**Invalid** - Expresses and NearTo are different base types:
```ipmt-invalid
c-X ::c --::X--> c-Y ::c
c-X --- c-Y
```
```ipmt-invalid
c-X ::c --- c-Y ::c
c-X --::X--> c-Y
```

**Invalid** - Expresses (default arrow) and NearTo are different base types:
```ipmt-invalid
c-X ::c --> c-Y ::c
c-X --::N-- c-Y
```

```ipmt-invalid
c-X ::c --::N-- c-Y ::c
c-X --> c-Y
```

### INVALID: Opposite part-of edge (violates mutual exclusivity)

**Invalid** - Expresses and NearTo are different base types:
```ipmt-invalid
c-Y ::c <--::X-- c-X ::c
c-X --- c-Y
```

```ipmt-invalid
c-X ::t --- c-Y ::c
c-Y ::c <--::X-- c-X
```

**Invalid** - Expresses (default arrow) and NearTo are different base types:
```ipmt-invalid
c-Y ::c <-- c-X ::c
c-X --::N-- c-Y ::c
```

```ipmt-invalid
c-X ::t --::N-- c-Y ::c
c-Y ::c <-- c-X ::c
```

### INVALID: Base with different node type specifiers (violate mutual exclusivity)

**Invalid** - Expresses and NearTo are different base types:
```ipmt-invalid
c-X ::c
c-Y ::c
c-X --::X--> c-Y
c-X --- c-Y
```
```ipmt-invalid
c-X ::c, c-Y ::c
c-X --- c-Y
c-X --::X--> c-Y
```

**Invalid** - Expresses (default arrow) and NearTo are different base types:
```ipmt-invalid
c-Y ::c
c-X ::c
c-X --> c-Y
c-X --::N-- c-Y
```

```ipmt-invalid
c-Y ::c, c-X ::c
c-X ::c --::N-- c-Y ::c
c-X ::c --> c-Y ::c
```

## Invalid

### Chains

 Invalid case of duplicate edge for (A,B) nodes pair in a chain:
```ipmt-invalid
A ::t -->B ::t --"text"-->C ::t
A ::t -->B ::t
```
