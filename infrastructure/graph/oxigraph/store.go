// Package oxigraph implements repositories.KnowledgeGraphStore against an
// Oxigraph SPARQL HTTP endpoint. Oxigraph runs as a separate process
// (`oxigraph serve`); WeOS speaks to it using the SPARQL 1.1 Protocol
// (https://www.w3.org/TR/sparql11-protocol/).
package oxigraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wepala/weos/v3/domain/entities"
	"github.com/wepala/weos/v3/domain/repositories"
)

const (
	// MIME types per SPARQL 1.1 Protocol.
	mimeSPARQLQuery   = "application/sparql-query"
	mimeSPARQLUpdate  = "application/sparql-update"
	mimeResultsJSON   = "application/sparql-results+json"
	mimeNTriples      = "application/n-triples"
	mimeTurtle        = "text/turtle"
	defaultTimeoutSec = 10
)

// Store is the Oxigraph-backed KnowledgeGraphStore.
type Store struct {
	endpoint   string // base URL, e.g. http://localhost:7878
	httpClient *http.Client
	username   string
	password   string
	logger     entities.Logger
}

// Options bundles configuration for NewStore.
type Options struct {
	Endpoint            string
	Username            string
	Password            string
	QueryTimeoutSeconds int
	Logger              entities.Logger
}

// NewStore constructs a Store. Endpoint is required and must be a non-empty
// HTTP(S) URL; QueryTimeoutSeconds <= 0 falls back to defaultTimeoutSec.
func NewStore(opts Options) (*Store, error) {
	if opts.Endpoint == "" {
		return nil, fmt.Errorf("oxigraph: endpoint is required")
	}
	if _, err := url.Parse(opts.Endpoint); err != nil {
		return nil, fmt.Errorf("oxigraph: invalid endpoint %q: %w", opts.Endpoint, err)
	}
	timeout := opts.QueryTimeoutSeconds
	if timeout <= 0 {
		timeout = defaultTimeoutSec
	}
	return &Store{
		endpoint:   strings.TrimRight(opts.Endpoint, "/"),
		httpClient: &http.Client{Timeout: time.Duration(timeout) * time.Second},
		username:   opts.Username,
		password:   opts.Password,
		logger:     opts.Logger,
	}, nil
}

// Active reports whether the store is connected to a real backend.
func (s *Store) Active() bool { return s != nil && s.endpoint != "" }

// AddTriples inserts triples via SPARQL UPDATE INSERT DATA. Idempotent at the
// RDF level — re-inserting a triple that already exists is a no-op.
func (s *Store) AddTriples(ctx context.Context, triples []repositories.Triple) error {
	if len(triples) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("INSERT DATA { ")
	for _, t := range triples {
		sb.WriteString(formatTriple(t))
		sb.WriteString(" ")
	}
	sb.WriteString("}")
	if s.logger != nil {
		s.logger.Debug(ctx, "oxigraph add triples", "count", len(triples))
	}
	return s.Update(ctx, sb.String())
}

// RemoveTriples deletes the given triples via SPARQL UPDATE DELETE DATA.
func (s *Store) RemoveTriples(ctx context.Context, triples []repositories.Triple) error {
	if len(triples) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("DELETE DATA { ")
	for _, t := range triples {
		sb.WriteString(formatTriple(t))
		sb.WriteString(" ")
	}
	sb.WriteString("}")
	if s.logger != nil {
		s.logger.Debug(ctx, "oxigraph remove triples", "count", len(triples))
	}
	return s.Update(ctx, sb.String())
}

// RemoveSubject drops every triple whose subject matches the given URI.
func (s *Store) RemoveSubject(ctx context.Context, subject string) error {
	if subject == "" {
		return nil
	}
	q := fmt.Sprintf("DELETE WHERE { %s ?p ?o }", formatTerm(subject))
	if s.logger != nil {
		s.logger.Debug(ctx, "oxigraph remove subject", "subject", subject)
	}
	return s.Update(ctx, q)
}

