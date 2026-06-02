package parser

import (
	"fmt"
	"strings"
)

// TokenKind classifies tokens produced by the scanner.
type TokenKind int

const (
	TokNode         TokenKind = iota // node segment text
	TokArrowFwd                      // -->
	TokArrowRev                      // <--
	TokArrowUndir                    // ---
	TokArrowExpl                     // --::X--> or --::N-- or <--::X--
	TokArrowTooltip                  // --"..."-->  or --"..."-- or --ident--> or --ident--
)

// Token is a single token from scanning a logical line.
type Token struct {
	Kind  TokenKind
	Start int    // offset within LogicalLine.Text (inclusive)
	End   int    // exclusive offset
	Text  string // raw token text for debugging / node segments

	// Arrow-specific:
	Tooltip string // unescaped tooltip text (TokArrowTooltip)
	ExpCode byte   // 'P','X','L','N' (TokArrowExpl)
	Reverse bool   // parsed from <-- form
	Undir   bool   // tooltip/explicit ending with -- (undirected)
}

// tokenize scans a logical line into tokens.
// Returns a mix of TokNode and arrow tokens.
func tokenize(line string) ([]Token, error) {
	var toks []Token
	nodeStart := -1 // start of current node text being accumulated

	// flushNode emits accumulated node text up to (but not including) pos.
	flushNode := func(pos int) {
		if nodeStart >= 0 && pos > nodeStart {
			toks = append(toks, Token{
				Kind:  TokNode,
				Start: nodeStart,
				End:   pos,
				Text:  line[nodeStart:pos],
			})
		}
		nodeStart = -1
	}

	p := 0
	for p < len(line) {
		c := line[p]

		// --- Handle quoted strings: skip content inside quotes ---
		if c == '"' {
			if nodeStart < 0 {
				nodeStart = p
			}
			// Find matching close quote
			j := p + 1
			for j < len(line) {
				if line[j] == '"' && !isEscaped(line, j) {
					break
				}
				j++
			}
			if j >= len(line) {
				// Unterminated quote: the opening `"` has no matching close on
				// this (already continuation-merged) logical line. Without this
				// error the whole rest of the line — arrows, type markers — would
				// be swallowed into a single node-text token.
				return nil, &scanError{msg: "unterminated quote: missing closing \"", start: p, end: len(line)}
			}
			p = j + 1 // past closing quote
			continue
		}

		// --- Try arrow patterns ---

		// 1. Bidirectional rejection: <-->
		if c == '<' && p+4 <= len(line) && line[p:p+4] == "<-->" {
			return nil, &scanError{msg: "invalid syntax: bidirectional arrow <--> is not supported", start: p, end: p + 4}
		}
		if c == ' ' && p+6 <= len(line) && line[p:p+6] == " <--> " {
			return nil, &scanError{msg: "invalid syntax: bidirectional arrow <--> is not supported", start: p, end: p + 6}
		}

		// 2. <-- prefix: reverse explicit, plain reverse
		if c == '<' && p+3 <= len(line) && line[p:p+3] == "<--" {
			// Try reverse explicit: <--::X--
			if tok, end, ok, err := tryReverseExplicit(line, p); err != nil {
				return nil, err
			} else if ok {
				flushNode(p)
				toks = append(toks, tok)
				p = end
				continue
			} else if hasExplicitCode(line, p+3) {
				// `<--::<code>` was written but the explicit reverse arrow did
				// not parse (e.g. `<--::N-->`, a reverse code with a forward
				// `-->` terminator). Surface a syntax error instead of silently
				// degrading to a plain `<--` plus leftover `::<code>...` node text.
				return nil, &scanError{msg: "invalid arrow: malformed reverse explicit arrow (a reverse code requires a '--' terminator)", start: p, end: p + 5}
			}
			// Plain reverse arrow: <--
			flushNode(p)
			toks = append(toks, Token{Kind: TokArrowRev, Start: p, End: p + 3, Reverse: true})
			p += 3
			continue
		}

		// 3. Spaced arrow patterns: " <-- ", " --> ", " --- ", " -- "
		if c == ' ' {
			// " <-- " — try spaced reverse explicit, then plain reverse
			if p+5 <= len(line) && line[p:p+5] == " <-- " {
				// Try spaced reverse explicit:  <-- ::X--
				if tok, end, ok, err := trySpacedReverseExplicit(line, p); err != nil {
					return nil, err
				} else if ok {
					flushNode(p)
					toks = append(toks, tok)
					p = end
					continue
				}
				// Plain spaced reverse
				flushNode(p)
				toks = append(toks, Token{Kind: TokArrowRev, Start: p, End: p + 5, Reverse: true})
				p += 5
				continue
			}
			// " --> "
			if p+5 <= len(line) && line[p:p+5] == " --> " {
				flushNode(p)
				toks = append(toks, Token{Kind: TokArrowFwd, Start: p, End: p + 5})
				p += 5
				continue
			}
			// " ---> " is not a valid arrow
			if p+7 <= len(line) && line[p:p+7] == " ---> " {
				return nil, &scanError{msg: "invalid arrow '--->'; did you mean '-->'?", start: p + 1, end: p + 5}
			}
			// " --- "
			if p+5 <= len(line) && line[p:p+5] == " --- " {
				flushNode(p)
				toks = append(toks, Token{Kind: TokArrowUndir, Start: p, End: p + 5})
				p += 5
				continue
			}
			// " -- " — try explicit, tooltip, or reject invalid undirected
			if p+4 <= len(line) && line[p:p+4] == " -- " {
				// Try spaced explicit:  --::X-->  or  --::N--
				if tok, end, ok, err := trySpacedExplicit(line, p); err != nil {
					return nil, err
				} else if ok {
					flushNode(p)
					toks = append(toks, tok)
					p = end
					continue
				}
				// Try spaced tooltip:  -- "..."-->  or  -- "..."--  or  -- ident-->  or  -- ident--
				if tok, end, ok := trySpacedTooltip(line, p); ok {
					flushNode(p)
					toks = append(toks, tok)
					p = end
					continue
				}
				// Bare " -- " is invalid undirected
				return nil, &scanError{msg: "invalid spaced two-dash undirected; use ' --- ' or '---'", start: p, end: p + 4}
			}
			// " --" followed by :: or " (explicit/tooltip with leading space)
			if p+3 < len(line) && line[p:p+3] == " --" {
				rest := line[p+3:]
				if len(rest) > 0 && (rest[0] == ':' || rest[0] == '"') {
					if tok, end, ok, err := tryCompactExplicit(line, p+1); err != nil {
						return nil, err
					} else if ok {
						flushNode(p)
						tok.Start = p // include leading space
						if end < len(line) && line[end] == ' ' {
							end++
							tok.End = end
						}
						toks = append(toks, tok)
						p = end
						continue
					}
					if tok, end, ok := tryCompactTooltip(line, p+1); ok {
						flushNode(p)
						tok.Start = p // include leading space
						if end < len(line) && line[end] == ' ' {
							end++
							tok.End = end
						}
						toks = append(toks, tok)
						p = end
						continue
					}
					// --" with no closing quote is an error
					if rest[0] == '"' {
						return nil, &scanError{msg: "unterminated tooltip: missing closing quote after --\"", start: p + 1, end: p + 4}
					}
				}
				// " --" followed by identifier char (unquoted tooltip)
				if len(rest) > 0 && isIdentStart(rest[0]) {
					if tok, end, ok := tryCompactUnquotedTooltip(line, p+1); ok {
						flushNode(p)
						tok.Start = p
						if end < len(line) && line[end] == ' ' {
							end++
							tok.End = end
						}
						toks = append(toks, tok)
						p = end
						continue
					}
				}
			}
		}

		// 4. --- compact undirected
		if c == '-' && p+3 <= len(line) && line[p:p+3] == "---" {
			// ---> is not a valid arrow; reject it explicitly
			if p+4 <= len(line) && line[p+3] == '>' {
				return nil, &scanError{msg: "invalid arrow '--->'; did you mean '-->'?", start: p, end: p + 4}
			} else {
				flushNode(p)
				toks = append(toks, Token{Kind: TokArrowUndir, Start: p, End: p + 3})
				p += 3
				continue
			}
		}

		// 5. -- prefix: try compact explicit, compact tooltip, compact forward -->
		if c == '-' && p+2 <= len(line) && line[p:p+2] == "--" {
			// --> compact forward
			if p+3 <= len(line) && line[p:p+3] == "-->" {
				flushNode(p)
				toks = append(toks, Token{Kind: TokArrowFwd, Start: p, End: p + 3})
				p += 3
				continue
			}
			// --:: compact explicit
			if tok, end, ok, err := tryCompactExplicit(line, p); err != nil {
				return nil, err
			} else if ok {
				flushNode(p)
				toks = append(toks, tok)
				p = end
				continue
			}
			// --"..." compact tooltip
			if tok, end, ok := tryCompactTooltip(line, p); ok {
				flushNode(p)
				toks = append(toks, tok)
				p = end
				continue
			}
			// --" with no closing quote is an error
			if p+2 < len(line) && line[p+2] == '"' {
				return nil, &scanError{msg: "unterminated tooltip: missing closing quote after --\"", start: p, end: p + 3}
			}
			// --identifier compact unquoted tooltip
			if p+2 < len(line) && isIdentStart(line[p+2]) {
				if tok, end, ok := tryCompactUnquotedTooltip(line, p); ok {
					flushNode(p)
					toks = append(toks, tok)
					p = end
					continue
				}
				// If identifier follows but doesn't form a valid tooltip, it's invalid undirected
				return nil, &scanError{msg: "invalid compact undirected; use '---'", start: p, end: p + 2}
			}
			// Bare compact "--" not part of anything recognized
			if p+2 < len(line) && line[p+2] == ' ' {
				// "-- " is OK — just text (e.g., em-dash usage)
				if nodeStart < 0 {
					nodeStart = p
				}
				p++
				continue
			}
			// Two isolated dashes followed by a non-space, non-dash, non-quote, non-colon char
			// is invalid undirected (should be ---)
			if p+2 < len(line) && line[p+2] != '-' && line[p+2] != '"' && line[p+2] != ':' && line[p+2] != '>' {
				return nil, &scanError{msg: "invalid compact undirected; use '---'", start: p, end: p + 2}
			}
		}

		// Default: accumulate node text
		if nodeStart < 0 {
			nodeStart = p
		}
		p++
	}

	// Flush remaining node text
	flushNode(len(line))

	return toks, nil
}

