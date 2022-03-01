package rest

import (
	"context"
	"encoding/json"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/labstack/echo/v4"
	weosLogs "github.com/wepala/weos/log"
	"github.com/wepala/weos/model"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

//New instantiates and initializes the api
func New(apiConfig string) (*RESTAPI, error) {
	e := echo.New()
	var err error
	api := &RESTAPI{}
	_, err = Initialize(e, api, apiConfig)
	if err != nil {
		e.Logger.Errorf("Unexpected error: '%s'", err)
	}
	return api, err
}

func Start(port string, apiConfig string, replay bool) *RESTAPI {
	api, err := New(apiConfig)
	if err != nil {
		api.EchoInstance().Logger.Error(err)
	}
	err = api.Initialize(nil)
	if err != nil {
		api.EchoInstance().Logger.Fatal(err)
	}

	if replay == true {
		e, _ := api.GetEventStore("Default")
		eventRepo := e.(*model.EventRepositoryGorm)
		projection, _ := api.GetProjection("Default")
		factories := api.GetEntityFactories()

		total, success, failed, err := eventRepo.ReplayEvents(context.Background(), time.Time{}, factories, projection)
		api.EchoInstance().Logger.Debugf("total: %d, success: %d, failed: %d, err: %s", total, success, failed, err)
	}

	api.EchoInstance().Logger.Fatal(api.EchoInstance().Start(":" + port))
	return api
}

//Serve API
func Serve(port string, api *RESTAPI) *RESTAPI {
	err := api.Initialize(nil)
	if err != nil {
		api.EchoInstance().Logger.Error(err)
	}
	api.EchoInstance().Logger.Fatal(api.EchoInstance().Start(":" + port))
	return api
}

func Initialize(e *echo.Echo, api *RESTAPI, apiConfig string) (*echo.Echo, error) {
	e.HideBanner = true
	if apiConfig == "" {
		apiConfig = "./api.yaml"
	}

	//setting the default logger in echo as zap where log level is error by default
	e.Logger, _ = weosLogs.NewZap("error")

	//set echo instance because the instance may not already be in the api that is passed in but the handlers must have access to it
	api.SetEchoInstance(e)

	//configure context middleware using the register method because the context middleware is in it's own file for code readability reasons
	api.RegisterMiddleware("Context", Context)

	var content []byte
	var err error
	//try load file if it's a yaml file otherwise it's the contents of a yaml file WEOS-1009
	if strings.Contains(apiConfig, ".yaml") || strings.Contains(apiConfig, "/yml") {
		content, err = ioutil.ReadFile(apiConfig)
		if err != nil {
			e.Logger.Fatalf("error loading api specification '%s'", err)
		}
	} else {
		content = []byte(apiConfig)
	}

	//change the $ref to another marker so that it doesn't get considered an environment variable WECON-1
	tempFile := strings.ReplaceAll(string(content), "$ref", "__ref__")
	//replace environment variables in file
	tempFile = os.ExpandEnv(string(tempFile))
	tempFile = strings.ReplaceAll(string(tempFile), "__ref__", "$ref")
	content = []byte(tempFile)
	loader := openapi3.NewSwaggerLoader()
	swagger, err := loader.LoadSwaggerFromData(content)
	if err != nil {
		e.Logger.Fatalf("error loading api specification '%s'", err)
	}
	//parse the main config
	var config *APIConfig
	if swagger.ExtensionProps.Extensions[WeOSConfigExtension] != nil {

		data, err := swagger.ExtensionProps.Extensions[WeOSConfigExtension].(json.RawMessage).MarshalJSON()
		if err != nil {
			e.Logger.Fatalf("error loading api config '%s", err)
			return e, err
		}
		err = json.Unmarshal(data, &config)
		if err != nil {
			e.Logger.Fatalf("error loading api config '%s", err)
			return e, err
		}

		err = api.AddConfig(config)
		if err != nil {
			e.Logger.Fatalf("error setting up module '%s", err)
			return e, err
		}
	}
	api.Swagger = swagger
	return e, nil
}
