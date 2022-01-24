package main

import (
	"flag"
	api "github.com/wepala/weos/controllers/rest"
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
}
