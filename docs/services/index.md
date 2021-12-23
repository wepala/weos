---
layout: default
title: Home
nav_order: 1
has_children: false
---
# WeOS Service

Services are the foundation of WeOS. Services are Application Programming Interfaces (APIs) that provide pre-built
functionality for developers to use. We created "WeOS Service" to make APIs easier to build and maintain. WeOS Service
is a Go application paired with an [OpenAPI specification](https://spec.openapis.org/oas/latest.html) to get a fully
functional API in minutes quickly. You can also
use WeOS Service to make complex APIs that are easy to scale and maintain.

## Quick Start
The easiest way to get started is to make a content API that uses the WeOS service binary and an OpenAPI specification.
To get started:
1. Define content types in the API spec file.
2. Define the endpoints for interacting with content types
3. Run the API

### Minimum Requirements
1. Executable for your platform
2. Text editor for creating API specification

### Define Content Types

For any WeOS service, you can define schemas for data used in the service. WeOS Service uses those schema
definitions (Content Types) to set up CRUD functionality and essential data stores.

These schema definitions are standard [OpenAPI objects](https://swagger.io/docs/specification/data-models/data-types/#object)
that use [OpenAPI data types](https://swagger.io/docs/specification/data-models/data-types). Developers can create
relationships between Content Types using arrays and the "$ref" tag to reference other Content Types defined in the schema.

Learn more about modeling APIs with Content Types in the feature section

### Define Endpoints

You can create endpoints that sort, filter, and paginate the content returned. You can set up the endpoints to create,
delete, list, or view content.

Endpoints are created by creating [OpenAPI paths](https://swagger.io/docs/specification/paths-and-operations/) that
reference the Content Types created in the specification schema. The operation and request body help determine the
functionality associated with the endpoint.  You can also explicitly indicate a handler for a specific path.

### Deploy

You can run the API by executing a command on the WeOS Service and referencing the API specification you create.
One binary, one API spec, that's all you need. Deploy your service to WeOS and get a secure, easy to maintain API that
is ready to use

## What's Next
1. How Does WeOS Work?
2. Creating an OpenAPI Spec
3. Advanced API building
4. Deploying you API in WeOS.cloud