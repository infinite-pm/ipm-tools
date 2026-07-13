package nameutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// CollisionConfig controls how name collisions are resolved.
type CollisionConfig struct {
	// PaddingDigits specifies how many digits to use for suffix numbers (default: 2)
	// Examples: 2 -> "-02", 3 -> "-003"
	PaddingDigits int

	// FirstWithSuffix determines whether the first item gets a suffix
	// false (default): first="base", second="base-02", third="base-03"
	// true: first="base-01", second="base-02", third="base-03"
	FirstWithSuffix bool
}

// DefaultCollisionConfig returns the default configuration.
// - PaddingDigits: 2 (produces -02, -03, etc.)
// - FirstWithSuffix: false (first item has no suffix)
func DefaultCollisionConfig() CollisionConfig {
	return CollisionConfig{
		PaddingDigits:   2,
		FirstWithSuffix: false,
	}
}

// Item represents a named item that may need collision resolution.
type Item interface {
	GetBaseName() string
	SetBaseName(name string)
}

// ResolveCollisions assigns unique names to items with the same base name.
// Items are grouped by their current base name, and suffixes are assigned
// according to the collision config.
//
// This is useful for batch processing where all items are known upfront.
func ResolveCollisions[T Item](items []T, cfg CollisionConfig) {
	if cfg.PaddingDigits <= 0 {
		cfg.PaddingDigits = 2
	}

	// Group items by base name
	groups := make(map[string][]T)
	for _, item := range items {
		base := item.GetBaseName()
		groups[base] = append(groups[base], item)
	}

	// Assign suffixes within each group
	for base, group := range groups {
		if len(group) == 1 && !cfg.FirstWithSuffix {
			// Single item keeps base name (unless FirstWithSuffix is true)
			continue
		}

		startIdx := 0
		if !cfg.FirstWithSuffix {
			startIdx = 1 // First item keeps base name
		}

		for i := startIdx; i < len(group); i++ {
			// Suffix is always i+1; the two modes differ only in startIdx
			// (FirstWithSuffix starts at index 0 -> -01, otherwise index 1 -> -02).
			suffix := i + 1
			newName := fmt.Sprintf("%s-%0*d", base, cfg.PaddingDigits, suffix)
			group[i].SetBaseName(newName)
		}
	}
}

// NameReserver helps reserve unique names incrementally, checking both
// in-memory state and filesystem.
//
// This is useful for processing items one-by-one where you need to check
// if files already exist on disk.
type NameReserver struct {
	OutDir      string
	Config      CollisionConfig
	used        map[string]struct{}
	maxAttempts int
}

// NewNameReserver creates a new name reserver for the given output directory.
func NewNameReserver(outDir string, cfg CollisionConfig) *NameReserver {
	if cfg.PaddingDigits <= 0 {
		cfg.PaddingDigits = 2
	}
	return &NameReserver{
		OutDir:      outDir,
		Config:      cfg,
		used:        make(map[string]struct{}),
		maxAttempts: 1000,
	}
}

// ReserveName attempts to reserve a unique name for the given base and extension.
// It first tries the base name, then adds numeric suffixes until an available name is found.
// Returns the reserved name (including extension) or an error if no name could be found.
func (r *NameReserver) ReserveName(base, ext string) (string, error) {
	// Try base name first (unless FirstWithSuffix is true)
	if !r.Config.FirstWithSuffix {
		name := base + ext
		if !r.nameTaken(name) {
			r.used[name] = struct{}{}
			return name, nil
		}
	}

	// Try with numeric suffixes. FirstWithSuffix numbers from -01; otherwise
	// the bare base was already tried above, so start at -02.
	startIdx := 1
	if !r.Config.FirstWithSuffix {
		startIdx = 2
	}

	for i := startIdx; i < r.maxAttempts; i++ {
		nameWithoutExt := fmt.Sprintf("%s-%0*d", base, r.Config.PaddingDigits, i)
		name := nameWithoutExt + ext
		if !r.nameTaken(name) {
			r.used[name] = struct{}{}
			return name, nil
		}
	}

	return "", fmt.Errorf("cannot allocate name for %s (tried %d names)", base+ext, r.maxAttempts)
}

// nameTaken checks if a name is already used (in-memory) or exists on filesystem.
func (r *NameReserver) nameTaken(name string) bool {
	if _, ok := r.used[name]; ok {
		return true
	}
	if _, err := os.Stat(filepath.Join(r.OutDir, name)); err == nil {
		return true
	}
	return false
}
