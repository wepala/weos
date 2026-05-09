package mcp

import (
	"context"
	"errors"

	"github.com/wepala/weos/v3/application"
	"github.com/wepala/weos/v3/domain/repositories"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// kg_tools.go registers the `knowledge-graph` MCP tool group. The tools are
// intentionally generic — SPARQL execute, neighborhood expand, label search,
// class describe, path-find — so any new resource type works without an MCP
// code change. SPARQL composition lives in application.KnowledgeGraphService;
// this file is a thin shape adapter for MCP transport.
//
// HTTP-transport note: when this server is mounted as an http.Handler, the
// existing Echo auth middleware that populates auth.AgentFromCtx flows through
// to the tool ctx unchanged. The permission filter (story #357) reads the
// agent from ctx and applies it to every result before it leaves the server.

// --- Input/output shapes ---

type KGSparqlQueryInput struct {
	Query string `json:"query" jsonschema:"a SPARQL 1.1 SELECT/ASK/CONSTRUCT/DESCRIBE query"`
}

type KGTermOutput struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Datatype string `json:"datatype,omitempty"`
	Lang     string `json:"lang,omitempty"`
}

type KGTripleOutput struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

type KGSparqlQueryOutput struct {
	// Form is one of "select", "ask", "construct" — the LLM uses this to
	// know which of the result fields to read.
	Form     string                  `json:"form"`
	Vars     []string                `json:"vars,omitempty"`
	Bindings []map[string]KGTermOutput `json:"bindings,omitempty"`
	Boolean  *bool                   `json:"boolean,omitempty"`
	Triples  []KGTripleOutput        `json:"triples,omitempty"`
}

type KGExpandEntityInput struct {
	IRI   string `json:"iri" jsonschema:"the IRI to expand (e.g. urn:product:abc, https://schema.org/Person)"`
	Depth int    `json:"depth,omitempty" jsonschema:"hops to walk; 1-3, defaults to 1"`
}

type KGExpandEntityOutput struct {
	IRI     string           `json:"iri"`
	Triples []KGTripleOutput `json:"triples"`
}

type KGSearchEntitiesInput struct {
	Q        string `json:"q" jsonschema:"case-insensitive substring matched against rdfs:label / schema:name / foaf:name / dcterms:title"`
	ClassIRI string `json:"class_iri,omitempty" jsonschema:"optional class IRI to restrict results to instances of that class (and subclasses)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max IRIs to return; 1-100, defaults to 20"`
}

type KGSearchEntitiesOutput struct {
	Matches []KGTermOutput `json:"matches"`
}

type KGDescribeClassInput struct {
	ClassIRI        string `json:"class_iri" jsonschema:"the class IRI to describe (e.g. https://schema.org/Person)"`
	SampleInstances int    `json:"sample_instances,omitempty" jsonschema:"how many example instances to include; 0-50, defaults to 0"`
}

type KGDescribeClassOutput struct {
	ClassIRI   string         `json:"class_iri"`
	Predicates []string       `json:"predicates"`
	Instances  []KGTermOutput `json:"instances,omitempty"`
}

type KGListClassesInput struct{}

type KGListClassesOutput struct {
	Classes []KGTermOutput `json:"classes"`
}

type KGFindPathInput struct {
	From    string `json:"from" jsonschema:"starting IRI"`
	To      string `json:"to" jsonschema:"target IRI"`
	MaxHops int    `json:"max_hops,omitempty" jsonschema:"path-length budget; 1-6, defaults to 4"`
}

type KGFindPathOutput struct {
	From    string           `json:"from"`
	To      string           `json:"to"`
	Triples []KGTripleOutput `json:"triples"`
}

// --- Conversion helpers ---

func toKGTermOutput(t repositories.KGTerm) KGTermOutput {
	return KGTermOutput{
		Type:     string(t.Type),
		Value:    t.Value,
		Datatype: t.Datatype,
		Lang:     t.Lang,
	}
}

func toKGTripleOutput(t repositories.Triple) KGTripleOutput {
	return KGTripleOutput{Subject: t.Subject, Predicate: t.Predicate, Object: t.Object}
}

func toKGTripleOutputs(ts []repositories.Triple) []KGTripleOutput {
	if len(ts) == 0 {
		return nil
	}
	out := make([]KGTripleOutput, 0, len(ts))
	for _, t := range ts {
		out = append(out, toKGTripleOutput(t))
	}
	return out
}

// resultForm reports which SPARQL form a result represents so the JSON output
// carries an explicit tag for the LLM to branch on, instead of nil-checking
// three independent fields.
func resultForm(r repositories.KGQueryResult) string {
	if r.Boolean != nil {
		return "ask"
	}
	if r.Triples != nil {
		return "construct"
	}
	return "select"
}

// --- Registration ---

