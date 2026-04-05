---
title: Home
layout: default
nav_order: 0
---

# WeOS Documentation

WeOS is an open source website system where your AI is the webmaster. Users describe what they want in natural language and the site updates accordingly. WeOS never calls any LLM directly — it exposes an [MCP server]({% link _explanation/mcp-protocol.md %}) that any MCP-compatible LLM (Claude, GPT, Gemini, Ollama) connects to for driving edits. Output is static-first HTML.

The Go binary does three things:

1. **Generates and serves the static site** — templates + content = HTML
2. **Runs the MCP server** — LLM-driven edits via Model Context Protocol
3. **Serves as the API backend** — REST API for programmatic access

---

## Where to Start

### Building a site with WeOS?

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
