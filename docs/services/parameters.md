---
layout: default
title: Parameters
nav_order: 2
has_children: false
---
# Passing Parameters
The content service allows developers to declare parameters and bind them to content type properties through a request
context. Parameter binding will enable developers to create an expressive API that uses all that OpenAPI offers.

## Request Context

Each Request has a context. Content Service controllers (handlers) extract the information it needs for its functionality
from the context. As an example, if a developer defines a content type _Blog_ with the default identifier **id**, the _Get_
handler expects a property `Blog.id` in the context. The handler uses the `Blog.id` to get the blog and return it to the user.

## Parameter Binding

Content service will automatically bind parameters to the content type's properties associated with the endpoint
(assuming they are named the same). If there is an endpoint for our Blog content type and there is an API parameter "id"
defined, then the content service will automatically map the parameter **id** to `Blog.id` in the context.

If the parameter is not named the same as the property on the content type, then the `x-property` attribute can link the
parameter to a content type property.

```yaml
/blogs/{someID}:
    get:
      parameters:
        - in: path
          name: someID
          schema:
            type: string
          required: true
          description: blog id
          x-property: Blog.id
```

The Content Service does not add information to the context NOT declared in the API specification.