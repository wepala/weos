package main

import (
	"flag"
	weos "github.com/wepala/weos/controllers/rest"
	"golang.org/x/net/context"
	"os"
	"time"
)

var port = flag.String("port", "8681", "-port=8681")
var schema = flag.String("spec", "./api.yaml", "schema for initialization")
var replay = flag.Bool("replay events", true, "replay events from gorm events")

//Pass flag for replay events

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

		//Entity factory will be needed in this context.
		e.ReplayEvents(context.Background(), time.Time{})
	}

}
