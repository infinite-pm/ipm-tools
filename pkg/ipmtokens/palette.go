package ipmtokens

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// palette.json is the single source of truth for the ipmt token palette:
// the (token-type, modifier) -> CSS-class-suffix mapping plus each style's
// color. It is embedded here so ipm-tools is self-contained; pkg/mdhtml
// composes its highlight CSS from PaletteCSSRules(), and
// ../vscode-ipm/scripts/gen-palette.ts reads this same file to generate the
// editor rules, preview CSS, and src/palette.gen.ts. Token-type indices and
// modifier bits live in tokens.go (SemanticTokenTypes / SemanticTokenModifiers).
//
//go:embed palette.json
var paletteJSON []byte

// PaletteRule is one (type, optional modifier) styling entry.
type PaletteRule struct {
	Type   string `json:"type"`
	Mod    string `json:"mod,omitempty"`
	Class  string `json:"class"`
	FG     string `json:"fg"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
}

// Palette is the ordered rule list parsed from palette.json.
var Palette []PaletteRule

var modBitByName = map[string]uint32{
	"alias":     ModAlias,
	"leadsTo":   ModLeadsTo,
	"partOf":    ModPartOf,
	"expresses": ModExpresses,
	"nearTo":    ModNearTo,
	"hasAlias":  ModHasAlias,
	"marker":    ModMarker,
}

func init() {
	var doc struct {
		Rules []PaletteRule `json:"rules"`
	}
	if err := json.Unmarshal(paletteJSON, &doc); err != nil {
		panic("ipmtokens: invalid palette.json: " + err.Error())
	}
	for _, r := range doc.Rules {
		if typeIndex(r.Type) < 0 {
			panic(fmt.Sprintf("ipmtokens: palette.json unknown token type %q", r.Type))
		}
		if r.Mod != "" {
			if _, ok := modBitByName[r.Mod]; !ok {
				panic(fmt.Sprintf("ipmtokens: palette.json unknown modifier %q", r.Mod))
			}
		}
	}
	Palette = doc.Rules
}

func typeIndex(name string) int {
	for i, t := range SemanticTokenTypes {
		if t == name {
			return i
		}
	}
	return -1
}

// ClassSuffix returns the `ipm-<suffix>` class suffix for an LSP semantic
// token (type + modifier bitmask), or "" if the type is unknown. The
// modifier bits a token actually carries are mutually exclusive within a
// type, so the first matching modifier rule wins; the base rule is the
// fallback.
func ClassSuffix(tokenType string, mods uint32) string {
	base := ""
	for _, r := range Palette {
		if r.Type != tokenType {
			continue
		}
		if r.Mod == "" {
			base = r.Class
			continue
		}
		if mods&modBitByName[r.Mod] != 0 {
			return r.Class
		}
	}
	return base
}

// TokenForClass is the inverse of ClassSuffix: the (token-type index,
// modifier bits) that an `as-token:NAME` paints over its text. ok=false
// for an unknown name.
func TokenForClass(name string) (typ uint32, mods uint32, ok bool) {
	for _, r := range Palette {
		if r.Class != name {
			continue
		}
		var mb uint32
		if r.Mod != "" {
			mb = modBitByName[r.Mod]
		}
		return uint32(typeIndex(r.Type)), mb, true
	}
	return 0, 0, false
}

// PaletteCSSRules returns the generated `.ipm-<suffix> { ... }` rules (one
// per palette entry, in palette order) for embedding in HTML/preview CSS.
func PaletteCSSRules() string {
	width := 0
	for _, r := range Palette {
		if n := len(".ipm-" + r.Class); n > width {
			width = n
		}
	}
	var b strings.Builder
	for _, r := range Palette {
		sel := ".ipm-" + r.Class
		decls := []string{"color: " + r.FG + ";"}
		if r.Bold {
			decls = append(decls, "font-weight: bold;")
		}
		if r.Italic {
			decls = append(decls, "font-style: italic;")
		}
		fmt.Fprintf(&b, "%-*s { %s }\n", width, sel, strings.Join(decls, " "))
	}
	return b.String()
}
