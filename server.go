package main

import (
	"flag"
	weos "github.com/wepala/weos/controllers/rest"
	"os"
)

var port = flag.String("port", "8681", "-port=8681")
var schema = flag.String("spec", "./api.yaml", "schema for initialization")
var replay = flag.Bool("replay events", true, "replay events from gorm events")

func main() {
	flag.Parse()
	apiFlag := *schema
	var apiEnv string
	apiEnv = os.Getenv("WEOS_SPEC")
	if apiEnv != "" {
		weos.Start(*port, apiEnv, *replay)
	} else if *schema != "" {
		weos.Start(*port, apiFlag, *replay)
	}
}