// scanError is an internal error from tokenization with positions relative to the logical line.
type scanError struct {
	msg   string
	start int
	end   int
}

func (e *scanError) Error() string { return e.msg }

// isEscaped checks if the byte at position j in s is preceded by an odd number of backslashes.
func isEscaped(s string, j int) bool {
	n := 0
	for k := j - 1; k >= 0 && s[k] == '\\'; k-- {
		n++
	}
	return n%2 == 1
}

// isIdentStart returns true if c can start an identifier.
func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// hasExplicitCode reports whether line at position q begins with "::" followed
// by a valid explicit edge code letter (P/X/L/N) — i.e. an explicit arrow was
// clearly intended.
func hasExplicitCode(line string, q int) bool {
	if q+3 > len(line) || line[q] != ':' || line[q+1] != ':' {
		return false
	}
	switch line[q+2] {
	case 'P', 'X', 'L', 'N':
		return true
	}
	return false
}

// isIdentChar returns true if c can continue an identifier (letters, digits, hyphens).
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-'
}

// findCloseQuote finds the index of the closing unescaped quote starting search from pos.
// Returns -1 if not found.
func findCloseQuote(s string, pos int) int {
	for j := pos; j < len(s); j++ {
		if s[j] == '"' && !isEscaped(s, j) {
			return j
		}
	}
	return -1
}

