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
