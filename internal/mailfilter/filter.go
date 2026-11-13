// SPDX-License-Identifier: AGPL-3.0-only
// Package mailfilter compiles bounded local mail predicates, never scripts/SQL.
package mailfilter

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrQuery = errors.New("invalid filter: use from:, to:, domain:, subject:, body:, AND/OR and parentheses (maximum 16 conditions, depth 4)")

type Message struct {
	From                string
	To                  []string
	Subject, Name, Text string
	Encrypted           bool
}
type Expr struct {
	field, value, op string
	children         []*Expr
}
type token struct {
	text   string
	quoted bool
}
type parser struct {
	tokens      []token
	pos, leaves int
}

func known(field string) bool {
	switch field {
	case "from", "to", "domain", "subject", "body", "metadata":
		return true
	}
	return false
}

// Compile preserves legacy plain phrase searches. Structured queries use AND
// before OR; adjacent conditions are AND. Body matching never reads ciphertext.
func Compile(query string) (*Expr, error) {
	if len(query) > 2048 || !utf8.ValidString(query) || strings.ContainsAny(query, "\r\n\x00") {
		return nil, ErrQuery
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return &Expr{field: "metadata"}, nil
	}
	tokens, err := lex(query)
	if err != nil {
		return nil, err
	}
	structured := false
	for _, t := range tokens {
		field, _, has := strings.Cut(t.text, ":")
		if has && known(field) || !t.quoted && (t.text == "AND" || t.text == "OR" || t.text == "(" || t.text == ")") {
			structured = true
		}
	}
	if !structured {
		if len(tokens) == 1 && tokens[0].quoted {
			query = tokens[0].text
		}
		return &Expr{field: "metadata", value: strings.ToLower(query)}, nil
	}
	p := parser{tokens: tokens}
	root, err := p.or(0)
	if err != nil || p.pos != len(tokens) {
		return nil, ErrQuery
	}
	return root, nil
}
func lex(query string) ([]token, error) {
	result := []token{}
	for i := 0; i < len(query); {
		if query[i] == ' ' || query[i] == '\t' {
			i++
			continue
		}
		if query[i] == '(' || query[i] == ')' {
			result = append(result, token{text: query[i : i+1]})
			i++
			continue
		}
		var b strings.Builder
		quote, quoted := false, false
		for i < len(query) {
			c := query[i]
			if c == '"' {
				quote = !quote
				quoted = true
				i++
				continue
			}
			if c == '\\' && quote {
				i++
				if i >= len(query) || (query[i] != '"' && query[i] != '\\') {
					return nil, ErrQuery
				}
				b.WriteByte(query[i])
				i++
				continue
			}
			if !quote && (c == ' ' || c == '\t' || c == '(' || c == ')') {
				break
			}
			b.WriteByte(c)
			i++
		}
		if quote || b.Len() == 0 {
			return nil, ErrQuery
		}
		result = append(result, token{text: b.String(), quoted: quoted})
		if len(result) > 64 {
			return nil, ErrQuery
		}
	}
	return result, nil
}
func (p *parser) is(s string) bool {
	return p.pos < len(p.tokens) && !p.tokens[p.pos].quoted && p.tokens[p.pos].text == s
}
func (p *parser) or(depth int) (*Expr, error) {
	first, err := p.and(depth)
	if err != nil {
		return nil, err
	}
	items := []*Expr{first}
	for p.is("OR") {
		p.pos++
		next, err := p.and(depth)
		if err != nil {
			return nil, err
		}
		items = append(items, next)
	}
	if len(items) == 1 {
		return first, nil
	}
	return &Expr{op: "or", children: items}, nil
}
func (p *parser) and(depth int) (*Expr, error) {
	first, err := p.atom(depth)
	if err != nil {
		return nil, err
	}
	items := []*Expr{first}
	for p.pos < len(p.tokens) && !p.is("OR") && !p.is(")") {
		if p.is("AND") {
			p.pos++
		}
		next, err := p.atom(depth)
		if err != nil {
			return nil, err
		}
		items = append(items, next)
	}
	if len(items) == 1 {
		return first, nil
	}
	return &Expr{op: "and", children: items}, nil
}
func (p *parser) atom(depth int) (*Expr, error) {
	if p.pos >= len(p.tokens) || depth > 4 || p.is("AND") || p.is("OR") || p.is(")") {
		return nil, ErrQuery
	}
	if p.is("(") {
		p.pos++
		inner, err := p.or(depth + 1)
		if err != nil || !p.is(")") {
			return nil, ErrQuery
		}
		p.pos++
		return inner, nil
	}
	t := p.tokens[p.pos]
	p.pos++
	p.leaves++
	if p.leaves > 16 {
		return nil, ErrQuery
	}
	field, value, has := strings.Cut(t.text, ":")
	if !has {
		field, value = "metadata", t.text
	} else if !known(field) {
		return nil, ErrQuery
	}
	if value == "" || len(value) > 254 {
		return nil, ErrQuery
	}
	return &Expr{field: field, value: strings.ToLower(value)}, nil
}
func (e *Expr) Match(m Message) bool {
	if e == nil {
		return false
	}
	if e.op == "and" {
		for _, c := range e.children {
			if !c.Match(m) {
				return false
			}
		}
		return true
	}
	if e.op == "or" {
		for _, c := range e.children {
			if c.Match(m) {
				return true
			}
		}
		return false
	}
	contains := func(s string) bool { return strings.Contains(strings.ToLower(s), e.value) }
	switch e.field {
	case "from":
		return strings.EqualFold(m.From, e.value)
	case "to":
		for _, to := range m.To {
			if strings.EqualFold(to, e.value) {
				return true
			}
		}
		return false
	case "domain":
		at := strings.LastIndexByte(m.From, '@')
		return at >= 0 && strings.EqualFold(m.From[at+1:], e.value)
	case "subject":
		return contains(m.Subject)
	case "body":
		if m.Encrypted {
			return false
		}
		text := m.Text
		if len(text) > 65536 {
			text = text[:65536]
		}
		return contains(text)
	case "metadata":
		return contains(m.From + " " + strings.Join(m.To, " ") + " " + m.Subject + " " + m.Name)
	}
	return false
}

// String returns a bounded-expression representation for the rule draft only.
func (e *Expr) String() string {
	if e == nil {
		return ""
	}
	if e.op != "" {
		parts := []string{}
		for _, child := range e.children {
			parts = append(parts, child.String())
		}
		return "(" + strings.Join(parts, " "+strings.ToUpper(e.op)+" ") + ")"
	}
	return e.field + ":\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(e.value) + "\""
}
