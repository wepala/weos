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
	"github.com/wepala/weos/v3/pkg/sparql"
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

// NewStore constructs a Store. Endpoint is required and must be an absolute
// HTTP(S) URL with a host (e.g. http://localhost:7878). Scheme-less values
// like "localhost:7878" are rejected here — url.Parse accepts them but the
// HTTP client later fails opaquely with "missing protocol scheme".
// QueryTimeoutSeconds <= 0 falls back to defaultTimeoutSec.
func NewStore(opts Options) (*Store, error) {
	if opts.Endpoint == "" {
		return nil, fmt.Errorf("oxigraph: endpoint is required")
	}
	parsed, err := url.Parse(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("oxigraph: invalid endpoint %q: %w", opts.Endpoint, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf(
			"oxigraph: endpoint %q must use http or https scheme", opts.Endpoint)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf(
			"oxigraph: endpoint %q is missing a host", opts.Endpoint)
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

// Update executes an arbitrary SPARQL UPDATE. Latency logged at DEBUG.
func (s *Store) Update(ctx context.Context, sparql string) error {
	start := time.Now()
	resp, err := s.do(ctx, http.MethodPost, "/update",
		mimeSPARQLUpdate, "", []byte(sparql))
	if s.logger != nil {
		s.logger.Debug(ctx, "oxigraph update",
			"elapsed_ms", time.Since(start).Milliseconds(),
			"update_bytes", len(sparql))
	}
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("oxigraph update failed: %s: %s", resp.Status, body)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Query executes a SPARQL query (SELECT / ASK / CONSTRUCT / DESCRIBE) and
// normalizes the response into a KGQueryResult. The form is detected from the
// query text (first non-PREFIX/non-comment keyword). Latency is logged at
// DEBUG (elapsed_ms field) so operators can spot slow queries when they
// turn LOG_LEVEL up.
func (s *Store) Query(ctx context.Context, sparql string) (repositories.KGQueryResult, error) {
	form := detectQueryForm(sparql)
	accept := mimeResultsJSON
	if form == queryFormConstruct || form == queryFormDescribe {
		accept = mimeNTriples
	}
	start := time.Now()
	resp, err := s.do(ctx, http.MethodPost, "/query", mimeSPARQLQuery, accept, []byte(sparql))
	if s.logger != nil {
		s.logger.Debug(ctx, "oxigraph query",
			"form", queryFormName(form),
			"elapsed_ms", time.Since(start).Milliseconds(),
			"query_bytes", len(sparql))
	}
	if err != nil {
		return repositories.KGQueryResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
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
		triples, skipped, sample := parseNTriples(body)
		if skipped > 0 && s.logger != nil {
			s.logger.Warn(ctx, "oxigraph: parser skipped malformed N-Triples lines",
				"skipped", skipped, "sample", sample, "kept", len(triples))
		}
		if triples == nil {
			// An empty graph result must still be tagged as a graph form. The
			// MCP adapter's resultForm() checks `Triples != nil` to distinguish
			// CONSTRUCT/DESCRIBE from SELECT, so a nil slice would be
			// misreported as a SELECT with no bindings.
			triples = []repositories.Triple{}
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
	defer func() { _ = resp.Body.Close() }()
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

// IsEmpty reports whether the default graph contains zero triples. A nil
// Boolean from an ASK query is treated as a protocol error rather than a
// silent "empty" so the backfill doesn't accidentally rebuild a populated
// graph because Oxigraph returned a malformed response.
func (s *Store) IsEmpty(ctx context.Context) (bool, error) {
	res, err := s.Query(ctx, "ASK { ?s ?p ?o }")
	if err != nil {
		return false, err
	}
	if res.Boolean == nil {
		return false, fmt.Errorf("oxigraph: ASK response missing boolean field")
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
// responses from Oxigraph. Returns the parsed triples, a count of malformed
// lines that were dropped, and the first malformed line (truncated) as a
// debugging sample. The caller decides whether to surface the skip count —
// returning it explicitly avoids the silent-data-loss trap a bare `error`
// return invited.
func parseNTriples(body []byte) (triples []repositories.Triple, skipped int, sample string) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSuffix(line, ".")
		line = strings.TrimSpace(line)
		t, ok := tryParseNTriplesLine(line)
		if !ok {
			skipped++
			if sample == "" {
				sample = truncate(line, 80)
			}
			continue
		}
		triples = append(triples, t)
	}
	return triples, skipped, sample
}

// tryParseNTriplesLine parses a single N-Triples line. Returns ok=false on
// any structural problem; the caller counts and samples skips at the parser
// level.
func tryParseNTriplesLine(line string) (repositories.Triple, bool) {
	s, rest, ok := splitNTriplesTerm(line)
	if !ok {
		return repositories.Triple{}, false
	}
	p, rest2, ok := splitNTriplesTerm(rest)
	if !ok {
		return repositories.Triple{}, false
	}
	o, _, ok := splitNTriplesTerm(rest2)
	if !ok {
		return repositories.Triple{}, false
	}
	return repositories.Triple{
		Subject:   unquoteTerm(s),
		Predicate: unquoteTerm(p),
		Object:    unquoteTerm(o),
	}, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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

// queryFormName returns a human-readable tag for a queryForm — used in
// debug logs so operators see "select" instead of an opaque integer.
func queryFormName(f queryForm) string {
	switch f {
	case queryFormAsk:
		return "ask"
	case queryFormConstruct:
		return "construct"
	case queryFormDescribe:
		return "describe"
	default:
		return "select"
	}
}

// detectQueryForm returns the high-level SPARQL form so we can pick the right
// Accept header. It delegates to sparql.DetectForm, which strips the prologue
// (comments + PREFIX/BASE, including inline single-line prologues) before
// reading the keyword — so a one-line `PREFIX s: <...> CONSTRUCT { ... }` is
// correctly classified as CONSTRUCT and gets the N-Triples Accept header.
func detectQueryForm(q string) queryForm {
	switch sparql.DetectForm(q) {
	case "ASK":
		return queryFormAsk
	case "CONSTRUCT":
		return queryFormConstruct
	case "DESCRIBE":
		return queryFormDescribe
	default:
		return queryFormSelect
	}
}

// --- Term formatting ---

// formatTriple renders a triple as N-Triples-style "<s> <p> <o> ." with the
// trailing period required by SPARQL UPDATE INSERT DATA / DELETE DATA.
func formatTriple(t repositories.Triple) string {
	return fmt.Sprintf("%s %s %s .", formatTerm(t.Subject), formatTerm(t.Predicate), formatTerm(t.Object))
}

// iriForbiddenChars are the characters the IRIREF grammar (RFC 3987 /
// SPARQL 1.1) forbids inside `<...>`. Shared by escapeIRI and
// isWellFormedIRIRef so the two never drift.
const iriForbiddenChars = "<>\"{}|^\\`"

// formatTerm wraps a term in the appropriate N-Triples syntax.
//   - A preformatted `<iri>` passes through ONLY if it is a single
//     well-formed IRIREF (no characters that could close it early or break
//     out into adjacent SPARQL); otherwise the whole value is escaped as a
//     bare IRI.
//   - A preformatted `"literal"` passes through ONLY if it is a single
//     well-formed literal (optional @lang / ^^<datatype>); otherwise the
//     whole value is re-escaped as an N-Triples literal.
//   - `_:bnode` blank nodes pass through.
//   - Anything else is treated as a bare IRI and percent-encoded.
//
// This is the canonical formatter for values produced by
// `application/triple_extraction.go`, which carry bare strings sourced from
// JSON-LD @id values. Validating the preformatted forms (rather than trusting
// any leading `<` or `"`) defends against SPARQL injection when such a value
// is adversarial — e.g. an @id of `<urn:x> <p> <o> . <urn:evil>`.
func formatTerm(v string) string {
	if v == "" {
		return `""`
	}
	switch v[0] {
	case '<':
		if isWellFormedIRIRef(v) {
			return v
		}
	case '"':
		if isWellFormedLiteral(v) {
			return v
		}
		return formatLiteral(v)
	case '_':
		if strings.HasPrefix(v, "_:") {
			return v
		}
	}
	return "<" + escapeIRI(v) + ">"
}

// isWellFormedIRIRef reports whether v is a single, fully-bracketed IRIREF
// (`<...>`) whose interior contains no character that could terminate it early
// or break out into adjacent SPARQL. Gate for passing a `<...>` term through
// formatTerm unescaped.
func isWellFormedIRIRef(v string) bool {
	if len(v) < 2 || v[0] != '<' || v[len(v)-1] != '>' {
		return false
	}
	inner := v[1 : len(v)-1]
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if c <= 0x20 || c == 0x7f || strings.IndexByte(iriForbiddenChars, c) >= 0 {
			return false
		}
	}
	return true
}

// isWellFormedLiteral reports whether v is a single well-formed N-Triples
// literal: an opening quote, backslash-escaped content with no raw control
// chars or unescaped quotes, a closing quote, and an optional language tag
// (@lang) or datatype (^^<iri>) suffix — with nothing trailing that could
// break out into adjacent SPARQL.
func isWellFormedLiteral(v string) bool {
	if len(v) < 2 || v[0] != '"' {
		return false
	}
	i := 1
	for i < len(v) {
		c := v[i]
		switch {
		case c == '\\':
			if i+1 >= len(v) || strings.IndexByte(`tbnrf"\/uU`, v[i+1]) < 0 {
				return false
			}
			i += 2
			continue
		case c == '"':
			return isValidLiteralSuffix(v[i+1:])
		case c < 0x20:
			return false // raw control char (incl. newline) must be escaped
		}
		i++
	}
	return false // no closing quote
}

// isValidLiteralSuffix validates what may follow a literal's closing quote:
// nothing, a `@language` tag, or a `^^<datatype>` IRI.
func isValidLiteralSuffix(suffix string) bool {
	switch {
	case suffix == "":
		return true
	case suffix[0] == '@':
		tag := suffix[1:]
		if tag == "" {
			return false
		}
		for i := 0; i < len(tag); i++ {
			c := tag[i]
			valid := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
			if !valid {
				return false
			}
		}
		return true
	case strings.HasPrefix(suffix, "^^"):
		return isWellFormedIRIRef(suffix[2:])
	default:
		return false
	}
}

// formatLiteral renders v as an N-Triples string literal, escaping the
// characters that would otherwise break out of the surrounding quotes.
func formatLiteral(v string) string {
	var sb strings.Builder
	sb.Grow(len(v) + 2)
	sb.WriteByte('"')
	for i := 0; i < len(v); i++ {
		switch c := v[i]; c {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// escapeIRI percent-encodes characters that would break an IRIREF in SPARQL
// or N-Triples. The grammar (RFC 3987 / SPARQL 1.1) forbids 0x00–0x20,
// `<`, `>`, `"`, `{`, `}`, `|`, `^`, `\`, and backtick inside `<...>`. We
// percent-encode anything in that set instead of stripping so the resulting
// IRI still round-trips back to the original via standard URL decoding.
func escapeIRI(v string) string {
	hasBad := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c <= 0x20 || c == 0x7f || strings.IndexByte(iriForbiddenChars, c) >= 0 {
			hasBad = true
			break
		}
	}
	if !hasBad {
		return v
	}
	var sb strings.Builder
	sb.Grow(len(v) + 8)
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c <= 0x20 || c == 0x7f || strings.IndexByte(iriForbiddenChars, c) >= 0 {
			fmt.Fprintf(&sb, "%%%02X", c)
			continue
		}
		sb.WriteByte(c)
	}
	return sb.String()
}
