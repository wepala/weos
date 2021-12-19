//go:build server
// +build server

package main

import (
	"flag"
	api "github.com/wepala/weos-content-service/controllers/rest"
)

var port = flag.String("port", "8681", "-port=8681")

func main() {
	flag.Parse()
	api.New(port, "./api.yaml")
}
