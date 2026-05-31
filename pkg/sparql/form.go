// Package sparql holds small, dependency-free helpers for inspecting SPARQL
// query text without a full parse.
package sparql

import (
	"strings"
	"unicode"
)

// DetectForm returns the SPARQL query form — "SELECT", "ASK", "CONSTRUCT", or
// "DESCRIBE" — after stripping the prologue (comments and PREFIX/BASE
// declarations), whether those appear on their own lines or inline ahead of
// the form keyword on a single line (`PREFIX s: <...> ASK { ... }`). Anything
// that isn't ASK/CONSTRUCT/DESCRIBE is reported as SELECT, the permissive
// default.
//
// This is the single source of truth for form detection, shared by the MCP
// scoped-query guard and the Oxigraph store's Accept-header selection so the
// two can't drift (e.g. one treating a one-line prefixed ASK as SELECT while
// the other treats it as ASK).
func DetectForm(query string) string {
	rest := strings.ToUpper(stripPrologue(query))
	switch {
	case hasKeyword(rest, "ASK"):
		return "ASK"
	case hasKeyword(rest, "CONSTRUCT"):
		return "CONSTRUCT"
	case hasKeyword(rest, "DESCRIBE"):
		return "DESCRIBE"
	default:
		return "SELECT"
	}
}

// stripPrologue removes leading whitespace, comment lines, and PREFIX/BASE
// declarations, returning the remainder beginning at the form keyword. It
// works token-by-token rather than line-by-line, so a one-line query whose
// prologue and form keyword share a line is handled correctly.
func stripPrologue(q string) string {
	for {
		q = strings.TrimLeftFunc(q, unicode.IsSpace)
		upper := strings.ToUpper(q)
		switch {
		case strings.HasPrefix(q, "#"):
			nl := strings.IndexByte(q, '\n')
			if nl < 0 {
				return "" // comment runs to end of input
			}
			q = q[nl+1:]
		case hasKeyword(upper, "PREFIX"):
			q = strings.TrimLeftFunc(q[len("PREFIX"):], unicode.IsSpace)
			if c := strings.IndexByte(q, ':'); c >= 0 {
				q = q[c+1:] // drop the prefix label up to and including ':'
			}
			q = skipIRIRef(strings.TrimLeftFunc(q, unicode.IsSpace))
		case hasKeyword(upper, "BASE"):
			q = skipIRIRef(strings.TrimLeftFunc(q[len("BASE"):], unicode.IsSpace))
		default:
			return q
		}
	}
}

// skipIRIRef drops a leading <...> IRI reference, if present.
func skipIRIRef(q string) string {
	if !strings.HasPrefix(q, "<") {
		return q
	}
	if _, after, found := strings.Cut(q, ">"); found {
		return after
	}
	return "" // unterminated — nothing usable follows
}

// hasKeyword reports whether s begins with kw as a whole token (kw followed by
// a non-identifier byte or end of string), so "ASK" doesn't match "ASKING" and
// "BASE" doesn't match a longer token. s is expected to be upper-cased.
func hasKeyword(s, kw string) bool {
	if !strings.HasPrefix(s, kw) {
		return false
	}
	if len(s) == len(kw) {
		return true
	}
	c := s[len(kw)]
	return !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_')
}
