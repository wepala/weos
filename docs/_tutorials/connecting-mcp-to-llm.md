---
title: Connecting MCP to an LLM
parent: Tutorials
layout: default
nav_order: 5
---

# Connecting MCP to an LLM

The Model Context Protocol (MCP) is what makes WeOS AI-native. WeOS exposes an MCP server that any compatible LLM client can connect to — the LLM discovers available tools (resource types, resources, persons, organizations) and uses them to manage your site through natural language.

In this tutorial you'll start the MCP server, connect it to Claude Desktop, and make your first AI-driven edit.

## Prerequisites

- WeOS built (see [Running WeOS]({% link _tutorials/running-weos.md %}))
- Some resource types installed (e.g., `./bin/weos resource-type preset install tasks`)
- An MCP-compatible LLM client. This tutorial uses [Claude Desktop](https://claude.ai/download), but the protocol works with any compatible client.

## Step 1: Understand the MCP Server

WeOS's MCP server uses **stdio transport** — it communicates via standard input/output. The LLM client launches the `weos mcp` process and communicates with it through stdin/stdout.

The server exposes four tool groups:
- **person** — create, get, list, update, delete persons
- **organization** — create, get, list, update, delete organizations
- **resource-type** — create, get, list, update, delete resource types; list and install presets
- **resource** — create, get, list, update, delete resources of any type

You can enable only specific tool groups:

```bash
# All tools (default)
./bin/weos mcp

# Only person and organization tools
./bin/weos mcp --services person --services organization

# Only resource management
./bin/weos mcp --services resource --services resource-type
```

## Step 2: Configure Claude Desktop

Open your Claude Desktop configuration file:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

Add the WeOS MCP server:

```json
{
  "mcpServers": {
    "weos": {
      "command": "/path/to/weos",
      "args": ["mcp"],
      "env": {
        "DATABASE_DSN": "/path/to/your/weos.db"
      }
    }
  }
}
```

Replace `/path/to/weos` with the absolute path to your built binary (e.g., `/Users/you/weos/bin/weos`).

The `DATABASE_DSN` environment variable tells the MCP server which database to use. Point it at the same database your `weos serve` instance uses.

**Restart Claude Desktop** after saving the configuration.

## Step 3: Verify the Connection

In Claude Desktop, you should see a tools icon indicating MCP servers are connected. Click it to see the available tools from WeOS.

You should see tools like:
- `resource_type_list` — list resource types
- `resource_type_preset_list` — list available presets
- `resource_type_preset_install` — install a preset
- `resource_create` — create a resource
- `resource_list` — list resources
- `person_create` — create a person
- And more...

## Step 4: Make Your First AI-Driven Edit

Try these natural language prompts in Claude Desktop:

### List available content types

> "What resource types are available in my WeOS site?"

Claude will call `resource_type_list` and show you the installed types.

### Install a preset

> "Install the website preset so I can manage web pages and blog posts."

Claude will call `resource_type_preset_install` with `name: "website"`.

### Create content

> "Create a blog post called 'Getting Started with WeOS' with a short introduction about how WeOS lets AI manage your website."

Claude will call `resource_create` with `type_slug: "blog-post"` and appropriate data.

### Manage people

> "Add a person named Jane Smith with email jane@example.com."

Claude will call `person_create` with the given details.

### Organize content

> "List all tasks and show me which ones are high priority."

Claude will call `resource_list` with `type_slug: "task"` and present the results.

## Step 5: Use With Other LLM Clients

WeOS's MCP server works with any MCP-compatible client, not just Claude. The protocol is the same — stdio transport with JSON-RPC messages.

### Generic MCP client configuration

Any MCP client that supports stdio transport can connect:

```json
{
  "command": "/path/to/weos",
  "args": ["mcp"],
  "env": {
    "DATABASE_DSN": "weos.db"
  }
}
```

### Selective tool exposure

For security or simplicity, you can limit which tools are available:

```json
{
  "command": "/path/to/weos",
  "args": ["mcp", "--services", "resource", "--services", "resource-type"]
}
```

This exposes only resource and resource-type management tools, hiding person and organization tools.

You can also set this via the `MCP_SERVICES` environment variable:

```json
{
  "command": "/path/to/weos",
  "args": ["mcp"],
  "env": {
    "MCP_SERVICES": "resource,resource-type"
  }
}
```

## Available MCP Tools

| Tool | Input | Description |
|------|-------|-------------|
| `person_create` | given_name, family_name, email | Create a person |
| `person_get` | id | Get a person by ID |
| `person_list` | cursor?, limit? | List persons |
| `person_update` | id, given_name, family_name, email?, avatar_url?, status? | Update a person |
| `person_delete` | id | Delete a person |
| `organization_create` | name, slug | Create an organization |
| `organization_get` | id | Get an organization |
| `organization_list` | cursor?, limit? | List organizations |
| `organization_update` | id, name, slug?, description?, url?, logo_url?, status? | Update an organization |
| `organization_delete` | id | Delete an organization |
| `resource_type_create` | name, slug, description?, context?, schema? | Create a resource type |
| `resource_type_get` | id | Get a resource type |
| `resource_type_list` | cursor?, limit?, includeAll? | List resource types |
| `resource_type_update` | id, name, slug?, description?, context?, schema?, status? | Update a resource type |
| `resource_type_delete` | id | Delete a resource type |
| `resource_type_preset_list` | (none) | List available presets |
| `resource_type_preset_install` | name, update? | Install a preset |
| `resource_create` | type_slug, data | Create a resource |
| `resource_get` | id | Get a resource |
| `resource_list` | type_slug, cursor?, limit?, sort_by?, sort_order? | List resources |
| `resource_update` | id, data | Update a resource |
| `resource_delete` | id | Delete a resource |

See [MCP Tools Reference]({% link _reference/mcp-tools.md %}) for complete input/output schemas.

## What You've Learned

- How the MCP server uses stdio transport
- How to configure Claude Desktop to connect to WeOS
- How to make AI-driven edits through natural language
- How to limit tool exposure with `--services`
- How to use WeOS with any MCP-compatible LLM client

## What's Next

- [MCP Protocol]({% link _explanation/mcp-protocol.md %}) — understand how MCP works under the hood
- [MCP Tools Reference]({% link _reference/mcp-tools.md %}) — complete tool schemas
- [CLI Commands]({% link _reference/cli.md %}) — the `weos mcp` command reference
