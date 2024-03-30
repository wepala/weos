---
layout: default
title: Model API content types
parent: Features
---
# Model API content types

As a developer you can define content types for your API. Content Types have properties that can be formatted and
  validated on submission. Relationships between content types can also be setup

## Background

**Given** a developer "Sojourner"  
**And** "Sojourner" has an account with id "1234"  
**And** "OpenAPI 3.0" is used to model the service  
**And** a content type "Category" modeled in the "OpenAPI 3.0" specification  

```
  Category:
    type: object
    properties:
      title:
        type: string
      description:
        type: string
```

## Scenarios

### Declare basic content type


A simple content type is one where the properties are primitive types. If there is no identifier specified one will
    be created by default

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Blog:
  type: object
  properties:
    title:
      type: string
      description: blog title
    description:
      type: string
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Blog" should be added to the projection  

| Field       | Comment    | Type         | Null  | Key | Default |
|:------------|:-----------|:-------------|:------|:----|:--------|
| id          |            | varchar(512) | false | PK  | NULL    |
| title       | blog title | varchar(512) | true  |     | NULL    |
| description |            | varchar(512) | true  |     | NULL    |

**And** a "Blog" entity configuration should be setup  

```mermaid
erDiagram
  Blog
  Blog {
    string id
    string title
    string description
  }
```

### Declare a content type with the identifier explicitly declared


Identifiers are used to configure primary keys in the projection. Multiple fields can be part of the identifiers

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Blog:
  type: object
  properties:
    guid:
      type: string
    title:
      type: string
    description:
      type: string
  x-identifier:
    - guid
    - title
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Blog" should be added to the projection  

| Field       | Comment    | Type         | Null  | Key | Default |
|:------------|:-----------|:-------------|:------|:----|:--------|
| guid        |            | varchar(512) | false | PK  | NULL    |
| title       | blog title | varchar(512) | false | PK  | NULL    |
| description |            | varchar(512) | true  |     | NULL    |

**And** a "Blog" entity configuration should be setup  

```mermaid
erDiagram
  Blog
  Blog {
    string guid
    string title
    string description
  }
```

### Declare a content type that has required fields


Required properies should be added to the `required` parameter as per the OpenAPI specification. Properties that are
    marked as identifiers don't need to be marked as `required`

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Blog:
  type: object
  properties:
    id:
      type: string
    title:
      type: string
    description:
      type: string
  required:
    - title
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Blog" should be added to the projection  

| Field       | Comment    | Type         | Null  | Key | Default |
|:------------|:-----------|:-------------|:------|:----|:--------|
| id          |            | varchar(512) | false | PK  | NULL    |
| title       | blog title | varchar(512) | false |     | NULL    |
| description |            | varchar(512) | true  |     | NULL    |

**And** a "Blog" entity configuration should be setup.  

### Declare content type that has a many to one relationship to another content type


Many to one relationships is determined by what a property is referencing. If the property of a Content Type is
    referencing a single other content type then many to one relationship is inferred.

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Blog:
  type: object
  properties:
    title:
      type: string
    description:
      type: string
```
**And** "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification  

```
Post:
  type: object
  properties:
    title:
      type: string
    description:
      type: string
    blog:
      $ref: "#/components/schemas/Blog"
    publishedDate:
      type: string
    views:
      type: integer
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Blog" should be added to the projection  

| Field       | Comment | Type         | Null  | Key | Default |
|:------------|:--------|:-------------|:------|:----|:--------|
| id          |         | varchar(512) | false | PK  | NULL    |
| title       |         | varchar(512) | false |     | NULL    |
| description |         | varchar(512) | true  |     | NULL    |

**And** a model "Post" should be added to the projection  

| Field       | Comment | Type         | Null  | Key | Default |
|:------------|:--------|:-------------|:------|:----|:--------|
| id          |         | varchar(512) | false | PK  | NULL    |
| title       |         | varchar(512) | false |     | NULL    |
| description |         | varchar(512) | true  |     | NULL    |
| blog        |         | varchar(512) | true  | FK  | NULL    |

**And** a "Blog" entity configuration should be setup.  

### Declare content type that has a many to many relationship to another content type

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Blog:
  type: object
  properties:
    title:
      type: string
    description:
      type: string
```
**And** "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification  

```
Post:
  type: object
  properties:
    title:
      type: string
    description:
      type: string
    publishedDate:
      type: string
    views:
      type: integer
    categories:
      type: array
      items:
        $ref: "#/components/schemas/Category"
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Post" should be added to the projection  

| Field       | Comment | Type         | Null  | Key | Default |
|:------------|:--------|:-------------|:------|:----|:--------|
| id          |         | varchar(512) | false | PK  | NULL    |
| title       |         | varchar(512) | true  |     | NULL    |
| description |         | varchar(512) | true  |     | NULL    |

**And** a model "PostCategory" should be added to the projection  

| Field       | Comment | Type         | Null  | Key | Default |
|:------------|:--------|:-------------|:------|:----|:--------|
| post_id     |         | varchar(512) | false | PK  | NULL    |
| category_id |         | varchar(512) | false | PK  | NULL    |

**And** a "Post" entity configuration should be setup  

```
erDiagram
  Blog ||--o{ Post : contains
  Blog {
    string id
    string title
    string description
  }
  Category ||--o{ Post : contains
  Post {
    string id
    string title
    string description
  }
```

### Use format to set granular types


