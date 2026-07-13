# nameutil

Package `nameutil` provides utilities for resolving name collisions by assigning sequential numeric suffixes.

## Purpose

This package helps manage situations where multiple items share the same base name and need unique identifiers. It provides two strategies:

1. **Batch processing**: When all items are known upfront (`ResolveCollisions`)
2. **Incremental processing**: When items are processed one-by-one with filesystem checks (`NameReserver`)

## Configuration

Collision resolution behavior is controlled by `CollisionConfig`:

```go
type CollisionConfig struct {
    PaddingDigits   int  // Number of zeros to pad (default: 2)
    FirstWithSuffix bool // Whether first item gets a suffix (default: false)
}
```

**Examples:**
- `PaddingDigits: 2, FirstWithSuffix: false` → `file`, `file-02`, `file-03`
- `PaddingDigits: 2, FirstWithSuffix: true` → `file-01`, `file-02`, `file-03`
- `PaddingDigits: 3, FirstWithSuffix: false` → `file`, `file-002`, `file-003`

## Functions

### ResolveCollisions

```go
func ResolveCollisions[T Item](items []T, cfg CollisionConfig)
```

Resolves collisions for a batch of items in memory. Items sharing the same base name get sequential suffixes assigned according to the configuration.

**Use when:** All items are known upfront and you want to process them in a single pass.

**Example:**
```go
type MyItem struct {
    name string
}

func (m *MyItem) GetBaseName() string { return m.name }
func (m *MyItem) SetBaseName(name string) { m.name = name }

items := []*MyItem{
    {name: "file"},
    {name: "file"},
    {name: "file"},
}

nameutil.ResolveCollisions(items, nameutil.DefaultCollisionConfig())
// Result: items[0].name = "file"
//         items[1].name = "file-02"
//         items[2].name = "file-03"
```

### NameReserver

```go
type NameReserver struct {
    OutDir string
    Config CollisionConfig
    // ... internal fields
}

func NewNameReserver(outDir string, cfg CollisionConfig) *NameReserver
func (r *NameReserver) ReserveName(base, ext string) (string, error)
```

Incrementally reserves unique names, checking both in-memory state and filesystem.

**Use when:** Processing items one-by-one and need to avoid conflicts with existing files.

**Example:**
```go
reserver := nameutil.NewNameReserver("/output", nameutil.DefaultCollisionConfig())

name1, _ := reserver.ReserveName("file", ".txt")  // "file.txt"
name2, _ := reserver.ReserveName("file", ".txt")  // "file-02.txt"
name3, _ := reserver.ReserveName("file", ".txt")  // "file-03.txt"
```

## Item Interface

To use `ResolveCollisions`, types must implement the `Item` interface:

```go
type Item interface {
    GetBaseName() string
    SetBaseName(name string)
}
```

## Used By

- [cmd-dev/sync-test-cases](../../cmd-dev/sync-test-cases/main.go) - Batch collision resolution for test fixtures

