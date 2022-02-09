package main

import (
	"context"
	"flag"
	weos "github.com/wepala/weos/controllers/rest"
	"os"
	"time"
)

var port = flag.String("port", "8681", "-port=8681")
var schema = flag.String("spec", "./api.yaml", "schema for initialization")
var replay = flag.Bool("replay events", false, "replay events from gorm events")

//TODO Add a flag for the time

func main() {
	flag.Parse()
	apiFlag := *schema
	var apiEnv string
	var restAPI *weos.RESTAPI
	apiEnv = os.Getenv("WEOS_SPEC")
	if apiEnv != "" {
		restAPI = weos.Start(*port, apiEnv)
	} else if *schema != "" {
		restAPI = weos.Start(*port, apiFlag)
	}

	if *replay == true {
		e, _ := restAPI.GetEventStore("default")
		factories := restAPI.GetEntityFactories()
		e.ReplayEvents(context.Background(), time.Time{}, factories)
	}

}
