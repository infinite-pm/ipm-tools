# Invalid ipmt

Some invalid cases.

## Things

### INVALID: Short syntax

```ipmt-invalid
tB -> tC
tB -- t
```

### INVALID: Base combinations (violate mutual exclusivity)

**Invalid** - PartOf and NearTo are different base types:
```ipmt-invalid
tB --::P--> tC
tB --- tC
```
```ipmt-invalid
tB --- tC
tB --::P--> tC
```

**Invalid** - LeadsTo (default arrow) and NearTo are different base types:
```ipmt-invalid
tB --> tC
tB --::N-- tC
```

```ipmt-invalid
tB --::N-- tC
tB --> tC
```

### INVALID: Opposite part-of edge (violates mutual exclusivity)

**Invalid** - PartOf and NearTo are different base types:
```ipmt-invalid
tC <--::P-- tB
tB --- tC
```

```ipmt-invalid
tB --- tC
tC <--::P-- tB
```

**Invalid** - PartOf (default arrow) and NearTo are different base types:
```ipmt-invalid
tC <-- tB
tB --::N-- tC
```

```ipmt-invalid
tB --::N-- tC
tC <-- tB
```


### INVALID: Opposite part-of with the same near to edge (violates mutual exclusivity)

**Invalid** - PartOf and NearTo are different base types:
```ipmt-invalid
tC --::P--> tD
tD --- tC
```
```ipmt-invalid
tD --- tC
tC --::P--> tD
```

**Invalid** - LeadsTo (default arrow) and NearTo are different base types:
```ipmt-invalid
tC --> tD
tD --::N-- tC
```

```ipmt-invalid
tD --::N-- tC
tC --> tD
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
c-X --- c-Y ::c
c-Y ::c <--::X-- c-X
```

**Invalid** - Expresses (default arrow) and NearTo are different base types:
```ipmt-invalid
c-Y ::c <-- c-X ::c
c-X --::N-- c-Y ::c
```

```ipmt-invalid
c-X --::N-- c-Y ::c
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
A-->B--"text"-->C
A-->B
```
