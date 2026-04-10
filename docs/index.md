---
title: Home
layout: default
nav_order: 0
---

# WeOS Documentation

WeOS is an open source Go application for building a **digital twin** of yourself or your business — a knowledge graph of the information from the apps and devices you use, exposed to any LLM so it can answer with your real context. By default WeOS is MCP-first: it exposes an [MCP server]({% link _explanation/mcp-protocol.md %}) that any MCP-compatible LLM (Claude, GPT, Gemini, Ollama) connects to. Optional built-in agent integrations (e.g. Google ADK/Gemini) are available when configured.

The Go binary does three things:

1. **Stores your data as a knowledge graph** — resources are represented as JSON-LD entities, and relationships between them are modeled as RDF triples using ontologies like Schema.org and FOAF, so people, events, products, places, messages and the relationships between them are first-class.
2. **Runs the MCP server** — any MCP-compatible LLM connects and queries your graph for grounded, context-rich responses.
3. **Optionally renders sites and APIs** — the same graph can drive a static-first HTML site or a REST API when you want to publish or integrate.

---

## Where to Start

### Setting up your digital twin?

Start with the **[Tutorials]({% link _tutorials/index.md %})** — they walk you through running WeOS, connecting an LLM, creating content types, and customizing your site step by step.

### Contributing to WeOS?

Start with the **[Explanation]({% link _explanation/index.md %})** section to understand the architecture — RDF ontologies, event sourcing, projections, and how MCP ties it all together. Then use the **[Reference]({% link _reference/index.md %})** for precise API and CLI details.

### Looking for a specific task?

The **[How-to Guides]({% link _howto/index.md %})** are goal-oriented recipes: install, configure, deploy, and manage resources.

---

## Quick Links

| Section | What you'll find |
|---------|-----------------|
| [Tutorials]({% link _tutorials/index.md %}) | Step-by-step learning paths |
| [How-to Guides]({% link _howto/index.md %}) | Task-oriented recipes |
| [Explanation]({% link _explanation/index.md %}) | Conceptual deep dives |
| [Reference]({% link _reference/index.md %}) | CLI, API, MCP, and config specs |
| [Architecture]({% link architecture/README.md %}) | Clean Architecture overview |
| [Decision Records]({% link decisions/index.md %}) | Architectural decisions and rationale |
