package controllers

import (
	"context"
	"database/sql"
	"github.com/wepala/weos-content-service/projections"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/wepala/weos"
	weoscontroller "github.com/wepala/weos-controller"
)

//RESTAPI is used to manage the API
type RESTAPI struct {
	weoscontroller.API
	Application weos.Application
	Log         weos.Log
	DB          *sql.DB
	Client      *http.Client
	projection  *projections.GORMProjection
}

//Initialize and setup configurations for RESTAPI
func (a *RESTAPI) Initialize() error {
	var err error
	//initialize app
	if a.Client == nil {
		a.Client = &http.Client{
			Timeout: time.Second * 10,
		}
	}
	a.Application, err = weos.NewApplicationFromConfig(a.Config.ApplicationConfig, a.Log, a.DB, a.Client, nil)
	if err != nil {
		return err
	}
	//setup projections
	a.projection, err = projections.NewProjection(a.Application)
	if err != nil {
		return err
	}
	//enable module
	// err = module.Initialize(a.Application)
	// if err != nil {
	// 	return err
	// }
	//run fixtures
	err = a.Application.Migrate(context.Background())
	if err != nil {
		return err
	}
	//set log level to debug
	a.EchoInstance().Logger.SetLevel(log.DEBUG)
	return nil
}

//New instantiates and initializes the api
func New(port *string, apiConfig string) {
	e := echo.New()
	weoscontroller.Initialize(e, &RESTAPI{}, apiConfig)
	e.Logger.Fatal(e.Start(":" + *port))
}
