// Package mdembed implements the marker grammar, hash normalization, and SVG
// metadata embedding shared between cmd/md-embed and any other tool that
// needs to read or write in-place ipmt rendering markers in Markdown.
//
// Marker shape (two lines per embed — required for CommonMark to render the
// image; if the HTML comment and image were on the same line, the comment
// would open an HTML block that swallows the image):
//
//	<!-- ipm-svg id=01 hash=ab12cd34 [path=./alt.svg] [pos=before] -->
//	![alt](./_ipm/foo/01.ipm.svg)
//
// The marker is independent of *how* the source was declared. A `\`\`\`ipmt`
// fence and an `<!-- ipm-include src=... -->` line are two equivalent ways
// to declare a block; both produce the same marker shape.
package mdembed

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Marker is a parsed two-line marker (a `<!-- ipm-svg ... -->` line followed
// by an `![alt](img)` line).
//
// Unknown attributes are preserved in Unknown to support forward compatibility:
// a future version of the toolchain may add new marker attributes, and an
// older parser should round-trip them faithfully rather than dropping them.
// See the schema-version rationale in the design notes.
type Marker struct {
	ID        string // e.g. "01"; ties to the SVG filename and to the source block within the file
	Hash      string // 8-char sha256 prefix of the normalized ipmt source
	Path      string // optional override for the SVG location (relative to the .md file)
	Pos       string // "" (= after, default) or "before"; where the marker pair sits relative to the block
	ImageAlt  string // alt text inside the image link; "" by default
	ImagePath string // path inside the image link; usually equals Path when Path is set

	// Unknown holds attributes the parser did not recognize, in their
	// original encounter order. FormatLines emits them at the tail of the
	// known-attribute list so a round-trip preserves both the values and
	// the relative ordering of known→unknown.
	Unknown []UnknownAttr
}

// UnknownAttr is a marker attribute the parser did not recognize. Held by
// Marker so unknown attributes round-trip cleanly.
type UnknownAttr struct {
	Key   string
	Value string
}

var (
	commentLineRE = regexp.MustCompile(`^\s*<!--\s+ipm-svg\s+(.+?)\s+-->\s*$`)
	imageLineRE   = regexp.MustCompile(`^\s*!\[([^\]]*)\]\(([^)]+)\)\s*$`)
)

// ParseMarker parses a two-line marker. Pass the candidate comment line and
// the line that immediately follows it. Returns ok=false unless both lines
// match the expected shapes and the comment carries at least id= and hash=.
func ParseMarker(commentLine, imageLine string) (Marker, bool) {
	c := commentLineRE.FindStringSubmatch(commentLine)
	if c == nil {
		return Marker{}, false
	}
	img := imageLineRE.FindStringSubmatch(imageLine)
	if img == nil {
		return Marker{}, false
	}
	out := Marker{
		ImageAlt:  img[1],
		ImagePath: img[2],
	}
	for kv := range strings.FieldsSeq(c[1]) {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key, val := kv[:eq], kv[eq+1:]
		switch key {
		case "id":
			out.ID = val
		case "hash":
			out.Hash = val
		case "path":
			out.Path = val
		case "pos":
			// Only "before" is a non-default value; "after" (and anything else)
			// canonicalizes to "" so FormatLines doesn't emit the attribute.
			if val == "before" {
				out.Pos = "before"
			}
		default:
			// Unknown attribute — preserve for round-trip.
			out.Unknown = append(out.Unknown, UnknownAttr{Key: key, Value: val})
		}
	}
	if out.ID == "" || out.Hash == "" {
		return Marker{}, false
	}
	return out, true
}

// FormatLines renders the marker as a two-element slice — [comment, image] —
// suitable for inserting into a `[]string` of file lines. Known attribute
// order is fixed (id, hash, path, pos) so output is stable across runs;
// unrecognized attributes (m.Unknown) follow in the order they were
// originally parsed.
func (m Marker) FormatLines() [2]string {
	var b strings.Builder
	b.WriteString("<!-- ipm-svg")
	attrs := []struct{ k, v string }{
		{"id", m.ID},
		{"hash", m.Hash},
		{"path", m.Path},
		{"pos", m.Pos},
	}
	for _, a := range attrs {
		if a.v == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(a.k)
		b.WriteByte('=')
		b.WriteString(a.v)
	}
	for _, u := range m.Unknown {
		if u.Key == "" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(u.Key)
		b.WriteByte('=')
		b.WriteString(u.Value)
	}
	b.WriteString(" -->")
	img := "![" + m.ImageAlt + "](" + m.ImagePath + ")"
	return [2]string{b.String(), img}
}

// Format renders the marker as the two lines joined with a newline. Useful
// for diff-style output and for tests; callers that need to splice into a
// line slice should prefer FormatLines.
func (m Marker) Format() string {
	ls := m.FormatLines()
	return ls[0] + "\n" + ls[1]
}

// IsMarkerCommentLine reports whether line is the comment portion of a marker.
// Cheap pre-filter; callers still need ParseMarker (with the following line)
// for the full structure.
func IsMarkerCommentLine(line string) bool {
	return commentLineRE.MatchString(line)
}

// AttrsString is a helper for tests / debug output; it returns the attributes
// of m as a deterministic "k=v k=v" string.
func (m Marker) AttrsString() string {
	pairs := map[string]string{}
	if m.ID != "" {
		pairs["id"] = m.ID
	}
	if m.Hash != "" {
		pairs["hash"] = m.Hash
	}
	if m.Path != "" {
		pairs["path"] = m.Path
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(pairs))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%s", k, pairs[k]))
	}
	return strings.Join(out, " ")
}
