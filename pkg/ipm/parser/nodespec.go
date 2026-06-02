package parser

import (
	"fmt"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
)

// NodeSpec is the result of parsing a single node annotation.
type NodeSpec struct {
	Name  string
	Alias string
	Type  model.NodeType
	// Candidates lists the possible kinds of an Unresolved node (from a ::?et /
	// ::?etc marker), primary first. Non-empty iff Type == Unresolved.
	Candidates []model.NodeType
	Tooltip    string
	// Span is [start, end) within the logical line where the name was found.
	NameStart int
	NameEnd   int

	// Lexeme spans within the logical line ([start,end), {-1,-1} if absent).
	// Filled by parseNodeSpecs from the pt-relative spans parseOneNodeSpec
	// records; consumed by the parser to populate model.Src.Lex.
	KindMarker [2]int // ::e / ::t / ::c
	AliasMark  [2]int // ::a
	TipMark    [2]int // ::tip
	AliasName  [2]int // the alias identifier (as written)
	TipString  [2]int // the quoted tooltip string (incl. quotes)
	NameQuoted bool   // the title (Name span) is a quoted string

	// For ::?<letters> markers, the `::?` prefix span (rendered as a generic
	// type-marker) plus a per-letter list so each candidate letter colors as
	// its own kind (e=event, t=thing, c=concept). Empty when Type !=
	// Unresolved; KindMarker is noSpan in that case.
	UndecidedPrefix  [2]int
	UndecidedLetters []LetterKindSpan
}

// LetterKindSpan is one candidate-kind letter inside a `::?<letters>` marker —
// the letter's position and the NodeType it represents.
type LetterKindSpan struct {
	Span [2]int
	Kind model.NodeType
}

var noSpan = [2]int{-1, -1}