func registerKnowledgeGraphTools(server *mcp.Server, svc application.KnowledgeGraphService) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "kg_sparql_query",
		Description: "Execute a SPARQL 1.1 query (SELECT/ASK/CONSTRUCT/DESCRIBE) against the " +
			"knowledge graph. Use kg_expand_entity or kg_search_entities for common patterns; " +
			"reach for this tool when those don't fit.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input KGSparqlQueryInput,
	) (*mcp.CallToolResult, KGSparqlQueryOutput, error) {
		res, err := svc.Query(ctx, input.Query)
		if err != nil {
			return nil, KGSparqlQueryOutput{}, kgErr(err)
		}
		out := KGSparqlQueryOutput{Form: resultForm(res), Vars: res.Vars, Boolean: res.Boolean}
		for _, row := range res.Bindings {
			converted := make(map[string]KGTermOutput, len(row))
			for k, v := range row {
				converted[k] = toKGTermOutput(v)
			}
			out.Bindings = append(out.Bindings, converted)
		}
		out.Triples = toKGTripleOutputs(res.Triples)
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "kg_expand_entity",
		Description: "Return the one-hop (or multi-hop) neighborhood of an entity as triples. " +
			"Use to learn what predicates and connected entities exist for a known IRI.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input KGExpandEntityInput,
	) (*mcp.CallToolResult, KGExpandEntityOutput, error) {
		triples, err := svc.ExpandEntity(ctx, input.IRI, input.Depth)
		if err != nil {
			return nil, KGExpandEntityOutput{}, kgErr(err)
		}
		return nil, KGExpandEntityOutput{IRI: input.IRI, Triples: toKGTripleOutputs(triples)}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "kg_search_entities",
		Description: "Find entities by name. Substring-matches against rdfs:label, schema:name, " +
			"foaf:name, and dcterms:title (case-insensitive). Returns IRIs to feed into " +
			"kg_expand_entity or kg_describe_class.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input KGSearchEntitiesInput,
	) (*mcp.CallToolResult, KGSearchEntitiesOutput, error) {
		matches, err := svc.SearchEntities(ctx, input.Q, input.ClassIRI, input.Limit)
		if err != nil {
			return nil, KGSearchEntitiesOutput{}, kgErr(err)
		}
		out := KGSearchEntitiesOutput{Matches: make([]KGTermOutput, 0, len(matches))}
		for _, m := range matches {
			out.Matches = append(out.Matches, toKGTermOutput(m))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "kg_describe_class",
		Description: "Describe a class IRI: the predicates its instances use, plus optional " +
			"example instances. Walks rdfs:subClassOf so subclasses are included automatically.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input KGDescribeClassInput,
	) (*mcp.CallToolResult, KGDescribeClassOutput, error) {
		desc, err := svc.DescribeClass(ctx, input.ClassIRI, input.SampleInstances)
		if err != nil {
			return nil, KGDescribeClassOutput{}, kgErr(err)
		}
		out := KGDescribeClassOutput{
			ClassIRI:   desc.ClassIRI,
			Predicates: desc.Predicates,
		}
		for _, t := range desc.Instances {
			out.Instances = append(out.Instances, toKGTermOutput(t))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "kg_list_classes",
		Description: "List every class IRI declared in the knowledge graph (anything typed " +
			"rdfs:Class or owl:Class). Useful for introspection — start here when you don't " +
			"know what kinds of things the graph contains.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, _ KGListClassesInput,
	) (*mcp.CallToolResult, KGListClassesOutput, error) {
		classes, err := svc.ListClasses(ctx)
		if err != nil {
			return nil, KGListClassesOutput{}, kgErr(err)
		}
		out := KGListClassesOutput{Classes: make([]KGTermOutput, 0, len(classes))}
		for _, c := range classes {
			out.Classes = append(out.Classes, toKGTermOutput(c))
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "kg_find_path",
		Description: "Find a path of triples connecting two IRIs within max_hops. Best-effort " +
			"(may not be the shortest path); empty triples = no path within budget.",
	}, func(
		ctx context.Context, _ *mcp.CallToolRequest, input KGFindPathInput,
	) (*mcp.CallToolResult, KGFindPathOutput, error) {
		triples, err := svc.FindPath(ctx, input.From, input.To, input.MaxHops)
		if err != nil {
			return nil, KGFindPathOutput{}, kgErr(err)
		}
		return nil, KGFindPathOutput{
			From: input.From, To: input.To, Triples: toKGTripleOutputs(triples),
		}, nil
	})
}

// kgErr surfaces the common ErrKGUnavailable case as a clear MCP-level
// message; other errors pass through untouched.
func kgErr(err error) error {
	if errors.Is(err, application.ErrKGUnavailable) {
		return errors.New("knowledge graph not configured (set OXIGRAPH_URL to enable)")
	}
	return err
}
