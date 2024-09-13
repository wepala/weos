package rest

import (
	"net/http"
	"os"
	"strconv"
	"time"
)

func NewClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100
	httpTimeoutString := os.Getenv("HTTP_TIMEOUT_SECONDS")
	var timeout int
	var err error
	if httpTimeoutString != "" {
		timeout, err = strconv.Atoi(httpTimeoutString)
	}
	if timeout == 0 || err != nil {
		timeout = 20
	}
	return &http.Client{
		Transport: t,
		Timeout:   time.Second * time.Duration(10),
	}
}
