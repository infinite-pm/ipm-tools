package tagexpr

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Eval evaluates a boolean tag expression against a set of test tags.
// Returns true if the expression matches the tag set, false otherwise.
// Expression syntax: TAG, !TAG, TAG1 && TAG2, TAG1 || TAG2, (expr)
// Precedence: ! > && > ||
func Eval(expr string, testTags []string) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false, fmt.Errorf("empty tag expression")
	}

	parser := &tagParser{
		expr:     expr,
		pos:      0,
		testTags: testTags,
	}

	result, err := parser.parseOr()
	if err != nil {
		return false, err
	}

	// Ensure we consumed the entire expression
	parser.skipWhitespace()
	if parser.pos < len(parser.expr) {
		return false, fmt.Errorf("unexpected characters at position %d: %s", parser.pos, parser.expr[parser.pos:])
	}

	return result, nil
}

type tagParser struct {
	expr     string
	pos      int
	testTags []string
}

// parseOr handles || operator (lowest precedence)
func (p *tagParser) parseOr() (bool, error) {
	left, err := p.parseAnd()
	if err != nil {
		return false, err
	}

	for {
		p.skipWhitespace()
		if !p.consumeOperator("||") {
			break
		}
		p.skipWhitespace()

		right, err := p.parseAnd()
		if err != nil {
			return false, err
		}

		left = left || right
	}

	return left, nil
}

// parseAnd handles && operator (middle precedence)
func (p *tagParser) parseAnd() (bool, error) {
	left, err := p.parseNot()
	if err != nil {
		return false, err
	}

	for {
		p.skipWhitespace()
		if !p.consumeOperator("&&") {
			break
		}
		p.skipWhitespace()

		right, err := p.parseNot()
		if err != nil {
			return false, err
		}

		left = left && right
	}

	return left, nil
}

// parseNot handles ! operator (highest precedence)
func (p *tagParser) parseNot() (bool, error) {
	p.skipWhitespace()

	// Check for negation
	if p.peek() == '!' {
		p.pos++
		p.skipWhitespace()

		value, err := p.parsePrimary()
		if err != nil {
			return false, err
		}

		return !value, nil
	}

	return p.parsePrimary()
}

// parsePrimary handles TAG identifiers and parenthesized expressions
func (p *tagParser) parsePrimary() (bool, error) {
	p.skipWhitespace()

	// Check for parenthesized expression
	if p.peek() == '(' {
		p.pos++
		p.skipWhitespace()

		value, err := p.parseOr()
		if err != nil {
			return false, err
		}

		p.skipWhitespace()
		if p.peek() != ')' {
			return false, fmt.Errorf("expected ')' at position %d", p.pos)
		}
		p.pos++

		return value, nil
	}

	// Parse tag identifier
	tag := p.parseTag()
	if tag == "" {
		return false, fmt.Errorf("expected tag identifier at position %d", p.pos)
	}

	// Check if tag exists in testTags
	return p.hasTag(tag), nil
}

// parseTag extracts a tag identifier (alphanumeric + hyphens)
func (p *tagParser) parseTag() string {
	start := p.pos

	for p.pos < len(p.expr) {
		ch, width := utf8.DecodeRuneInString(p.expr[p.pos:])
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '-' || ch == '_' {
			p.pos += width
		} else {
			break
		}
	}

	return p.expr[start:p.pos]
}

// hasTag checks if a tag exists in the test tags
func (p *tagParser) hasTag(tag string) bool {
	for _, t := range p.testTags {
		if t == tag {
			return true
		}
	}
	return false
}

// peek returns the current character without consuming it
func (p *tagParser) peek() byte {
	if p.pos >= len(p.expr) {
		return 0
	}
	return p.expr[p.pos]
}

// consumeOperator attempts to consume a multi-character operator
func (p *tagParser) consumeOperator(op string) bool {
	if p.pos+len(op) > len(p.expr) {
		return false
	}

	if p.expr[p.pos:p.pos+len(op)] == op {
		p.pos += len(op)
		return true
	}

	return false
}

// skipWhitespace skips whitespace characters
func (p *tagParser) skipWhitespace() {
	for p.pos < len(p.expr) {
		ch, width := utf8.DecodeRuneInString(p.expr[p.pos:])
		if !unicode.IsSpace(ch) {
			break
		}
		p.pos += width
	}
}
