---
title: Explanation
layout: default
nav_order: 3
---

# Explanation

Explanation pages are **understanding-oriented** — they answer "why" and "how it works" rather than "how to do X." Read these when you want to build a mental model of how WeOS is designed.

| Topic | What it covers |
|-------|---------------|
| [RDF and the Ontology]({% link _explanation/rdf-and-ontology.md %}) | Why content is modeled as linked data using Schema.org, FOAF, and other vocabularies |
| [Atomic Models and Triples]({% link _explanation/atomic-models-and-triples.md %}) | How resources are atomic units linked by RDF triples |
| [Event Store]({% link _explanation/event-store.md %}) | Event sourcing fundamentals, the Pericarp library, and the Unit of Work pattern |
| [Projections]({% link _explanation/projections.md %}) | How events become queryable SQL tables via the ProjectionManager |
| [MCP Protocol]({% link _explanation/mcp-protocol.md %}) | How the Model Context Protocol makes WeOS LLM-agnostic |
| [Architecture]({% link _explanation/architecture.md %}) | Clean Architecture layers, dependency injection, and the request lifecycle |
