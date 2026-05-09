package oxigraph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wepala/weos/v3/domain/repositories"
)

// fakeOxigraph captures the most recent request so tests can assert on the
// SPARQL/payload that the store sent, and serves canned responses keyed by URL
// path. It plays the role of `oxigraph serve` in unit tests.
type fakeOxigraph struct {
	t          *testing.T
	server     *httptest.Server
	lastMethod string
	lastPath   string
	lastBody   string
	lastCT     string
	lastAccept string
	// responses keyed by request path; last-write-wins.
	responses   map[string]fakeResponse
	requestsLog []string
}

type fakeResponse struct {
	status      int
	contentType string
	body        string
}

func newFakeOxigraph(t *testing.T) *fakeOxigraph {
	t.Helper()
	f := &fakeOxigraph{t: t, responses: map[string]fakeResponse{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.lastMethod = r.Method
		f.lastPath = r.URL.Path + func() string {
			if r.URL.RawQuery == "" {
				return ""
			}
			return "?" + r.URL.RawQuery
		}()
		f.lastBody = string(body)
		f.lastCT = r.Header.Get("Content-Type")
		f.lastAccept = r.Header.Get("Accept")
		f.requestsLog = append(f.requestsLog, f.lastMethod+" "+f.lastPath)

		key := r.URL.Path
		if resp, ok := f.responses[key]; ok {
			if resp.contentType != "" {
				w.Header().Set("Content-Type", resp.contentType)
			}
			w.WriteHeader(resp.status)
			_, _ = w.Write([]byte(resp.body))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOxigraph) replyWith(path string, status int, contentType, body string) {
	f.responses[path] = fakeResponse{status: status, contentType: contentType, body: body}
}

func newTestStore(t *testing.T, endpoint string) *Store {
	t.Helper()
	s, err := NewStore(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestNewStore_RequiresEndpoint(t *testing.T) {
	if _, err := NewStore(Options{}); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestStore_AddTriples_WritesInsertData(t *testing.T) {
	fake := newFakeOxigraph(t)
	store := newTestStore(t, fake.server.URL)

	err := store.AddTriples(context.Background(), []repositories.Triple{
		{Subject: "urn:type:product", Predicate: "https://schema.org/name", Object: "Widget"},
	})
	if err != nil {
		t.Fatalf("AddTriples: %v", err)
	}

	if fake.lastPath != "/update" {
		t.Errorf("path = %q, want /update", fake.lastPath)
	}
	if fake.lastCT != mimeSPARQLUpdate {
		t.Errorf("content-type = %q, want %q", fake.lastCT, mimeSPARQLUpdate)
	}
	if !strings.Contains(fake.lastBody, "INSERT DATA") {
		t.Errorf("body missing INSERT DATA: %q", fake.lastBody)
	}
	if !strings.Contains(fake.lastBody, "<urn:type:product>") {
		t.Errorf("body missing wrapped IRI: %q", fake.lastBody)
	}
}

func TestStore_AddTriples_EmptyIsNoop(t *testing.T) {
	fake := newFakeOxigraph(t)
	store := newTestStore(t, fake.server.URL)

	if err := store.AddTriples(context.Background(), nil); err != nil {
		t.Fatalf("AddTriples(nil): %v", err)
	}
	if len(fake.requestsLog) != 0 {
		t.Errorf("empty input should not hit server; got %v", fake.requestsLog)
	}
}

func TestStore_RemoveSubject_DeletesByPattern(t *testing.T) {
	fake := newFakeOxigraph(t)
	store := newTestStore(t, fake.server.URL)

	if err := store.RemoveSubject(context.Background(), "urn:product:abc"); err != nil {
		t.Fatalf("RemoveSubject: %v", err)
	}

	if !strings.Contains(fake.lastBody, "DELETE WHERE") {
		t.Errorf("body missing DELETE WHERE: %q", fake.lastBody)
	}
	if !strings.Contains(fake.lastBody, "<urn:product:abc>") {
		t.Errorf("body missing wrapped subject: %q", fake.lastBody)
	}
}

func TestStore_Query_SelectParsesJSON(t *testing.T) {
	fake := newFakeOxigraph(t)
	fake.replyWith("/query", http.StatusOK, mimeResultsJSON, `{
		"head": {"vars": ["s", "p"]},
		"results": {"bindings": [
			{"s": {"type": "uri", "value": "urn:product:1"},
			 "p": {"type": "literal", "value": "Widget", "xml:lang": "en"}}
		]}
	}`)
	store := newTestStore(t, fake.server.URL)

	res, err := store.Query(context.Background(), "SELECT ?s ?p WHERE { ?s ?p ?o }")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := fake.lastAccept; got != mimeResultsJSON {
		t.Errorf("accept = %q, want %q", got, mimeResultsJSON)
	}
	if len(res.Bindings) != 1 {
		t.Fatalf("got %d bindings, want 1", len(res.Bindings))
	}
	got := res.Bindings[0]["s"]
	if got.Type != repositories.KGTermIRI || got.Value != "urn:product:1" {
		t.Errorf("subject term wrong: %+v", got)
	}
	if got := res.Bindings[0]["p"]; got.Lang != "en" {
		t.Errorf("language tag dropped: %+v", got)
	}
}

func TestStore_Query_AskParsesBoolean(t *testing.T) {
	fake := newFakeOxigraph(t)
	fake.replyWith("/query", http.StatusOK, mimeResultsJSON, `{"head":{},"boolean":true}`)
	store := newTestStore(t, fake.server.URL)

	res, err := store.Query(context.Background(), "ASK { ?s ?p ?o }")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Boolean == nil || !*res.Boolean {
		t.Errorf("expected boolean=true, got %v", res.Boolean)
	}
}

func TestStore_Query_ConstructParsesNTriples(t *testing.T) {
	fake := newFakeOxigraph(t)
	fake.replyWith("/query", http.StatusOK, mimeNTriples,
		"<urn:product:1> <https://schema.org/name> \"Widget\" .\n"+
			"<urn:product:1> <https://schema.org/price> \"9.99\"^^<http://www.w3.org/2001/XMLSchema#decimal> .\n",
	)
	store := newTestStore(t, fake.server.URL)

	res, err := store.Query(context.Background(), "CONSTRUCT { ?s ?p ?o } WHERE { ?s ?p ?o }")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := fake.lastAccept; got != mimeNTriples {
		t.Errorf("accept = %q, want %q", got, mimeNTriples)
	}
	if len(res.Triples) != 2 {
		t.Fatalf("got %d triples, want 2", len(res.Triples))
	}
	if res.Triples[0].Subject != "urn:product:1" {
		t.Errorf("subject = %q", res.Triples[0].Subject)
	}
}

func TestStore_IsEmpty_TrueWhenAskFalse(t *testing.T) {
	fake := newFakeOxigraph(t)
	fake.replyWith("/query", http.StatusOK, mimeResultsJSON, `{"head":{},"boolean":false}`)
	store := newTestStore(t, fake.server.URL)

	empty, err := store.IsEmpty(context.Background())
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if !empty {
		t.Fatal("IsEmpty should be true when ASK returns false")
	}
}

func TestStore_LoadOntology_PostsToStore(t *testing.T) {
	fake := newFakeOxigraph(t)
	store := newTestStore(t, fake.server.URL)

	turtle := []byte(`@prefix s: <https://schema.org/> . s:Person a s:Class .`)
	if err := store.LoadOntology(context.Background(), "text/turtle", turtle); err != nil {
		t.Fatalf("LoadOntology: %v", err)
	}
	if !strings.HasPrefix(fake.lastPath, "/store") {
		t.Errorf("path = %q, want /store", fake.lastPath)
	}
	if fake.lastCT != "text/turtle" {
		t.Errorf("content-type = %q", fake.lastCT)
	}
	if string(fake.lastBody) != string(turtle) {
		t.Errorf("body mismatch")
	}
}

func TestStore_PropagatesHTTPError(t *testing.T) {
	fake := newFakeOxigraph(t)
	fake.replyWith("/update", http.StatusBadRequest, "text/plain", "syntax error")
	store := newTestStore(t, fake.server.URL)

	err := store.AddTriples(context.Background(), []repositories.Triple{
		{Subject: "s", Predicate: "p", Object: "o"},
	})
	if err == nil {
		t.Fatal("expected error from 400 response")
	}
	if !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("error should include server response, got: %v", err)
	}
}

func TestDetectQueryForm(t *testing.T) {
	cases := []struct {
		query string
		want  queryForm
	}{
		{"SELECT * WHERE { ?s ?p ?o }", queryFormSelect},
		{"ASK { ?s ?p ?o }", queryFormAsk},
		{"CONSTRUCT { ?s ?p ?o } WHERE { ?s ?p ?o }", queryFormConstruct},
		{"DESCRIBE <urn:s>", queryFormDescribe},
		{"PREFIX schema: <https://schema.org/>\nSELECT ?x WHERE { ?x a schema:Person }", queryFormSelect},
		{"# comment\nASK { ?s ?p ?o }", queryFormAsk},
	}
	for _, tc := range cases {
		if got := detectQueryForm(tc.query); got != tc.want {
			t.Errorf("detectQueryForm(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestFormatTerm(t *testing.T) {
	cases := []struct {
		name, in, out string
	}{
		{"plain IRI", "urn:product:1", "<urn:product:1>"},
		{"already bracketed", "<urn:wrapped>", "<urn:wrapped>"},
		{"already quoted literal", `"already a literal"`, `"already a literal"`},
		{"blank node", "_:b1", "_:b1"},
		{"empty becomes empty literal", "", `""`},
		// Defensive: a bare value starting with `_` but not `_:` falls through
		// to the IRI branch; the wrapper must still produce a syntactically
		// valid IRIREF, not be mistaken for a blank node.
		{"underscore prefix is not a blank node", "_internal", "<_internal>"},
		// SPARQL injection defense: characters forbidden in an IRIREF
		// (RFC 3987 / SPARQL 1.1 grammar) must be percent-encoded so an
		// adversarial @id can't break out of <...>.
		{"injects > escaped", "http://x/a>b", "<http://x/a%3Eb>"},
		{"injects < escaped", "http://x/a<b", "<http://x/a%3Cb>"},
		{"injects quote escaped", `http://x/"q"`, "<http://x/%22q%22>"},
		{"injects space escaped", "http://x/a b", "<http://x/a%20b>"},
		{"injects newline escaped", "http://x/a\nb", "<http://x/a%0Ab>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTerm(tc.in); got != tc.out {
				t.Errorf("formatTerm(%q) = %q, want %q", tc.in, got, tc.out)
			}
		})
	}
}

func TestParseNTriples_HandlesEdgeCases(t *testing.T) {
	body := []byte(
		// blank-node subject
		"_:b0 <https://schema.org/name> \"Anon\" .\n" +
			// language-tagged literal
			"<urn:product:1> <https://schema.org/name> \"hello\"@en .\n" +
			// escaped quote inside literal
			"<urn:product:1> <https://schema.org/description> \"He said \\\"hi\\\"\" .\n" +
			// comment line
			"# this is a comment\n" +
			// blank line
			"\n" +
			// malformed line
			"this is not a triple\n",
	)
	triples, skipped, sample := parseNTriples(body)
	if len(triples) != 3 {
		t.Fatalf("got %d triples, want 3 (blank-node + lang + escaped-quote)", len(triples))
	}
	if triples[0].Subject != "_:b0" {
		t.Errorf("blank-node subject not preserved: %q", triples[0].Subject)
	}
	if triples[1].Object != `"hello"@en` {
		t.Errorf("language tag dropped: %q", triples[1].Object)
	}
	if triples[2].Object != `"He said \"hi\""` {
		t.Errorf("escaped quote handling wrong: %q", triples[2].Object)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (the malformed line)", skipped)
	}
	if sample == "" {
		t.Error("expected a non-empty sample of the skipped line")
	}
}

func TestStore_IsEmpty_NilBooleanReturnsError(t *testing.T) {
	fake := newFakeOxigraph(t)
	// Body has no `boolean` field — simulates an Oxigraph quirk or proxy
	// stripping the field. We treat it as a protocol error so the backfill
	// doesn't silently rebuild a populated graph.
	fake.replyWith("/query", http.StatusOK, mimeResultsJSON, `{"head":{}}`)
	store := newTestStore(t, fake.server.URL)

	_, err := store.IsEmpty(context.Background())
	if err == nil {
		t.Fatal("expected error on missing boolean field; got nil")
	}
}
