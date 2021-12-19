package rest_test

import (
	"github.com/labstack/echo/v4"
	api "github.com/wepala/weos-service/controllers/rest"
	"testing"
)

func TestRESTAPI_Initialize(t *testing.T) {
	t.Run("basic schema", func(t *testing.T) {
		e := echo.New()
		tapi := api.RESTAPI{}
		openApi := `openapi: 3.0.3
info:
  title: Blog
  description: Blog example
  version: 1.0.0
servers:
  - url: https://prod1.weos.sh/blog/dev
    description: WeOS Dev
  - url: https://prod1.weos.sh/blog/v1
x-weos-config:
  logger:
    level: warn
    report-caller: true
    formatter: json
  database:
    driver: sqlite3
    database: test.db
  event-source:
    - title: default
      driver: service
      endpoint: https://prod1.weos.sh/events/v1
    - title: event
      driver: sqlite3
      database: test.db
  databases:
    - title: default
      driver: sqlite3
      database: test.db
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
`
		api.Initialize(e, &tapi, openApi)
		if !tapi.Application.DB().Migrator().HasTable("category") {
			t.Errorf("expected categories table to exist")
		}
	})
}