// splitByCommaOutsideQuotes splits text by commas that are not inside double-quoted strings.
func splitByCommaOutsideQuotes(text string) []string {
	var parts []string
	inQuote := false
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '"' && !isEscaped(text, i) {
			inQuote = !inQuote
			continue
		}
		if !inQuote && text[i] == ',' {
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	parts = append(parts, text[start:])
	return parts
}

// parseNodeSpecs splits segment text by commas (outside quotes) and parses each part.
// segStart/segEnd are absolute positions in the logical line.
func parseNodeSpecs(text string, segStart int) ([]NodeSpec, error) {
	parts := splitByCommaOutsideQuotes(text)
	var specs []NodeSpec
	cursor := 0
	for _, part := range parts {
		pt := strings.TrimSpace(part)
		if pt == "" {
			cursor += len(part) + 1 // +1 for comma
			continue
		}
		spec, err := parseOneNodeSpec(pt)
		if err != nil {
			return nil, err
		}
		// Map pt-relative lexeme spans to logical-line coordinates. pt begins
		// at `cursor + leading-whitespace(part)` within text; segStart shifts
		// text → logical line.
		ptBase := segStart + cursor + (len(part) - len(strings.TrimLeft(part, " \t")))
		toLogical := func(s [2]int) [2]int {
			if s[0] < 0 {
				return noSpan
			}
			return [2]int{ptBase + s[0], ptBase + s[1]}
		}

		// Name position: prefer the by-exclusion ptName computed in
		// parseOneNodeSpec (skips substring matches inside alias/marker
		// regions). Fall back to a global search for the rare case the
		// exclusion didn't find a match.
		if spec.ptName[0] >= 0 {
			spec.NameStart = ptBase + spec.ptName[0]
			spec.NameEnd = ptBase + spec.ptName[1]
		} else {
			rel := strings.Index(text[cursor:], spec.origName)
			if rel < 0 {
				rel = strings.Index(text[cursor:], pt)
				if rel < 0 {
					rel = 0
				}
			}
			spec.NameStart = segStart + cursor + rel
			spec.NameEnd = spec.NameStart + len(spec.origName)
		}

		spec.NodeSpec.KindMarker = toLogical(spec.ptKindMarker)
		spec.NodeSpec.AliasMark = toLogical(spec.ptAliasMark)
		spec.NodeSpec.TipMark = toLogical(spec.ptTipMark)
		spec.NodeSpec.AliasName = toLogical(spec.ptAliasName)
		spec.NodeSpec.TipString = toLogical(spec.ptTipString)
		spec.NodeSpec.UndecidedPrefix = toLogical(spec.ptUndecidedPrefix)
		if len(spec.ptUndecidedLetters) > 0 {
			spec.NodeSpec.UndecidedLetters = make([]LetterKindSpan, len(spec.ptUndecidedLetters))
			for i, l := range spec.ptUndecidedLetters {
				spec.NodeSpec.UndecidedLetters[i] = LetterKindSpan{
					Span: toLogical(l.Span),
					Kind: l.Kind,
				}
			}
		}

		specs = append(specs, spec.NodeSpec)
		cursor += len(part) + 1
	}
	return specs, nil
}

// parsedNode is the internal result including origName for position tracking.
type parsedNode struct {
	NodeSpec
	origName string // name before unquoting, for position
	// pt-relative lexeme spans ({-1,-1} if absent); parseNodeSpecs maps
	// these to logical-line coordinates.
	ptKindMarker       [2]int
	ptAliasMark        [2]int
	ptTipMark          [2]int
	ptAliasName        [2]int
	ptTipString        [2]int
	ptName             [2]int // origName's position inside pt (skipping substring matches inside removed spans)
	ptUndecidedPrefix  [2]int
	ptUndecidedLetters []LetterKindSpan // pt-relative
}

// parseOneNodeSpec parses a single node part like "alias::a Name ::e \"tip\"::tip".
// Canonical order: [alias::a] name [::type] ["tooltip"::tip]
func parseOneNodeSpec(pt string) (parsedNode, error) {
	var result parsedNode
	result.Type = model.Thing
	rest := pt

	// 1. Try to extract alias::a at the beginning (outside any quotes)
	if ai := findAnnotation(rest, "::a"); ai >= 0 {
		before := strings.TrimSpace(rest[:ai])
		after := strings.TrimSpace(rest[ai+3:])

		// Alias must be before name: alias::a name [::type] ["tip"::tip]
		if before != "" && !strings.ContainsAny(before, " \t") {
			// alias::a form — alias is `before`, rest is `after`
			result.Alias = before
			rest = after
		}
	}

	// 2. Extract tooltip: find "..."::tip (outside quotes, using findAnnotation for ::tip)
	if tipIdx := findAnnotation(rest, "::tip"); tipIdx >= 0 {
		// Find the quoted string directly before ::tip
		beforeTip := strings.TrimRight(rest[:tipIdx], " \t")

		// The quoted string should end right before ::tip
		if len(beforeTip) > 0 && beforeTip[len(beforeTip)-1] == '"' {
			// Search backward for the opening quote
			openQ := -1
			for k := len(beforeTip) - 2; k >= 0; k-- {
				if beforeTip[k] == '"' && !isEscaped(beforeTip, k) {
					openQ = k
					break
				}
			}
			if openQ >= 0 {
				tipRaw := beforeTip[openQ+1 : len(beforeTip)-1]
				tip, err := unescapeQuoted(tipRaw)
				if err != nil {
					return parsedNode{}, err
				}
				result.Tooltip = tip
				// Remove tooltip + ::tip from rest
				afterTip := rest[tipIdx+5:]
				rest = strings.TrimSpace(beforeTip[:openQ]) + afterTip
			}
		}
	}

	// 3a. Undecided marker: ::?<letters>, where <letters> is 2–3 of e/t/c (no
	// repeats), first = primary. Detected before the single-letter scan.
	if qi := findUndecidedMarker(rest); qi >= 0 {
		end := qi + 3 // past "::?"
		var letters []byte
		for end < len(rest) && (rest[end] == 'e' || rest[end] == 't' || rest[end] == 'c') {
			letters = append(letters, rest[end])
			end++
		}
		cands, err := candidatesFromLetters(letters)
		if err != nil {
			return parsedNode{}, err
		}
		if end < len(rest) {
			if n := rest[end]; n != ' ' && n != '\t' && n != '"' && n != ':' && n != ',' {
				return parsedNode{}, fmt.Errorf("invalid undecided node type marker; use ::?et, ::?ec, ::?tc, or ::?etc")
			}
		}
		result.Type = model.Unresolved
		result.Candidates = cands
		rest = strings.TrimSpace(rest[:qi] + rest[end:])
	}

	// 3. Extract type marker: ::e, ::c, ::t (only outside quotes). Skipped when an
	// undecided marker already set the kind. A node may carry AT MOST ONE type
	// marker — conflicting or duplicate markers (e.g. ::e::c, ::t::t) are a syntax
	// error, not a silent last-wins (matches the ipmt spec's invalid examples).
	if result.Type != model.Unresolved {
		typeMarkers := []struct {
			tag string
			typ model.NodeType
		}{
			{"::e", model.Event},
			{"::c", model.Concept},
			{"::t", model.Thing},
		}
		count := 0
		for _, marker := range typeMarkers {
			for s := rest; ; {
				idx := findAnnotation(s, marker.tag)
				if idx < 0 {
					break
				}
				count++
				s = s[idx+len(marker.tag):]
			}
		}
		if count > 1 {
			return parsedNode{}, fmt.Errorf("invalid syntax: a node may have at most one type marker (::e, ::c, or ::t)")
		}
		for _, marker := range typeMarkers {
			if idx := findAnnotation(rest, marker.tag); idx >= 0 {
				result.Type = marker.typ
				// Remove from rest
				end := idx + len(marker.tag)
				rest = strings.TrimSpace(rest[:idx] + rest[end:])
			}
		}
	}

	// Reject uppercase type markers
	for _, upper := range []string{"::E", "::C", "::T"} {
		if findAnnotation(rest, upper) >= 0 {
			return parsedNode{}, fmt.Errorf("invalid syntax: uppercase node type markers are not allowed; use ::e, ::c, or ::t")
		}
	}

	// 4. If alias wasn't found at the beginning, try at the end of rest.
	// Old format: "name ::type alias::a" → after type extraction rest = "name alias::a"
	if result.Alias == "" {
		if ai := findAnnotation(rest, "::a"); ai >= 0 {
			before := strings.TrimSpace(rest[:ai])
			after := strings.TrimSpace(rest[ai+3:])
			// alias is the last word before ::a (no spaces in alias)
			if after == "" && before != "" {
				lastSpace := strings.LastIndexAny(before, " \t")
				if lastSpace >= 0 {
					aliasCandidate := before[lastSpace+1:]
					if aliasCandidate != "" && !strings.ContainsAny(aliasCandidate, " \t") {
						result.Alias = aliasCandidate
						rest = strings.TrimSpace(before[:lastSpace])
					}
				} else {
					// before is a single word — it's the alias AND name
					result.Alias = before
					rest = ""
				}
			}
		}
	}

	// After extracting alias, tooltip, and type, the remaining text is the name
	name := strings.TrimSpace(rest)

	// Strip bracket labels: "e1 [long label text]" → "e1"
	if bi := strings.Index(name, "["); bi >= 0 {
		if ci := strings.LastIndex(name, "]"); ci > bi {
			name = strings.TrimSpace(name[:bi])
		}
	}

	// Strip quotes from alias
	if result.Alias != "" {
		unquotedAlias, err := stripQuotes(result.Alias)
		if err != nil {
			return parsedNode{}, err
		}
		result.Alias = unquotedAlias
	}

	// Keep original name for position tracking
	result.origName = name

	// Strip quotes from name
	if name != "" {
		unquotedName, err := stripQuotes(name)
		if err != nil {
			return parsedNode{}, err
		}
		result.Name = unquotedName
	}

	// A spec with NEITHER a name nor an alias is rejected. Without this, `""`,
	// `[label-only]` and a bare `::e` each produce a node with no identity at
	// all — and, because lookup keys on the name, every such spec in the
	// document resolves to the SAME node, silently collecting one another's
	// edges instead of failing as a syntax error. (An alias-only declaration
	// like `alpha::a ::?etc` is a legitimate shorthand: the alias is the
	// node's identity.)
	if result.Name == "" && result.Alias == "" {
		return parsedNode{}, fmt.Errorf("invalid syntax: node has neither a name nor an alias (an empty quoted string, a bracket-only label, or annotations with no name)")
	}

	// Lexeme spans, computed on the ORIGINAL pt (never mutated — only the
	// `rest` copy was). findAnnotation is quote-aware, so a `::e` inside a
	// quoted title or tooltip is not mistaken for a marker.
	result.ptKindMarker = noSpan
	result.ptAliasMark = noSpan
	result.ptTipMark = noSpan
	result.ptAliasName = noSpan
	result.ptTipString = noSpan

	// Kind marker: only present for an explicit ::e/::t/::c (a default Thing
	// has none, so findAnnotation returns -1 and we leave it absent).
	kindTag := map[model.NodeType]string{
		model.Event:   "::e",
		model.Concept: "::c",
		model.Thing:   "::t",
	}[result.Type]
	if kindTag != "" {
		if off := findAnnotation(pt, kindTag); off >= 0 {
			result.ptKindMarker = [2]int{off, off + len(kindTag)}
		}
	}
	// Undecided marker (::?<letters>): record the `::?` prefix and each
	// candidate letter separately, so the prefix renders as a type-marker
	// and each letter colors as its own kind. ptKindMarker stays noSpan
	// — Unresolved has no single ::e/::t/::c marker.
	result.ptUndecidedPrefix = noSpan
	result.ptUndecidedLetters = nil
	if result.Type == model.Unresolved {
		if off := findUndecidedMarker(pt); off >= 0 {
			result.ptUndecidedPrefix = [2]int{off, off + 3} // the `::?`
			for i := off + 3; i < len(pt); i++ {
				var kind model.NodeType
				switch pt[i] {
				case 'e':
					kind = model.Event
				case 't':
					kind = model.Thing
				case 'c':
					kind = model.Concept
				default:
					i = len(pt) // bail
					continue
				}
				result.ptUndecidedLetters = append(result.ptUndecidedLetters,
					LetterKindSpan{Span: [2]int{i, i + 1}, Kind: kind})
			}
		}
	}

	// ::a marker + alias-name identifier (the last whitespace-delimited
	// token before ::a — covers both `alias::a name` and `name ::e alias::a`).
	if result.Alias != "" {
		if off := findAnnotation(pt, "::a"); off >= 0 {
			result.ptAliasMark = [2]int{off, off + 3}
			s := strings.TrimRight(pt[:off], " \t")
			start := strings.LastIndexAny(s, " \t") + 1
			if start < len(s) {
				result.ptAliasName = [2]int{start, len(s)}
			}
		}
	}

	// ::tip marker + the quoted tooltip string right before it.
	if result.Tooltip != "" {
		if off := findAnnotation(pt, "::tip"); off >= 0 {
			result.ptTipMark = [2]int{off, off + 5}
			s := strings.TrimRight(pt[:off], " \t")
			if len(s) > 0 && s[len(s)-1] == '"' {
				openQ := -1
				for k := len(s) - 2; k >= 0; k-- {
					if s[k] == '"' && !isEscaped(s, k) {
						openQ = k
						break
					}
				}
				if openQ >= 0 {
					result.ptTipString = [2]int{openQ, len(s)}
				}
			}
		}
	}

	// Quoted title → also a string token (preserves the old stringRE color).
	result.NameQuoted = len(result.origName) >= 2 &&
		result.origName[0] == '"' && result.origName[len(result.origName)-1] == '"'

	// Locate origName in pt by exclusion: find the first occurrence whose
	// position is NOT inside any removed lexeme span. Without this, a name
	// that's a substring of an earlier alias/marker would mis-position
	// (e.g. `abcd::a bcd ::e` — "bcd" appears at offset 1 inside "abcd"
	// before the real one at offset 8).
	result.ptName = noSpan
	if result.origName != "" {
		removed := [5][2]int{
			result.ptAliasName, result.ptAliasMark,
			result.ptKindMarker, result.ptTipString, result.ptTipMark,
		}
		inside := func(pos int) bool {
			for _, r := range removed {
				if r[0] < 0 {
					continue
				}
				if pos >= r[0] && pos < r[1] {
					return true
				}
			}
			return false
		}
		search := 0
		for search < len(pt) {
			idx := strings.Index(pt[search:], result.origName)
			if idx < 0 {
				break
			}
			pos := search + idx
			if !inside(pos) {
				result.ptName = [2]int{pos, pos + len(result.origName)}
				break
			}
			search = pos + 1
		}
	}

	return result, nil
}

// findUndecidedMarker returns the index of a "::?" marker outside quotes, or -1.
// (findAnnotation can't be used: its boundary check rejects the kind letters
// that follow "::?".)
func findUndecidedMarker(text string) int {
	inQuote := false
	for i := 0; i+2 < len(text); i++ {
		if text[i] == '"' && !isEscaped(text, i) {
			inQuote = !inQuote
			continue
		}
		if !inQuote && text[i] == ':' && text[i+1] == ':' && text[i+2] == '?' {
			return i
		}
	}
	return -1
}

// candidatesFromLetters maps a run of kind letters (2–3 of e/t/c, no repeats) to
// ordered NodeTypes (primary first). Rejects bare ::? (0 letters), a single
// letter, >3, and repeats.
func candidatesFromLetters(letters []byte) ([]model.NodeType, error) {
	if len(letters) < 2 || len(letters) > 3 {
		return nil, fmt.Errorf("undecided node type marker needs 2-3 of e/t/c (e.g. ::?et, ::?etc)")
	}
	seen := map[byte]bool{}
	out := make([]model.NodeType, 0, len(letters))
	for _, b := range letters {
		if seen[b] {
			return nil, fmt.Errorf("undecided node type marker has a repeated kind letter")
		}
		seen[b] = true
		switch b {
		case 'e':
			out = append(out, model.Event)
		case 't':
			out = append(out, model.Thing)
		case 'c':
			out = append(out, model.Concept)
		}
	}
	return out, nil
}

// findAnnotation finds the position of a ::tag annotation in text,
// making sure it's not inside a quoted string.
func findAnnotation(text, tag string) int {
	inQuote := false
	for i := 0; i < len(text); i++ {
		if text[i] == '"' && !isEscaped(text, i) {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if i+len(tag) <= len(text) && text[i:i+len(tag)] == tag {
			// Make sure the tag is followed by a word boundary
			end := i + len(tag)
			if end < len(text) {
				next := text[end]
				// ::a must be followed by space or end
				// ::e, ::c, ::t must be followed by space, quote, ::, or end
				if next != ' ' && next != '\t' && next != '"' && next != ':' && next != ',' {
					continue
				}
			}
			return i
		}
	}
	return -1
}
