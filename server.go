package main

import (
	"flag"
	"github.com/wepala/weos/v2/rest"
	"go.uber.org/fx"
	"os"
)

var port = flag.String("port", "8681", "-port=8681")
var schema = flag.String("spec", "./api.yaml", "schema for initialization")
var replay = flag.Bool("replay events", false, "replay events from gorm events")
var mcp = flag.Bool("mcp", false, "enable mcp support")

func main() {
	flag.Parse()
	if schema != nil {
		os.Setenv("WEOS_SPEC", *schema)
	}
	if port != nil {
		os.Setenv("WEOS_PORT", *port)
	}
	if mcp != nil && *mcp {
		//use fx Module to start the mcp server
		fx.New(
			rest.MCP,
		).Run()
	} else {
		//use fx Module to start the server
		fx.New(
			rest.API,
		).Run()
	}

}
