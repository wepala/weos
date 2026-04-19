---
title: MCP Protocol
parent: Explanation
layout: default
nav_order: 5
---

# MCP Protocol

The **Model Context Protocol** (MCP) is what makes WeOS AI-native without being tied to any specific LLM. Instead of calling an LLM API directly, WeOS exposes an MCP server that any MCP-compatible LLM client can connect to. The LLM discovers available tools, understands their schemas, and uses them to manage your site.

## Why MCP?

Most AI-powered tools embed LLM calls directly in their codebase. This creates vendor lock-in — you're stuck with whichever LLM the developer chose. When a better model comes along, you wait for the developer to integrate it.

WeOS takes the opposite approach: **the LLM connects to WeOS, not the other way around.** WeOS exposes its capabilities as MCP tools, and any MCP-compatible client (Claude, GPT, Gemini, Ollama, or custom agents) can use them.

This means:
- **Zero LLM calls in the codebase** — WeOS never imports an LLM SDK
- **Instant compatibility** — any new MCP client works with WeOS immediately
- **User choice** — use whichever LLM you prefer, switch anytime
- **Offline capable** — WeOS works without any LLM; the MCP server is optional

## How It Works

### Transport: stdio

WeOS's MCP server uses **stdio transport** — it communicates via standard input and output using JSON-RPC 2.0 messages. The LLM client launches `weos mcp` as a subprocess and exchanges messages through stdin/stdout.

```
LLM Client (Claude Desktop, etc.)
    │
    ├── stdin  ──→  weos mcp process
    └── stdout ←──  weos mcp process
```

This is the simplest MCP transport — no HTTP server, no ports, no authentication. The LLM client manages the process lifecycle.

### Tool Discovery

When the LLM client connects, it calls `tools/list` to discover available tools. WeOS returns a list of tool definitions with:
- **Name** — a unique identifier (e.g., `resource_create`)
- **Description** — what the tool does
- **Input schema** — a JSON Schema describing the required and optional parameters

The LLM uses these schemas to understand what data each tool needs and validates its output before calling.

### Tool Invocation

When the LLM decides to use a tool, it sends a `tools/call` request with the tool name and arguments. WeOS executes the operation (same code path as the REST API) and returns the result.

```
User: "Create a blog post called Hello World"
  │
  └─→ LLM: calls resource_create {type_slug: "blog-post", data: {headline: "Hello World", ...}}
       │
       └─→ WeOS: creates the resource, returns the result
            │
            └─→ LLM: "I've created the blog post 'Hello World'."
```

## Tool Groups

WeOS organizes MCP tools into four service groups:

| Service | Tools | Purpose |
|---------|-------|---------|
| `person` | person_create, person_get, person_list, person_update, person_delete | Manage people (FOAF/Schema.org Person) |
| `organization` | organization_create, organization_get, organization_list, organization_update, organization_delete | Manage organizations (W3C ORG) |
| `resource-type` | resource_type_create, resource_type_get, resource_type_list, resource_type_update, resource_type_delete, resource_type_preset_list, resource_type_preset_install | Manage content type definitions and presets |
| `resource` | resource_create, resource_get, resource_list, resource_update, resource_delete | CRUD for any resource type |

### Selective Exposure

You can limit which tool groups are available using the `--services` flag or the `MCP_SERVICES` environment variable:

```bash
# Only expose resource management tools
weos mcp --services resource --services resource-type

# Via environment variable
MCP_SERVICES=resource,resource-type weos mcp
```

This is useful for:
- **Security** — limit what the LLM can do
- **Focus** — reduce the tool list so the LLM focuses on relevant operations
- **Multi-agent setups** — different agents get different tool sets

By default (no `--services` flag), all four groups are enabled.

## Shared Code Path

MCP tools delegate to the same application services as the REST API handlers. A `resource_create` MCP call goes through the same `ResourceService.Create()` method as a `POST /api/:typeSlug` HTTP request. This means:

- Same validation rules
- Same event sourcing
- Same projection updates
- Same authorization (when configured)

The MCP server is a thin adapter layer — it translates MCP protocol messages into service method calls and formats the results.

## Ontology-Grounded Reasoning

Because every resource type carries a JSON-LD context with Schema.org types and properties, the LLM has rich semantic context for reasoning about your content. When it sees a tool that operates on resources with `@type: "Product"` and properties like `name`, `price`, `sku`, it understands these aren't arbitrary fields — they're well-defined concepts from Schema.org.

This grounding reduces hallucination and improves the quality of LLM-generated content.

## Further Reading

- [Connecting MCP to an LLM]({% link _tutorials/connecting-mcp-to-llm.md %}) — hands-on tutorial
- [MCP Tools Reference]({% link _reference/mcp-tools.md %}) — complete tool schemas
- [RDF and the Ontology]({% link _explanation/rdf-and-ontology.md %}) — how ontologies ground LLM reasoning
