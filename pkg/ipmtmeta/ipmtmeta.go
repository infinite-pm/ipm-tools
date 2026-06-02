// Package ipmtmeta defines the ipmt processing-pipeline flag vocabulary and the
// `# ipmt:` first-line pragma, and reconciles the two carriers of that metadata:
//
//   - a ```ipmt fence info-string ("```ipmt unresolved" → ["unresolved"]),
//     extracted by pkg/markdown.FenceMeta; and
//   - a `# ipmt:` first-non-empty-line comment inside the ipmt source, which is
//     the ONLY channel available to standalone .ipmt files and <!-- ipm-include -->
//     blocks (they have no fence).
//
// The flags select how the block is PROCESSED (solver, embed). The negative-
// example `invalid` case is deliberately NOT a flag here — it is a separate
// fence language (```ipmt-invalid), skipped by every render tool.
//
// Strictness (the design takes no backward-compatibility burden — tools and
// files upgrade together):
//   - the pragma is valid ONLY on the first non-empty line; a pragma-shaped
//     line anywhere else is an error (no silently-dead pragmas);
//   - an unknown/malformed flag token (from the fence OR the pragma) is an
//     error, not silently ignored.
package ipmtmeta

import (
	"fmt"
	"slices"
	"strings"
)

// Flag tokens. Bare tokens plus the one key=value token embed=false.
// `draft` (the lenient-authoring mode) is reserved but NOT registered until
// the mode ships — today it is rejected like any other unknown flag.
const (
	FlagUnresolved = "unresolved"
	FlagDefaults   = "defaults"
	FlagEmbedFalse = "embed=false"
)

var knownFlags = map[string]bool{
	FlagUnresolved: true,
	FlagDefaults:   true,
	FlagEmbedFalse: true,
}

// Contains reports whether meta carries flag.
func Contains(meta []string, flag string) bool {
	return slices.Contains(meta, flag)
}

// isPragmaLine reports whether a source line is a `# ipmt:` pragma (a `#`
// comment whose first word is `ipmt:`). `# ipmt is nice` (no colon) is an
// ordinary comment, not a pragma.
func isPragmaLine(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "#") {
		return false
	}
	t = strings.TrimSpace(strings.TrimLeft(t, "#"))
	return strings.HasPrefix(t, "ipmt:")
}

// pragmaTokens returns the whitespace-separated tokens after `ipmt:` on a
// pragma line (assumes isPragmaLine(line) is true).
func pragmaTokens(line string) []string {
	t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
	return strings.Fields(strings.TrimPrefix(t, "ipmt:"))
}

// MetaFromContent extracts and validates the `# ipmt:` pragma tokens from ipmt
// source. It returns nil when there is no pragma. It errors if a pragma-shaped
// line is not the first non-empty line, or carries an unknown flag.
func MetaFromContent(content string) ([]string, error) {
	lines := strings.Split(content, "\n")
	firstNonEmpty := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			firstNonEmpty = i
			break
		}
	}
	var tokens []string
	for i, l := range lines {
		if !isPragmaLine(l) {
			continue
		}
		if i != firstNonEmpty {
			return nil, fmt.Errorf("# ipmt: pragma must be on the first non-empty line (found on line %d)", i+1)
		}
		for _, tok := range pragmaTokens(l) {
			if !knownFlags[tok] {
				return nil, fmt.Errorf("unknown # ipmt: flag %q", tok)
			}
			tokens = append(tokens, tok)
		}
	}
	return tokens, nil
}

// EffectiveMeta returns the validated, de-duplicated union of the fence-info
// tokens and the content pragma tokens (fence order first, then pragma). An
// unknown token from either source is an error.
func EffectiveMeta(fenceTokens []string, content string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(tok string) error {
		if !knownFlags[tok] {
			return fmt.Errorf("unknown ipmt flag %q", tok)
		}
		if !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
		return nil
	}
	for _, tok := range fenceTokens {
		if err := add(tok); err != nil {
			return nil, err
		}
	}
	pragma, err := MetaFromContent(content)
	if err != nil {
		return nil, err
	}
	for _, tok := range pragma {
		if err := add(tok); err != nil {
			return nil, err
		}
	}
	return out, nil
}
