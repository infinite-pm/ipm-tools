package markdown

import (
	"regexp"
	"strings"
)

var (
	// Regex to match non-alphanumeric characters (except hyphens, spaces, slashes, and dots)
	nonAlphanumeric = regexp.MustCompile(`[^a-z0-9\s\-/.]`)
	// Regex to match multiple spaces or hyphens
	multipleSpacesOrHyphens = regexp.MustCompile(`[\s-]+`)
	// Regex to match consecutive slashes
	multipleSlashes = regexp.MustCompile(`/+`)
)

// Generate creates a GitHub-like, ASCII-only markdown anchor slug from a
// heading title. Rules:
//   - Convert to lowercase
//   - Replace spaces with hyphens
//   - Remove special characters except hyphens, slashes, and dots (slashes
//     and dots are retained, unlike GitHub which strips them)
//   - Collapse consecutive slashes into single slash
//   - Remove leading/trailing hyphens
//   - Collapse multiple hyphens into one
//
// Note: this is ASCII-only. Unlike GitHub, all non-ASCII letters are
// dropped (e.g. "Über Café" → "ber-caf", "日本語" → ""). Callers compare two
// Generate outputs against each other, so the divergence is self-consistent
// for matching, but it is NOT byte-equal to a GitHub heading anchor.
func Generate(title string) string {
	// Convert to lowercase
	anchor := strings.ToLower(title)

	// Remove special characters (keep spaces, hyphens, slashes, and dots for now)
	anchor = nonAlphanumeric.ReplaceAllString(anchor, "")

	// Replace spaces with hyphens and collapse multiple hyphens/spaces
	anchor = multipleSpacesOrHyphens.ReplaceAllString(anchor, "-")

	// Collapse multiple slashes into single slash
	anchor = multipleSlashes.ReplaceAllString(anchor, "/")

	// Trim leading/trailing hyphens
	anchor = strings.Trim(anchor, "-")

	return anchor
}
