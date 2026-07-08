package mdembed

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Meta describes the per-SVG metadata embedded in the generated artifact so a
// refresh tool can detect drift, orphaned files, and renderer-version
// staleness without parsing the source `.md` again.
type Meta struct {
	Hash        string // matches Marker.Hash in the .md
	SourceID    string // matches Marker.ID
	SourceFile  string // forward-slash, repo-root-relative path of the source .md
	GeneratedBy string // tool name + version, e.g. "md-embed@dev"
}

const svgMetaPrefix = "ipm-svg meta:"

var svgMetaCommentRE = regexp.MustCompile(`(?s)<!--\s*` + svgMetaPrefix + `(.*?)-->`)
var svgRootOpenRE = regexp.MustCompile(`(?s)<svg\b[^>]*>`)

// generatedByFieldRE matches the `generated-by=<token>` field plus one trailing
// space inside an embedded meta comment, so it can be elided for comparison.
var generatedByFieldRE = regexp.MustCompile(`generated-by=\S+\s?`)

// EqualExceptGeneratedBy reports whether SVG byte slices a and b are identical
// once the embedded `generated-by=<tool@version>` provenance field is
// disregarded. A re-render that changes only which tool/version produced the
// artifact (e.g. ipm-rpc@… → md-embed@dev) therefore compares equal, letting
// callers leave the file untouched instead of churning version control.
func EqualExceptGeneratedBy(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	return bytes.Equal(
		generatedByFieldRE.ReplaceAll(a, nil),
		generatedByFieldRE.ReplaceAll(b, nil),
	)
}

// EmbedMeta returns svg with an XML comment carrying meta inserted just after
// the opening <svg ...> tag. Any pre-existing ipm-svg meta comment is replaced.
// The function does not validate or re-emit other svg content; it operates on
// raw bytes so it survives whatever ipmsvg-gen produced.
func EmbedMeta(svg []byte, meta Meta) []byte {
	cleaned := svgMetaCommentRE.ReplaceAll(svg, nil)
	cleaned = stripDoubleBlankLines(cleaned)
	loc := svgRootOpenRE.FindIndex(cleaned)
	if loc == nil {
		// Not a recognizable SVG; return as-is so callers can decide what to do.
		return svg
	}
	insertAt := loc[1]
	comment := "\n  " + formatMetaComment(meta) + "\n"
	out := make([]byte, 0, len(cleaned)+len(comment))
	out = append(out, cleaned[:insertAt]...)
	out = append(out, []byte(comment)...)
	out = append(out, cleaned[insertAt:]...)
	return out
}

// ExtractMeta scans svg for an embedded ipm-svg meta comment and parses it.
// Returns the zero Meta and ok=false if no comment is present.
func ExtractMeta(svg []byte) (Meta, bool) {
	m := svgMetaCommentRE.FindSubmatch(svg)
	if m == nil {
		return Meta{}, false
	}
	out := Meta{}
	for kv := range strings.FieldsSeq(string(m[1])) {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key, val := kv[:eq], kv[eq+1:]
		switch key {
		case "hash":
			out.Hash = val
		case "source-id":
			out.SourceID = val
		case "source-file":
			out.SourceFile = val
		case "generated-by":
			out.GeneratedBy = val
		}
	}
	if out.Hash == "" || out.SourceID == "" {
		return Meta{}, false
	}
	return out, true
}

func formatMetaComment(meta Meta) string {
	pairs := map[string]string{
		"hash":         meta.Hash,
		"source-id":    meta.SourceID,
		"source-file":  meta.SourceFile,
		"generated-by": meta.GeneratedBy,
	}
	keys := make([]string, 0, len(pairs))
	for k, v := range pairs {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, pairs[k]))
	}
	return fmt.Sprintf("<!-- %s %s -->", svgMetaPrefix, strings.Join(parts, " "))
}

// stripDoubleBlankLines collapses a single run of three newlines ("\n\n\n",
// i.e. one blank line too many) down to two ("\n\n", a single blank line). It
// does NOT fully collapse longer runs — "\n\n\n\n\n" becomes "\n\n\n\n", still a
// double blank line — because the non-overlapping ReplaceAll only removes one
// newline per match. That is sufficient for its sole caller, EmbedMeta, where
// removing a prior one-line meta comment can leave at most one extra blank line
// to clean up. Do not rely on it as a general blank-line normalizer.
func stripDoubleBlankLines(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\n\n\n"), []byte("\n\n"))
}
