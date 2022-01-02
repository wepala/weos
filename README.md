<p align="center">
  <a href="https://weos.cloud">
    <img src="./docs/assets/images/logo.png#gh-light-mode-only" width="318px" alt="WeOS logo" />
  </a>
</p>

<h3 align="center">Open-Source API Platform for building Apps Quickly</h3>
<p align="center">Get started building your app using microservices that combine an <a href="https://spec.openapis.org/oas/latest.html">OpenAPI specification</a> and an
executable (no Docker required)</p>
<p align="center"><a href="https://weos.dev/demo">See Example App</a></p>
<br />


![Diagram that shows WeOS microservice using an OpenAPI spec and connected to a database!](./docs/assets/images/weos-microservice-layout.png "Basic WeOS microservice layout")

![GitHub branch checks state](https://img.shields.io/github/checks-status/wepala/weos/dev) ![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/wepala/weos)   
Check out the [example section](https://wepala.github.io/weos-service/examples) to get inspiration for the types of APIs and applications you can create with WeOS.

## Quick Start
1. Setup OpenAPI spec (you can use one from [our examples](https://wepala.github.io/weos-service/examples))
2. Download the WeOS binary for your platform
3. Run the API
4. Deploy to WeOS Cloud (optional)

## Setup OpenAPI Specification
OpenAPI specifications power WeOS services. For simple APIs that are basic create, read, update, delete functionality
(CRUD), you can generate a vanilla OpenAPI specification using schemas to model your data. We also provide extensions
for adding controllers and middleware to endpoints. See our specification documentation to get the complete list of
functionality available. You can also use one of our examples as a starting point.

## Download the WeOS binary
The WeOS binary is essentially a server that uses the OpenAPI specification for configuration. We chose to build the
server with Go because we wanted to make the server extensible, easy to deploy and maintain with no runtime required.
You can download a binary for your environment on our release page.

## Run the API
Now that you have a specification and the WeOS executable, you can run the API by using the `weos` command in the same
folder where the specification and binary are. By default, the API will run on port 8681 (you can configure this using
the `--port` switch), and it will try to use a specification file named `api.yaml` (you can specify this using the
`--spec` switch). We set up the example APIs to use SQLite as the data store, but this too can be changed to use
Postgresql, MySQL, or SQLServer.

## Deploy to WeOS Cloud
We're doing all we can to make it easy to get started with microservices. In addition to providing the building blocks
needed to make excellent APIs, we also offer an environment to deploy and test them in. You can create an account on the
WeOS cloud to deploy your services. We also provide a catalog of APIs already running in the cloud so that developers
can focus on making the user the interface for their application

## What's Next
1. How Does WeOS Work?
2. Creating an OpenAPI Spec
3. Advanced API building
4. Deploying you API in WeOS.cloud