Developers can use the `format` attribute to set the format of a property. This should be used to validate content
    using common formats (e.g. email)

**Given** "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification  

```
Post:
  type: object
  properties:
    id:
      type: string
      format: ksuid
    title:
      type: string
    description:
      type: string
    email:
      type: string
      format: email
    publishedDate:
      type: string
      format: date-time
    views:
      type: integer
    categories:
      type: array
      items:
        $ref: "#/components/schemas/Category"
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Post" should be added to the projection  

| Field         | Comment | Type         | Null  | Key | Default |
|:--------------|:--------|:-------------|:------|:----|:--------|
| id            |         | varchar(512) | false | PK  | NULL    |
| title         |         | varchar(512) | true  |     | NULL    |
| description   |         | varchar(512) | true  |     | NULL    |
| email         |         | varchar(512) | true  |     | NULL    |
| publishedDate |         | datetime     | true  |     | NULL    |

**And** a model "PostCategory" should be added to the projection  

| Field       | Comment | Type         | Null  | Key | Default |
|:------------|:--------|:-------------|:------|:----|:--------|
| post_id     |         | varchar(512) | false | PK  | NULL    |
| category_id |         | varchar(512) | false | PK  | NULL    |

**And** a "Post" entity configuration should be setup  

```
erDiagram
  Blog ||--o{ Post : contains
  Blog {
    string id
    string title
    string description
  }
  Category ||--o{ Post : contains
  Post {
    string id
    string title
    string description
    string email
    datetime publishedDate
  }
```

### Setup validation rules for content


Developers can also use Regex to define content validation for a field

**Given** "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification  

```
Post:
  type: object
  properties:
    id:
      type: string
      format: ksuid
    title:
      type: string
    description:
      type: string
    email:
      type: string
      pattern: '^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$'
    publishedDate:
      type: string
      format: date-time
    views:
      type: integer
    categories:
      type: array
      items:
        $ref: "#/components/schemas/Category"
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Post" should be added to the projection  

| Field         | Comment | Type         | Null  | Key | Default |
|:--------------|:--------|:-------------|:------|:----|:--------|
| id            |         | varchar(512) | false | PK  | NULL    |
| title         |         | varchar(512) | true  |     | NULL    |
| description   |         | varchar(512) | true  |     | NULL    |
| email         |         | varchar(512) | true  |     | NULL    |
| publishedDate |         | datetime     | true  |     | NULL    |
| views         |         | int          | true  |     | NULL    |

**And** a model "PostCategory" should be added to the projection  

| Field       | Comment | Type         | Null  | Key | Default |
|:------------|:--------|:-------------|:------|:----|:--------|
| post_id     |         | varchar(512) | false | PK  | NULL    |
| category_id |         | varchar(512) | false | PK  | NULL    |

**And** a "Post" entity configuration should be setup  

```
erDiagram
  Blog ||--o{ Post : contains
  Blog {
    string id
    string title
    string description
  }
  Category ||--o{ Post : contains
  Post {
    string id
    string title
    string description
    string email
    datetime publishedDate
    integer views
  }
```

### Setup a content type with an enumeration


A property can be defined with a list of possible options.

**Given** "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification  

```
Post:
  type: object
  properties:
    id:
      type: string
      format: ksuid
    title:
      type: string
    description:
      type: string
    status:
      type: string
      enum:
        - unpublished
        - published
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Post" should be added to the projection  

| Field       | Comment | Type         | Null  | Key | Default     |
|:------------|:--------|:-------------|:------|:----|:------------|
| id          |         | varchar(512) | false | PK  | NULL        |
| title       |         | varchar(512) | true  |     | NULL        |
| description |         | varchar(512) | true  |     | NULL        |
| status      |         | varchar(512) | false |     | unpublished |

**And** a "Post" entity configuration should be setup  

```
erDiagram
  Blog ||--o{ Post : contains
  Blog {
    string id
    string title
    string description
  }
  Category ||--o{ Post : contains
  Post {
    string id
    string title
    string description
    string status
  }
```

### Setup a content type with an enumeration that is nullable


A property with a list of options can have a null option though it needs to be explicitly identified

**Given** "Sojourner" adds a schema "Post" to the "OpenAPI 3.0" specification  

```
Post:
  type: object
  properties:
    id:
      type: string
      format: ksuid
    title:
      type: string
    description:
      type: string
    status:
      type: string
      nullable: true
      enum:
        - null
        - unpublished
        - published
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** a model "Post" should be added to the projection  

| Field       | Comment | Type         | Null  | Key | Default |
|:------------|:--------|:-------------|:------|:----|:--------|
| id          |         | varchar(512) | false | PK  | NULL    |
| title       |         | varchar(512) | true  |     | NULL    |
| description |         | varchar(512) | true  |     | NULL    |
| status      |         | varchar(512) | true  |     | NULL    |

**And** a "Post" entity configuration should be setup  

```
erDiagram
  Blog ||--o{ Post : contains
  Blog {
    string id
    string title
    string description
  }
  Category ||--o{ Post : contains
  Post {
    string id
    string title
    string description
    string status
  }
```

### Create a content type that already exists

**Given** "Sojourner" adds a schema "Blog" to the "OpenAPI 3.0" specification  

```
Category:
    type: object
    properties:
      title:
        type: string
      summary:
        type: string
```
**When** the "OpenAPI 3.0" specification is parsed  
**Then** an error should be returned.  
