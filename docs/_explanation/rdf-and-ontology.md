---
title: RDF and the Ontology
parent: Explanation
layout: default
nav_order: 1
---

# RDF and the Ontology

WeOS models all content as **linked data** using the Resource Description Framework (RDF). Every resource type carries a JSON-LD context that maps its properties to well-known vocabularies. This design serves three purposes: it gives LLMs a grounded understanding of your content, it produces structured data that search engines consume for SEO, and it creates a shared language across different resource types.

## Why RDF?

Most CMSs store content as opaque blobs — a "product" is just a row with a `title` column. The CMS knows the column name, but it doesn't know what "title" *means*. An LLM editing that content has to guess based on column names and conventions.

With RDF, every property has a precise definition. When a WeOS resource type declares `"@vocab": "https://schema.org/"`, the property `name` isn't just a string column — it's `schema:name`, defined by Schema.org as "the name of the item." An LLM that understands Schema.org (and all major LLMs do) can reason about the content accurately.

## JSON-LD Contexts

Every resource type in WeOS has a JSON-LD `@context` that maps property names to vocabulary IRIs. Here's the context from the core `Person` type:

```json
{
  "@vocab": "https://schema.org/",
  "foaf": "http://xmlns.com/foaf/0.1/"
}
```

This means:
- `@vocab` sets Schema.org as the default vocabulary — unqualified property names like `name`, `email`, `givenName` resolve to `schema:name`, `schema:email`, `schema:givenName`
- The `foaf` prefix makes FOAF (Friend of a Friend) vocabulary available — you could use `foaf:knows` to express social connections

The `@type` field declares what *kind* of thing this resource is:

```json
{
  "@vocab": "https://schema.org/",
  "@type": "Product"
}
```

This tells both LLMs and search engines: "this resource is a Schema.org Product."

## Vocabularies Used

WeOS draws from established vocabularies, choosing the best fit for each domain:

| Vocabulary | Prefix | Used for |
|-----------|--------|----------|
| [Schema.org](https://schema.org) | `schema:` | General-purpose: products, articles, events, persons, organizations |
| [FOAF](http://xmlns.com/foaf/0.1/) | `foaf:` | People and social connections |
| [vCard](https://www.w3.org/2006/vcard/ns) | `vcard:` | Contact information |
| [W3C ORG](http://www.w3.org/ns/org#) | `org:` | Organizations, memberships, roles |
| [Activity Streams 2.0](https://www.w3.org/ns/activitystreams) | `as:` | Social activities and feeds |
| [GoodRelations](http://purl.org/goodrelations/v1#) | `gr:` | E-commerce: offers, prices, availability |
| [PROV-O](http://www.w3.org/ns/prov#) | `prov:` | Provenance and audit trails |
| [SKOS](http://www.w3.org/2004/02/skos/core#) | `skos:` | Knowledge organization: concepts, taxonomies |

## How This Benefits LLMs

When an LLM connects to WeOS via MCP, it doesn't just see column names — it sees semantic types. A resource with `@type: "Product"` and properties `name`, `price`, `sku` gives the LLM enough context to:

1. **Understand intent** — "add a new product" maps directly to creating a resource of type Product
2. **Infer relationships** — a Product can have Offers (GoodRelations), Reviews (Schema.org), and a brand (Schema.org)
3. **Generate valid data** — the LLM knows `price` should be a number and `sku` should be a string identifier
4. **Produce structured output** — the JSON-LD data is already valid structured data for Google, Bing, and other consumers

## How This Benefits SEO

JSON-LD is Google's recommended format for structured data. Because WeOS stores content as JSON-LD natively, your content is already in the format search engines expect. A blog post with `@type: "BlogPosting"` and properties `headline`, `datePublished`, `author` can be served directly as a `<script type="application/ld+json">` block in the HTML — no transformation needed.

## The @graph Format

When a resource has relationships (triples), WeOS stores the data in JSON-LD `@graph` format:

```json
{
  "@context": {"@vocab": "https://schema.org/"},
  "@graph": [
    {
      "@id": "urn:task:abc123",
      "@type": "Action",
      "name": "Design landing page",
      "status": "open",
      "priority": "medium"
    },
    {
      "project": "urn:project:xyz789"
    }
  ]
}
```

The first node (index 0) contains the entity's own properties. The second node (index 1) contains references to other resources (edges). This separation keeps the entity data clean while preserving relationship information.

See [Atomic Models and Triples]({% link _explanation/atomic-models-and-triples.md %}) for more on how relationships work.

## Further Reading

- [Atomic Models and Triples]({% link _explanation/atomic-models-and-triples.md %}) — how resources link to each other
- [Projections]({% link _explanation/projections.md %}) — how JSON-LD data becomes queryable SQL columns
- [Preset Catalog]({% link _reference/preset-catalog.md %}) — see the JSON-LD contexts for all built-in types