// parseExplicitTail parses the optional tooltip and arrow terminator after the explicit code letter.
// pos is the position right after the code letter in line.
// code is the explicit code ('P','X','L','N').
// reverse indicates if this is a reverse arrow form (<--::X--).
// Returns tooltip, end position, ok, err. err is non-nil only when a tooltip
// quote was opened but never closed: an explicit arrow was clearly intended, so
// the caller must surface the error rather than silently degrade to node text.
func parseExplicitTail(line string, pos int, code byte, reverse bool) (string, int, bool, error) {
	q := pos

	// skip optional spaces
	for q < len(line) && line[q] == ' ' {
		q++
	}

	var tip string

	// try quoted tooltip
	if q < len(line) && line[q] == '"' {
		j := findCloseQuote(line, q+1)
		if j < 0 {
			return "", 0, false, &scanError{msg: "unterminated tooltip: missing closing quote in explicit arrow", start: q, end: len(line)}
		}
		tipRaw := line[q+1 : j]
		unesc, err := unescapeQuoted(tipRaw)
		if err != nil {
			return "", 0, false, nil
		}
		tip = unesc
		q = j + 1
	} else if q < len(line) && isIdentStart(line[q]) {
		// try unquoted identifier tooltip
		j := q
		for j < len(line) && isIdentChar(line[j]) {
			if j+3 <= len(line) && line[j:j+3] == "-->" {
				break
			}
			if j+2 <= len(line) && line[j:j+2] == "--" {
				break
			}
			j++
		}
		if j > q {
			tip = line[q:j]
			q = j
		}
	}

	// skip optional spaces after tooltip
	for q < len(line) && line[q] == ' ' {
		q++
	}

	// Determine expected terminator
	useUndirTerminator := (code == 'N') || reverse

	if useUndirTerminator {
		// expect --
		if q+2 <= len(line) && line[q:q+2] == "--" {
			end := q + 2
			if end < len(line) && line[end] == '>' {
				if code == 'N' && !reverse {
					// --::N--> tolerated as --::N--
					end++
				} else {
					// reverse form: -- followed by > is invalid
					return "", 0, false, nil
				}
			}
			return tip, end, true, nil
		}
		return "", 0, false, nil
	}

	// directed forward: expect -->
	if q+3 <= len(line) && line[q:q+3] == "-->" {
		return tip, q + 3, true, nil
	}

	return "", 0, false, nil
}

