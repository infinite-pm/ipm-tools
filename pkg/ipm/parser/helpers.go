package parser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// Options holds optional settings for the parser.
type Options struct {
	Filename string
	// DefaultNodeType is the kind of a node spec without a kind marker. The
	// zero value means model.Thing (the current grammar); model.Event makes a
	// bare spec an event. Explicit markers and ::?et always win.
	DefaultNodeType model.NodeType
	// SpecHook, when set, is called for every node spec the parser resolves
	// (see SpecEvent). Used by cmd-dev/ipmt-migrate; nil for every other caller.
	SpecHook func(SpecEvent)
}

// SpecEvent describes one resolved node spec occurrence, with raw byte
// offsets into the parsed input.
type SpecEvent struct {
	Spec NodeSpec
	// Node is the node the spec resolved to (its kind is the first one seen).
	Node model.Node
	// NameRaw, AliasMarkRaw: raw [start,end) of the name and of the ::a marker
	// ({-1,-1} when absent or empty).
	NameRaw      [2]int
	AliasMarkRaw [2]int
	// AliasRef is true when the spec is a bare reference to an alias defined
	// earlier (the name is the node's alias, not its name).
	AliasRef bool
}

// ParseIPMTBytes parses IPMT source bytes using the same options as the CLI.
func ParseIPMTBytes(data []byte, filename string) (*model.IpmGraph, error) {
	return Parse(data, Options{Filename: filename})
}

// MustJSON pretty prints the document JSON (helper for CLI/tests).
func MustJSON(doc *model.IpmGraph) string {
	b, err := json.MarshalIndent(doc, "", "\t")
	if err != nil {
		panic(err)
	}
	return string(b)
}

// unescapeQuoted processes backslash escape sequences inside quoted strings.
// Supports the four escapes the serializer (pkg/ipmtext) emits: \n (newline),
// \t (tab), \\ (backslash), \" (double quote). Others are errors.
func unescapeQuoted(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
			case 't':
				b.WriteByte('\t')
				i++
			case '\\':
				b.WriteByte('\\')
				i++
			case '"':
				b.WriteByte('"')
				i++
			default:
				return "", fmt.Errorf("invalid escape \\%c", s[i+1])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

// stripQuotes removes surrounding double quotes and unescapes. If not quoted, returns unchanged.
func stripQuotes(s string) (string, error) {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return unescapeQuoted(s[1 : len(s)-1])
	}
	return s, nil
}

// containsCommaOutsideQuotes reports whether s contains a comma that is not inside a double-quoted string.
func containsCommaOutsideQuotes(s string) bool {
	inQuote := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' && !isEscaped(s, i) {
			inQuote = !inQuote
			continue
		}
		if !inQuote && s[i] == ',' {
			return true
		}
	}
	return false
}
