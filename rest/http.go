package rest

import (
	"net/http"
	"time"
)

func NewClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100
	return &http.Client{
		Transport: t,
		Timeout:   time.Second * 10,
	}
}
