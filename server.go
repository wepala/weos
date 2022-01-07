//go:build server
// +build server

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
	api.New(port, "./api.yaml")
}
