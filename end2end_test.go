package service_test

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/labstack/echo/v4"
	weoscontroller "github.com/wepala/weos-controller"
)

var e *echo.Echo
var API weoscontroller.APIInterface

func reset(*godog.Scenario) {
	os.Remove("test.db")
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	e = echo.New()
	weoscontroller.Initialize(e, API, "../api.yaml")

	ctx.BeforeScenario(reset)
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
