package scenario

import (
	"fmt"
	"strings"
	"unicode"
)

// compileKeyword parses a pytest-style -k expression into a predicate over a
// (lowercased) haystack. Grammar:
//
//	expr   := or
//	or     := and ( 'or' and )*
//	and    := not ( 'and' not )*
//	not    := 'not' not | atom
//	atom   := '(' expr ')' | IDENT
//
// An IDENT matches if it is a case-insensitive substring of the haystack;
// 'and', 'or', 'not' are reserved operators.
func compileKeyword(expr string) (func(haystack string) bool, error) {
	toks, err := lexKeyword(expr)
	if err != nil {
		return nil, err
	}
	p := &kwParser{toks: toks}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("-k: unexpected %q", p.toks[p.pos])
	}
	return node, nil
}

func lexKeyword(expr string) ([]string, error) {
	var toks []string
	var ident strings.Builder
	flush := func() {
		if ident.Len() > 0 {
			toks = append(toks, ident.String())
			ident.Reset()
		}
	}
	for _, r := range expr {
		switch {
		case r == '(' || r == ')':
			flush()
			toks = append(toks, string(r))
		case unicode.IsSpace(r):
			flush()
		default:
			ident.WriteRune(r)
		}
	}
	flush()
	return toks, nil
}

type kwParser struct {
	toks []string
	pos  int
}

func (p *kwParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *kwParser) parseOr() (func(string) bool, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for strings.EqualFold(p.peek(), "or") {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l, r := left, right
		left = func(h string) bool { return l(h) || r(h) }
	}
	return left, nil
}

func (p *kwParser) parseAnd() (func(string) bool, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for strings.EqualFold(p.peek(), "and") {
		p.pos++
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l, r := left, right
		left = func(h string) bool { return l(h) && r(h) }
	}
	return left, nil
}

func (p *kwParser) parseNot() (func(string) bool, error) {
	if strings.EqualFold(p.peek(), "not") {
		p.pos++
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return func(h string) bool { return !inner(h) }, nil
	}
	return p.parseAtom()
}

func (p *kwParser) parseAtom() (func(string) bool, error) {
	tok := p.peek()
	switch {
	case tok == "":
		return nil, fmt.Errorf("-k: unexpected end of expression")
	case tok == "(":
		p.pos++
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("-k: missing ')'")
		}
		p.pos++
		return inner, nil
	case tok == ")":
		return nil, fmt.Errorf("-k: unexpected ')'")
	case strings.EqualFold(tok, "and"), strings.EqualFold(tok, "or"):
		return nil, fmt.Errorf("-k: unexpected operator %q", tok)
	default:
		p.pos++
		needle := strings.ToLower(tok)
		return func(h string) bool { return strings.Contains(h, needle) }, nil
	}
}
