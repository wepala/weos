package sparql

import "testing"

func TestDetectForm(t *testing.T) {
	cases := []struct {
		name, query, want string
	}{
		{"plain select", "SELECT ?s WHERE { ?s ?p ?o }", "SELECT"},
		{"plain ask", "ASK { ?s ?p ?o }", "ASK"},
		{"plain construct", "CONSTRUCT { ?s ?p ?o } WHERE { ?s ?p ?o }", "CONSTRUCT"},
		{"plain describe", "DESCRIBE <urn:x>", "DESCRIBE"},
		{"lowercase ask", "ask { ?s ?p ?o }", "ASK"},
		{"leading whitespace", "   \n  ASK { ?s ?p ?o }", "ASK"},

		// The single-line-prologue bug: PREFIX/BASE and the form keyword share
		// a line. Line-by-line skipping mis-read these as SELECT.
		{"one-line prefix + ask", "PREFIX s: <https://schema.org/> ASK { ?x s:ssn ?o }", "ASK"},
		{"one-line prefix + construct", "PREFIX s: <https://schema.org/> CONSTRUCT { ?s ?p ?o } WHERE { ?s ?p ?o }", "CONSTRUCT"},
		{"one-line base + ask", "BASE <https://schema.org/> ASK { ?s ?p ?o }", "ASK"},
		{"multiple prefixes one line", "PREFIX a: <urn:a:> PREFIX b: <urn:b:> ASK { a:x b:p ?o }", "ASK"},

		// Multi-line prologue still works.
		{"multi-line prefix + ask", "PREFIX s: <https://schema.org/>\nASK { ?x s:ssn ?o }", "ASK"},
		{"comment then ask", "# pick anyone\nASK { ?s ?p ?o }", "ASK"},
		{"base no space before iri", "BASE<https://schema.org/>\nSELECT ?s WHERE { ?s ?p ?o }", "SELECT"},

		// Boundary: a token that merely starts with a keyword is not the form.
		{"variable named ?asking", "SELECT ?asking WHERE { ?asking ?p ?o }", "SELECT"},

		{"empty", "", "SELECT"},
		{"comment only", "# nothing here", "SELECT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectForm(tc.query); got != tc.want {
				t.Errorf("DetectForm(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}
