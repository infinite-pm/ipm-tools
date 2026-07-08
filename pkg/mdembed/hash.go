package mdembed

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// HashPrefixLen is the number of hex characters emitted in marker hash
// attributes. 8 hex chars = 32 bits; per-file collisions are vanishingly rare
// and the per-block ID is the real correlation key.
const HashPrefixLen = 8

// NormalizeIPMT canonicalizes an ipmt source so cosmetic edits (trailing
// whitespace, blank lines at the end, CRLF) do not change the hash. The
// normalized form is what HashIPMT consumes; the same normalization runs in
// the in-place embed tool and the vscode extension, so they agree byte-for-byte.
func NormalizeIPMT(src string) string {
	src = markdown.NormalizeLF(src)
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// HashIPMT returns the HashPrefixLen-character lowercase-hex sha256 prefix of
// the normalized source.
func HashIPMT(src string) string {
	sum := sha256.Sum256([]byte(NormalizeIPMT(src)))
	return hex.EncodeToString(sum[:])[:HashPrefixLen]
}