// tryReverseExplicit tries to match <--::X-- at position p, optionally with tooltip.
// A non-nil error means an explicit arrow was clearly intended but malformed
// (e.g. an unterminated tooltip quote) and must not silently degrade to text.
func tryReverseExplicit(line string, p int) (Token, int, bool, error) {
	// <--::X-- : p points at '<'
	q := p + 3 // past "<--"
	if q+2 > len(line) || line[q:q+2] != "::" {
		return Token{}, 0, false, nil
	}
	if q+3 > len(line) {
		return Token{}, 0, false, nil
	}
	code := line[q+2]
	if code != 'P' && code != 'X' && code != 'L' && code != 'N' {
		return Token{}, 0, false, nil
	}
	tip, end, ok, err := parseExplicitTail(line, q+3, code, true)
	if !ok {
		return Token{}, 0, false, err
	}
	return Token{
		Kind:    TokArrowExpl,
		Start:   p,
		End:     end,
		ExpCode: code,
		Tooltip: tip,
		Reverse: true,
		Undir:   code == 'N',
	}, end, true, nil
}

// trySpacedReverseExplicit tries to match " <-- ::X-- " starting at p (which is at the space).
// Optionally supports tooltip: " <-- ::X "tip"-- " or " <-- ::X ident-- ".
func trySpacedReverseExplicit(line string, p int) (Token, int, bool, error) {
	// " <-- " starts at p, so "::" should appear after some optional spaces
	q := p + 5 // past " <-- "
	for q < len(line) && line[q] == ' ' {
		q++
	}
	if q+2 > len(line) || line[q:q+2] != "::" {
		return Token{}, 0, false, nil
	}
	if q+3 > len(line) {
		return Token{}, 0, false, nil
	}
	code := line[q+2]
	if code != 'P' && code != 'X' && code != 'L' && code != 'N' {
		return Token{}, 0, false, nil
	}
	tip, end, ok, err := parseExplicitTail(line, q+3, code, true)
	if !ok {
		return Token{}, 0, false, err
	}
	if end < len(line) && line[end] == ' ' {
		end++
	}
	return Token{
		Kind:    TokArrowExpl,
		Start:   p,
		End:     end,
		ExpCode: code,
		Tooltip: tip,
		Reverse: true,
		Undir:   code == 'N',
	}, end, true, nil
}

