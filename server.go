package main

import (
	"flag"
	api "github.com/wepala/weos-service/controllers/rest"
	"os"
)

var port = flag.String("port", "8681", "-port=8681")
var schema = flag.String("schema", "./api.yaml", "schema for initialization")

func main() {
	flag.Parse()
	apiFlag := *schema
	var apiEnv string
	os.Setenv("WEOS_SCHEMA", `
openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
x-weos-config:
  event-source:
    - title: default
      driver: service
      endpoint: https://prod1.weos.sh/events/v1
    - title: event
      driver: dynamodb
      database: events
  database:
    driver: sqlite3
    database: e2e.db
  rest:
    middleware:
      - RequestID
      - Recover
      - ZapLogger
components:
  schemas:
    Category:
      type: object
      properties:
        title:
          type: string
        description:
          type: string
      required:
        - title
      x-identifier:
        - title
    Author:
      type: object
      properties:
        firstName:
          type: string
        lastName:
          type: string
        email:
          type: string
          format: email
      required:
        - firstName
        - lastName
      x-identifier:
        - email
    Blog:
      type: object
      properties:
        url:
          type: string
          format: uri
        title:
          type: string
        description:
          type: string
        image:
          type: string
          format: byte
        categories:
          type: array
          items:
            $ref: "#/components/schemas/Post"
        posts:
          type: array
          items:
            $ref: "#/components/schemas/Category"
        lastUpdated:
          type: string
          format: date-time
        created:
          type: string
          format: date-time
      required:
        - title
        - url
    Post:
      type: object
      properties:
        title:
          type: string
        description:
          type: string
        author:
          $ref: "#/components/schemas/Author"
        created:
          type: string
          format: date-time
paths:
  /health:
    summary: Health Check
    get:
      x-controller: HealthCheck
      responses:
        200:
          description: Health Response
        500:
          description: API Internal Error
  /blogs:
    post:
      operationId: Add Blog
      requestBody:
        description: Blog info that is submitted
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/Blog"
          application/x-www-form-urlencoded:
            schema:
              $ref: "#/components/schemas/Blog"
      responses:
        201:
          description: Add Blog to Aggregator
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Blog"
    get:
      operationId: Get Blogs
      parameters:
        - in: query
          name: q
          schema:
            type: string
          required: false
          description: query string
      responses:
        200:
          description: List of blogs
          content:
            application/json:
              schema:
                type: object
                properties:
                  total:
                    type: integer
                  page:
                    type: integer
                  items:
                    $ref: "#/components/schemas/Blog"
  /blogs/{id}:
    get:
      parameters:
        - in: path
          name: id
          schema:
            type: string
          required: true
          description: blog id
      summary: Get Blog by id
      responses:
        200:
          description: Blog details without any supporting collections
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Blog"

  /blogs/{id}/posts:
    get:
      parameters:
      - in: path
        name: id
        schema:
          type: string
        required: true
        description: blog id
      summary: Get a blog's posts
      operationId: Post List
      responses:
        200:
          description: List of a blog's posts
          content:
            application/json:
              schema:
                type: object
                properties:
                  total:
                    type: integer
                  page:
                    type: integer
                  items:
                    type: array
                    items:
                      $ref: "#/components/schemas/Post"
  /blogs/{id}/posts/{postId}:
    get:
      parameters:
        - in: path
          name: id
          schema:
            type: string
          required: true
          description: blog id
        - in: path
          name: postId
          schema:
            type: string
          required: true
      summary: Get blog post by id
      responses:
        200:
          description: Get blog post information
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Post"`)
	apiEnv = os.Getenv("WEOS_SCHEMA")

	if apiEnv != "" {
		api.New(port, apiEnv)
	} else if *schema != "" {
		api.New(port, apiFlag)
	}
	//TODO check if WEOS_SCHEMA environment variable is set and use that
	//TODO check if there is a flag schema is set and use that
	//TODO if none of those are set default to api.yaml
}