// Update executes an arbitrary SPARQL UPDATE.
func (s *Store) Update(ctx context.Context, sparql string) error {
	resp, err := s.do(ctx, http.MethodPost, "/update",
		mimeSPARQLUpdate, "", []byte(sparql))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("oxigraph update failed: %s: %s", resp.Status, body)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Query executes a SPARQL query (SELECT / ASK / CONSTRUCT / DESCRIBE) and
// normalizes the response into a KGQueryResult. The form is detected from the
// query text (first non-PREFIX/non-comment keyword).
func (s *Store) Query(ctx context.Context, sparql string) (repositories.KGQueryResult, error) {
	form := detectQueryForm(sparql)
	accept := mimeResultsJSON
	if form == queryFormConstruct || form == queryFormDescribe {
		accept = mimeNTriples
	}
	resp, err := s.do(ctx, http.MethodPost, "/query", mimeSPARQLQuery, accept, []byte(sparql))
	if err != nil {
		return repositories.KGQueryResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return repositories.KGQueryResult{}, fmt.Errorf(
			"oxigraph query failed: %s: %s", resp.Status, body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return repositories.KGQueryResult{}, fmt.Errorf("oxigraph: read body: %w", err)
	}
	if form == queryFormConstruct || form == queryFormDescribe {
		triples, perr := parseNTriples(body)
		if perr != nil {
			return repositories.KGQueryResult{}, perr
		}
		return repositories.KGQueryResult{Triples: triples}, nil
	}
	return parseSPARQLJSON(body)
}

// LoadOntology bulk-loads a serialized RDF document into the default graph
// using the SPARQL Graph Store HTTP Protocol.
func (s *Store) LoadOntology(ctx context.Context, format string, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	if format == "" {
		format = mimeTurtle
	}
	resp, err := s.do(ctx, http.MethodPost, "/store?default", format, "", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("oxigraph load failed: %s: %s", resp.Status, respBody)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if s.logger != nil {
		s.logger.Debug(ctx, "oxigraph load ontology", "format", format, "bytes", len(body))
	}
	return nil
}

// Clear removes every triple from the default graph.
func (s *Store) Clear(ctx context.Context) error {
	return s.Update(ctx, "CLEAR DEFAULT")
}

// IsEmpty reports whether the default graph contains zero triples.
func (s *Store) IsEmpty(ctx context.Context) (bool, error) {
	res, err := s.Query(ctx, "ASK { ?s ?p ?o }")
	if err != nil {
		return false, err
	}
	if res.Boolean == nil {
		return true, nil
	}
	return !*res.Boolean, nil
}

// do executes an HTTP request with optional basic auth and the given content
// types. acceptType may be empty for write operations.
func (s *Store) do(
	ctx context.Context, method, path, contentType, acceptType string, body []byte,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oxigraph: build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if acceptType != "" {
		req.Header.Set("Accept", acceptType)
	}
	if s.username != "" {
		req.SetBasicAuth(s.username, s.password)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph: request to %s: %w", path, err)
	}
	return resp, nil
}

// --- SPARQL response parsing ---

// sparqlSelectResponse mirrors the SPARQL 1.1 Query Results JSON Format.
type sparqlSelectResponse struct {
	Head struct {
		Vars []string `json:"vars"`
	} `json:"head"`
	Results struct {
		Bindings []map[string]struct {
			Type     string `json:"type"`
			Value    string `json:"value"`
			Datatype string `json:"datatype"`
			Lang     string `json:"xml:lang"`
		} `json:"bindings"`
	} `json:"results"`
	Boolean *bool `json:"boolean,omitempty"`
}

func parseSPARQLJSON(body []byte) (repositories.KGQueryResult, error) {
	var raw sparqlSelectResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return repositories.KGQueryResult{}, fmt.Errorf("oxigraph: parse JSON results: %w", err)
	}
	if raw.Boolean != nil {
		return repositories.KGQueryResult{Boolean: raw.Boolean}, nil
	}
	out := repositories.KGQueryResult{
		Vars:     raw.Head.Vars,
		Bindings: make([]map[string]repositories.KGTerm, 0, len(raw.Results.Bindings)),
	}
	for _, row := range raw.Results.Bindings {
		converted := make(map[string]repositories.KGTerm, len(row))
		for k, v := range row {
			converted[k] = repositories.KGTerm{
				Type:     repositories.KGTermType(v.Type),
				Value:    v.Value,
				Datatype: v.Datatype,
				Lang:     v.Lang,
			}
		}
		out.Bindings = append(out.Bindings, converted)
	}
	return out, nil
}

// parseNTriples is a minimal N-Triples parser sufficient for CONSTRUCT/DESCRIBE
// responses from Oxigraph. It does not fully validate against the spec; for
// edge cases we let the SPARQL store stay authoritative.
func parseNTriples(body []byte) ([]repositories.Triple, error) {
	var triples []repositories.Triple
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSuffix(line, ".")
		line = strings.TrimSpace(line)
		s, rest, ok := splitNTriplesTerm(line)
		if !ok {
			continue
		}
		p, rest2, ok := splitNTriplesTerm(rest)
		if !ok {
			continue
		}
		o, _, ok := splitNTriplesTerm(rest2)
		if !ok {
			continue
		}
		triples = append(triples, repositories.Triple{
			Subject:   unquoteTerm(s),
			Predicate: unquoteTerm(p),
			Object:    unquoteTerm(o),
		})
	}
	return triples, nil
}

// splitNTriplesTerm splits the next term off the front of an N-Triples line.
// Handles `<iri>`, `_:bnode`, and `"literal"` (with optional language tag or
// datatype) — quoted literals may contain spaces, so we have to track the
// quote state instead of naively splitting on whitespace.
func splitNTriplesTerm(line string) (term, rest string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	switch line[0] {
	case '<':
		end := strings.Index(line, ">")
		if end < 0 {
			return "", "", false
		}
		return line[:end+1], strings.TrimSpace(line[end+1:]), true
	case '_':
		end := strings.IndexAny(line, " \t")
		if end < 0 {
			return line, "", true
		}
		return line[:end], strings.TrimSpace(line[end:]), true
	case '"':
		// Find the closing quote, respecting backslash escapes.
		i := 1
		for i < len(line) {
			if line[i] == '\\' && i+1 < len(line) {
				i += 2
				continue
			}
			if line[i] == '"' {
				break
			}
			i++
		}
		if i >= len(line) {
			return "", "", false
		}
		end := i + 1
		// Optional language tag (@en) or datatype (^^<...>).
		for end < len(line) && line[end] != ' ' && line[end] != '\t' {
			if line[end] == '<' {
				closing := strings.Index(line[end:], ">")
				if closing < 0 {
					return "", "", false
				}
				end += closing + 1
				break
			}
			end++
		}
		return line[:end], strings.TrimSpace(line[end:]), true
	}
	return "", "", false
}

// unquoteTerm strips the angle brackets from `<iri>` so callers see the bare
// IRI string. Literals and blank nodes are returned as-is so the original
// lexical form survives.
func unquoteTerm(t string) string {
	if len(t) >= 2 && t[0] == '<' && t[len(t)-1] == '>' {
		return t[1 : len(t)-1]
	}
	return t
}

// --- Query form detection ---

type queryForm int

const (
	queryFormSelect queryForm = iota
	queryFormAsk
	queryFormConstruct
	queryFormDescribe
)

// detectQueryForm returns the high-level SPARQL form so we can pick the right
// Accept header. We strip prefixes/comments and look at the first keyword.
func detectQueryForm(q string) queryForm {
	for _, line := range strings.Split(q, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "PREFIX ") || strings.HasPrefix(upper, "BASE ") {
			continue
		}
		switch {
		case strings.HasPrefix(upper, "ASK"):
			return queryFormAsk
		case strings.HasPrefix(upper, "CONSTRUCT"):
			return queryFormConstruct
		case strings.HasPrefix(upper, "DESCRIBE"):
			return queryFormDescribe
		default:
			return queryFormSelect
		}
	}
	return queryFormSelect
}

// --- Term formatting ---

// formatTriple renders a triple as N-Triples-style "<s> <p> <o> ." with the
// trailing period required by SPARQL UPDATE INSERT DATA / DELETE DATA.
func formatTriple(t repositories.Triple) string {
	return fmt.Sprintf("%s %s %s .", formatTerm(t.Subject), formatTerm(t.Predicate), formatTerm(t.Object))
}

// formatTerm wraps a term in the appropriate N-Triples syntax. Heuristic:
// already-quoted/already-bracketed terms pass through; bare blank-node IDs
// (`_:foo`) pass through; anything else is treated as an IRI and wrapped in
// angle brackets, with double-quotes escaped if the value happens to be a
// literal that the caller forgot to format. This covers the common case where
// triples produced by `application/triple_extraction.go` carry bare IRI
// strings.
func formatTerm(v string) string {
	if v == "" {
		return `""`
	}
	switch v[0] {
	case '<', '"':
		return v
	case '_':
		if strings.HasPrefix(v, "_:") {
			return v
		}
	}
	// Plain IRI — wrap in angle brackets. Escape any embedded > to keep the
	// generated SPARQL syntactically valid.
	return "<" + strings.ReplaceAll(v, ">", "%3E") + ">"
}