// trySpacedExplicit tries to match " --::X--> " or " --::N-- " starting at p.
// Optionally supports tooltip: " --::X "tip"--> " or " --::X ident--> ".
// p points at the space before "--".
func trySpacedExplicit(line string, p int) (Token, int, bool, error) {
	// " -- " is at p..p+4, so "::" should be right after
	q := p + 4 // past " -- "
	for q < len(line) && line[q] == ' ' {
		q++
	}
	if q+2 > len(line) || line[q:q+2] != "::" {
		return Token{}, 0, false, nil
	}
	if q+3 > len(line) {
		return Token{}, 0, false, nil
	}
	code := line[q+2]
	if code != 'P' && code != 'X' && code != 'L' && code != 'N' {
		return Token{}, 0, false, nil
	}
	tip, end, ok, err := parseExplicitTail(line, q+3, code, false)
	if !ok {
		return Token{}, 0, false, err
	}
	if end < len(line) && line[end] == ' ' {
		end++
	}
	return Token{
		Kind:    TokArrowExpl,
		Start:   p,
		End:     end,
		ExpCode: code,
		Tooltip: tip,
		Undir:   code == 'N',
	}, end, true, nil
}

// tryCompactExplicit tries to match --::X--> or --::N-- at position p.
// Optionally supports tooltip: --::X "tip"--> or --::X ident-->.
func tryCompactExplicit(line string, p int) (Token, int, bool, error) {
	// p at first '-'
	if p+4 > len(line) || line[p:p+4] != "--::" {
		return Token{}, 0, false, nil
	}
	if p+5 > len(line) {
		return Token{}, 0, false, nil
	}
	code := line[p+4]
	if code != 'P' && code != 'X' && code != 'L' && code != 'N' {
		return Token{}, 0, false, nil
	}
	tip, end, ok, err := parseExplicitTail(line, p+5, code, false)
	if !ok {
		return Token{}, 0, false, err
	}
	return Token{
		Kind:    TokArrowExpl,
		Start:   p,
		End:     end,
		ExpCode: code,
		Tooltip: tip,
		Undir:   code == 'N',
	}, end, true, nil
}

// trySpacedTooltip tries to match " -- "..."-- " or " -- "..."--> " starting at p.
func trySpacedTooltip(line string, p int) (Token, int, bool) {
	q := p + 4 // past " -- "
	for q < len(line) && line[q] == ' ' {
		q++
	}
	// Try quoted tooltip
	if q < len(line) && line[q] == '"' {
		tok, end, ok := tryTooltipFromQuote(line, p, q)
		if ok {
			if end < len(line) && line[end] == ' ' {
				end++
				tok.End = end
			}
		}
		return tok, end, ok
	}
	// Try unquoted identifier tooltip
	if q < len(line) && isIdentStart(line[q]) {
		tok, end, ok := tryUnquotedTooltipFrom(line, p, q)
		if ok {
			if end < len(line) && line[end] == ' ' {
				end++
				tok.End = end
			}
		}
		return tok, end, ok
	}
	return Token{}, 0, false
}

// tryCompactTooltip tries to match --"..."-- or --"..."--> at position p.
func tryCompactTooltip(line string, p int) (Token, int, bool) {
	if p+3 > len(line) || line[p:p+2] != "--" || line[p+2] != '"' {
		return Token{}, 0, false
	}
	return tryTooltipFromQuote(line, p, p+2)
}

