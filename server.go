// +build server

package service

import (
	"flag"
)

var port = flag.String("port", "8681", "-port=8681")

func main() {
	flag.Parse()
	//TODO: import api and run the New function with port as argument
}
