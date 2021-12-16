package service_test

import (
	"context"
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	api "github.com/wepala/weos-content-service/controllers"
)

var e *echo.Echo
var API api.RESTAPI

func reset(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
	os.Remove("test.db")
	return ctx, nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Before(reset)

	//add context steps
}
func InitializeSuite(ctx *godog.TestSuiteContext) {
	e = echo.New()
	api.Initialize(e, &API, "../api.yaml")
}

func TestBDD(t *testing.T) {
	status := godog.TestSuite{
		Name:                "BDD Tests",
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format: "pretty",
		},
	}.Run()
	if status != 0 {
		t.Errorf("there was an error running tests, exit code %d", status)
	}
}
