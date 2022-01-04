---
layout: default
title: Controllers
parent: Getting Started
nav_order: 3
has_children: false
---
# Controllers
Controllers map requests to models as well as retrieves information through projections. WeOS provides standard
controllers for everyday actions, e.g., Create, Read, Update, Delete (CRUD).

WeOS automatically binds controllers to endpoints that don't have one already. The controller is automatically attached
based on the HTTP method, request body, and response info.

## Create
The Create controller will create an item of the content type associated with the controller's endpoint.

## Read
There are two controllers for reading data from WeOS.
1. Get - Get a single item
2. List - Get a collection of items

### Get
The Get controller retrieves an item using the identifier of the associated content type. The developer defines an
endpoint with the identifier declared as a parameter to use the Get controller. The Get controller retrieves the
identifier from the request context and uses the projection to get the information from the database.

### List
The List controller returns a paginated collection of items based on filters and sorts. The controller uses the filters
declared in the context to retrieve information using a projection.

### Update
There are two