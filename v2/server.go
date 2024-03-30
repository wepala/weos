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

func main() {
	flag.Parse()
	if schema != nil {
		os.Setenv("WEOS_SPEC", *schema)
	}
	if port != nil {
		os.Setenv("WEOS_PORT", *port)
	}
	//use fx Module to start the server
	fx.New(
		rest.API,
		//fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
		//	return &fxevent.ZapLogger{
		//		Logger: log,
		//	}
		//}),
	).Run()
}
