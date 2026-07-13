package nameutil

import (
	"os"
	"path/filepath"
	"testing"
)

// testItem is a test implementation of the Item interface
type testItem struct {
	baseName string
}

func (t *testItem) GetBaseName() string {
	return t.baseName
}

func (t *testItem) SetBaseName(name string) {
	t.baseName = name
}

// TestResolveCollisions_DefaultConfig tests collision resolution with default config
func TestResolveCollisions_DefaultConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
		config   CollisionConfig
	}{
		{
			name:     "no collisions",
			input:    []string{"file1", "file2", "file3"},
			expected: []string{"file1", "file2", "file3"},
			config:   DefaultCollisionConfig(),
		},
		{
			name:     "two collisions - default (first no suffix)",
			input:    []string{"file", "file"},
			expected: []string{"file", "file-02"},
			config:   DefaultCollisionConfig(),
		},
		{
			name:     "three collisions - default",
			input:    []string{"file", "file", "file"},
			expected: []string{"file", "file-02", "file-03"},
			config:   DefaultCollisionConfig(),
		},
		{
			name:     "two collisions - first with suffix",
			input:    []string{"file", "file"},
			expected: []string{"file-01", "file-02"},
			config:   CollisionConfig{PaddingDigits: 2, FirstWithSuffix: true},
		},
		{
			name:     "three collisions - 3 digit padding",
			input:    []string{"file", "file", "file"},
			expected: []string{"file", "file-002", "file-003"},
			config:   CollisionConfig{PaddingDigits: 3, FirstWithSuffix: false},
		},
		{
			name:     "three collisions - 3 digit padding, first with suffix",
			input:    []string{"file", "file", "file"},
			expected: []string{"file-001", "file-002", "file-003"},
			config:   CollisionConfig{PaddingDigits: 3, FirstWithSuffix: true},
		},
		{
			name:     "mixed - some collisions",
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "a-02", "c", "b-02"},
			config:   DefaultCollisionConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create items
			items := make([]*testItem, len(tt.input))
			for i, name := range tt.input {
				items[i] = &testItem{baseName: name}
			}

			// Resolve collisions
			ResolveCollisions(items, tt.config)

			// Check results
			for i, expected := range tt.expected {
				if items[i].GetBaseName() != expected {
					t.Errorf("item[%d]: got %q, want %q", i, items[i].GetBaseName(), expected)
				}
			}
		})
	}
}

// TestNameReserver tests incremental name reservation
func TestNameReserver(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name           string
		config         CollisionConfig
		reservations   []struct{ base, ext string }
		expected       []string
		createExisting []string // Files to create before running test
	}{
		{
			name:   "no collisions",
			config: DefaultCollisionConfig(),
			reservations: []struct{ base, ext string }{
				{"file1", ".txt"},
				{"file2", ".txt"},
			},
			expected: []string{"file1.txt", "file2.txt"},
		},
		{
			name:   "collision in memory - default",
			config: DefaultCollisionConfig(),
			reservations: []struct{ base, ext string }{
				{"file", ".txt"},
				{"file", ".txt"},
				{"file", ".txt"},
			},
			expected: []string{"file.txt", "file-02.txt", "file-03.txt"},
		},
		{
			name:           "collision with filesystem",
			config:         DefaultCollisionConfig(),
			createExisting: []string{"file.txt"},
			reservations: []struct{ base, ext string }{
				{"file", ".txt"},
				{"file", ".txt"},
			},
			expected: []string{"file-02.txt", "file-03.txt"},
		},
		{
			name:   "first with suffix",
			config: CollisionConfig{PaddingDigits: 2, FirstWithSuffix: true},
			reservations: []struct{ base, ext string }{
				{"file", ".txt"},
				{"file", ".txt"},
			},
			expected: []string{"file-01.txt", "file-02.txt"},
		},
		{
			name:   "3 digit padding",
			config: CollisionConfig{PaddingDigits: 3, FirstWithSuffix: false},
			reservations: []struct{ base, ext string }{
				{"file", ".txt"},
				{"file", ".txt"},
			},
			expected: []string{"file.txt", "file-002.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test directory
			testDir := filepath.Join(tmpDir, tt.name)
			if err := os.MkdirAll(testDir, 0o755); err != nil {
				t.Fatal(err)
			}

			// Create existing files
			for _, fname := range tt.createExisting {
				fpath := filepath.Join(testDir, fname)
				if err := os.WriteFile(fpath, []byte("test"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			// Create reserver
			reserver := NewNameReserver(testDir, tt.config)

			// Reserve names
			results := make([]string, len(tt.reservations))
			for i, res := range tt.reservations {
				name, err := reserver.ReserveName(res.base, res.ext)
				if err != nil {
					t.Fatalf("reservation %d failed: %v", i, err)
				}
				results[i] = name
			}

			// Check results
			for i, expected := range tt.expected {
				if results[i] != expected {
					t.Errorf("reservation[%d]: got %q, want %q", i, results[i], expected)
				}
			}
		})
	}
}
