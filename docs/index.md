---
layout: default
title: Home
nav_order: 1
has_children: false
---
# WeOS Service

You can use the Content Service to manage content in an application. You can have a fully functional API by simply modeling your data in an Open API specification.

## Quick Start
1. Define content types in the API spec file.
2. Define the endpoints for interacting with content types
3. Run the API

### Define Content Types

For any WeOS service, you can define schemas for data used in the service. The Content Service uses those schema definitions (Content Types) to set up CRUD functionality and essential data stores.

These schema definitions are standard [OpenAPI objects](https://swagger.io/docs/specification/data-models/data-types/#object) that use [OpenAPI data types](https://swagger.io/docs/specification/data-models/data-types). Developers can create relationships between Content Types using arrays and the "$ref" tag to reference other Content Types defined in the schema.

Learn more about modeling APIs with Content Types in the feature section

### Define Endpoints

You can create endpoints that sort, filter, and paginate the content returned. You can set up the endpoints to create, delete, list, or view content.

Endpoints are created by creating [OpenAPI paths](https://swagger.io/docs/specification/paths-and-operations/) that reference the Content Types created in the specification schema. The operation and request body help determine the functionality associated with the endpoint.  You can also explicitly indicate a handler for a specific path.

### Deploy

You can run the API by executing a command on the Content Service and referencing the API specification you create. One binary, one API spec, that's all you need. Deploy your content service to WeOS and get a secure, easy to maintain API that is ready to use

## Whats Next
1. Passing Parameters