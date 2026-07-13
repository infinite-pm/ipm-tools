package filterutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ParserMeta contains metadata about the parser
type ParserMeta struct {
	ItemType    string `json:"item_type"`
	ItemSubtype string `json:"item_subtype"`
}

// RefEntry represents a source file entry in _refs.json
type RefEntry struct {
	Source     string                 `json:"source"`
	SourceSHA1 string                 `json:"source_sha1"`
	Name       string                 `json:"name,omitempty"`
	Method     string                 `json:"method,omitempty"`
	ParserName string                 `json:"parser_name,omitempty"`
	ParserMeta map[string]interface{} `json:"parser_meta,omitempty"`
	Items      []RefItem              `json:"items"`
}

// RefItem represents a destination item in _refs.json
type RefItem struct {
	Destination string            `json:"destination"`
	Line        int               `json:"line,omitempty"`
	Anchor      string            `json:"anchor,omitempty"`
	ContentSHA1 string            `json:"content_sha1"`
	Outputs     map[string]string `json:"outputs,omitempty"`
	Heading     string            `json:"heading,omitempty"` // ### heading where test is defined
	Section     string            `json:"section,omitempty"` // ## section where test is defined
	Tags        []string          `json:"tags,omitempty"`    // Test tags from @test-tags comment
}

// FindProjectRoot searches for .git directory or tests/sources.json starting from current directory
func FindProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		// Check for .git directory
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
			return dir, nil
		}

		// Check for tests/sources.json
		sourcesPath := filepath.Join(dir, "tests", "sources.json")
		if _, err := os.Stat(sourcesPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("project root not found (no .git or tests/sources.json)")
}

// ExtractStemFromFilter extracts the stem (base filename without extension) from a filter string.
// If the filter is a path, it resolves it and extracts the stem.
// Returns the stem and absolute path (if filter was a path), or error.
func ExtractStemFromFilter(filter string) (stem string, absPath string, err error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", "", fmt.Errorf("filter must be non-empty")
	}

	// Check if filter is a path (contains path separator or file extension)
	if strings.Contains(filter, string(filepath.Separator)) || filepath.Ext(filter) != "" {
		// Treat as path - try to resolve it
		if filepath.IsAbs(filter) {
			absPath = filter
		} else {
			// Find project root by looking for .git or sources.json
			root, err := FindProjectRoot()
			if err != nil {
				return "", "", fmt.Errorf("cannot resolve relative path without project root: %w", err)
			}
			absPath = filepath.Join(root, filter)
		}

		// Verify the path exists
		if _, err := os.Stat(absPath); err != nil {
			return "", "", fmt.Errorf("filter path does not exist: %s (%w)", filter, err)
		}

		// Extract stem from the path (basename without extension)
		base := filepath.Base(absPath)
		stem = strings.TrimSuffix(base, filepath.Ext(base))

		// Normalize to lowercase
		stem = strings.ToLower(stem)
		return stem, absPath, nil
	}

	// Filter is already a stem, just normalize it
	stem = strings.ToLower(filter)
	return stem, "", nil
}

// LoadRefs loads _refs.json from the given directory
func LoadRefs(dirPath string) ([]RefEntry, error) {
	refsPath := filepath.Join(dirPath, "_refs.json")
	b, err := os.ReadFile(refsPath)
	if err != nil {
		return nil, err
	}
	var refs []RefEntry
	if err := json.Unmarshal(b, &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

// FindDestinationForStem searches for a stem in all destination directories and returns
// the unique destination directory path. Returns error if stem is not found or is ambiguous.
func FindDestinationForStem(baseDir string, destinations []string, stem string) (string, error) {
	var matches []string
	for _, dest := range destinations {
		destPath := filepath.Join(baseDir, dest)
		refs, err := LoadRefs(destPath)
		if err != nil {
			continue
		}
		found := false
		for _, entry := range refs {
			for _, item := range entry.Items {
				if item.Destination == stem {
					matches = append(matches, dest)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("stem %q not found in any _refs.json", stem)
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", fmt.Errorf("stem %q is ambiguous (found in multiple destination dirs): %s", stem, strings.Join(matches, ", "))
	}
	return matches[0], nil
}

// ResolveFilterToDestination resolves a filter (stem or path) to a destination directory and normalized stem.
// It handles both simple stem names and full file paths.
func ResolveFilterToDestination(baseDir string, destinations []string, filter string, originalFilter string) (destDir string, stem string, err error) {
	stem, absPath, err := ExtractStemFromFilter(filter)
	if err != nil {
		return "", "", err
	}

	// If we have an absolute path, try to determine destination from the path structure
	if absPath != "" {
		relPath, err := filepath.Rel(baseDir, absPath)
		if err == nil && !strings.HasPrefix(relPath, "..") {
			// Path is within baseDir, extract destination directory
			parts := strings.Split(filepath.ToSlash(relPath), "/")
			if len(parts) >= 2 {
				// Try to match against known destinations
				for i := 1; i <= len(parts)-1; i++ {
					possibleDest := filepath.Join(parts[:i]...)
					for _, dest := range destinations {
						if dest == possibleDest {
							// Verify stem exists in this destination's _refs.json
							destPath := filepath.Join(baseDir, dest)
							refs, err := LoadRefs(destPath)
							if err == nil {
								for _, entry := range refs {
									for _, item := range entry.Items {
										if item.Destination == stem {
											return dest, stem, nil
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Fall back to searching all destinations
	dest, err := FindDestinationForStem(baseDir, destinations, stem)
	if err != nil {
		// Enhance error message if original filter was a path
		if absPath != "" && originalFilter != stem {
			return "", "", fmt.Errorf("filter path %q (stem %q) not found in any _refs.json", originalFilter, stem)
		}
		return "", "", fmt.Errorf("filter stem not found in any _refs.json: %q", stem)
	}

	return dest, stem, nil
}