// tryCompactUnquotedTooltip tries to match --identifier--> or --identifier-- at position p.
func tryCompactUnquotedTooltip(line string, p int) (Token, int, bool) {
	if p+2 >= len(line) || line[p:p+2] != "--" {
		return Token{}, 0, false
	}
	q := p + 2
	if !isIdentStart(line[q]) {
		return Token{}, 0, false
	}
	return tryUnquotedTooltipFrom(line, p, q)
}

// tryTooltipFromQuote tries to parse a quoted tooltip starting at quote position q.
// arrowStart is where the arrow token should start (for span tracking).
func tryTooltipFromQuote(line string, arrowStart, q int) (Token, int, bool) {
	j := findCloseQuote(line, q+1)
	if j < 0 {
		return Token{}, 0, false
	}
	tipRaw := line[q+1 : j]
	tip, err := unescapeQuoted(tipRaw)
	if err != nil {
		return Token{}, 0, false
	}

	k := j + 1
	for k < len(line) && line[k] == ' ' {
		k++
	}

	// Check for --> ending (directed)
	if k+3 <= len(line) && line[k:k+3] == "-->" {
		end := k + 3
		return Token{
			Kind:    TokArrowTooltip,
			Start:   arrowStart,
			End:     end,
			Tooltip: tip,
		}, end, true
	}
	// Check for -- ending (undirected, not -->)
	if k+2 <= len(line) && line[k:k+2] == "--" && (k+3 > len(line) || line[k+2] != '>') {
		end := k + 2
		return Token{
			Kind:    TokArrowTooltip,
			Start:   arrowStart,
			End:     end,
			Tooltip: tip,
			Undir:   true,
		}, end, true
	}
	return Token{}, 0, false
}

// tryUnquotedTooltipFrom tries to parse an unquoted identifier tooltip from position q.
func tryUnquotedTooltipFrom(line string, arrowStart, q int) (Token, int, bool) {
	j := q
	for j < len(line) && isIdentChar(line[j]) {
		// Stop before "-->" or "--" sequences
		if j+3 <= len(line) && line[j:j+3] == "-->" {
			break
		}
		if j+2 <= len(line) && line[j:j+2] == "--" {
			break
		}
		j++
	}
	if j == q {
		return Token{}, 0, false
	}
	tip := line[q:j]

	k := j
	for k < len(line) && line[k] == ' ' {
		k++
	}

	// Check for --> ending (directed)
	if k+3 <= len(line) && line[k:k+3] == "-->" {
		end := k + 3
		return Token{
			Kind:    TokArrowTooltip,
			Start:   arrowStart,
			End:     end,
			Tooltip: tip,
		}, end, true
	}
	// Check for -- ending (undirected, not -->)
	if k+2 <= len(line) && line[k:k+2] == "--" && (k+3 > len(line) || line[k+2] != '>') {
		end := k + 2
		return Token{
			Kind:    TokArrowTooltip,
			Start:   arrowStart,
			End:     end,
			Tooltip: tip,
			Undir:   true,
		}, end, true
	}
	return Token{}, 0, false
}

// arrowTokens filters a token list to only arrow tokens.
func arrowTokens(toks []Token) []Token {
	var arrows []Token
	for _, t := range toks {
		if t.Kind != TokNode {
			arrows = append(arrows, t)
		}
	}
	return arrows
}

// validateTokens performs post-tokenization validation.
func validateTokens(line string, toks []Token) error {
	arrows := arrowTokens(toks)

	// Check for commas in middle segments when there are multiple arrows
	if len(arrows) > 1 && strings.Contains(line, ",") {
		// Build segments between arrows
		type seg struct{ start, end int }
		var segs []seg
		last := 0
		for _, a := range arrows {
			segs = append(segs, seg{last, a.Start})
			last = a.End
		}
		segs = append(segs, seg{last, len(line)})

		// Middle segments (not first, not last) must not contain commas outside quotes
		for i := 1; i < len(segs)-1; i++ {
			text := line[segs[i].start:segs[i].end]
			if containsCommaOutsideQuotes(text) {
				return fmt.Errorf("invalid syntax: ambiguous comma with multiple arrows in one line: %q", line)
			}
		}
	}

	return nil
}
