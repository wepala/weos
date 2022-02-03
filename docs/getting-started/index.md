---
layout: default
title: Getting Started
nav_order: 2
has_children: true
---
# Getting Started

There are a couple of ways to get started with WeOS. 
1. [Use the command line application](#using-the-weos-cli)
2. Create your own binary

## Setup OpenAPI Specification
WeOS takes a design first approach with the [OpenAPI specifications](https://openapis.org) at it's core. For simple APIs that are basic create, read, update, delete functionality
(CRUD). You can create a vanilla OpenAPI specification using [OpenAPI schemas](https://swagger.io/docs/specification/data-models/) to model your data. We also provide extensions
for adding controllers and middleware to endpoints. See our [specification documentation](./specification.md) to get the complete list of
functionality available. You can also use one of [our examples](../examples) as a starting point.

## Using the WeOS CLI
Using the CLI is by far the easiest starting point. Download and run the CLI by pointing it to your OpenAPI spec.
By default, the API will run on port 8681 (you can configure this using the `--port` switch), and it will try to use a
specification file named `api.yaml` (you can specify this using the `--spec` switch).

The WeOS CLI is essentially a server that uses the OpenAPI specification for configuration. We chose to build the
server with Go because we wanted to make the server extensible, easy to deploy and maintain with no serverside runtime required.
You can download a binary for your environment on [our release page](https://github.com/wepala/weos/releases).

## Creating You Own Binary
While the CLI helps you get started quickly with CRUD, if you want extend the functionality beyond the basics we got you
covered. We've architected
