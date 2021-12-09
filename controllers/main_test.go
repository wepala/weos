package controllers_test

import (
	"os"
	"testing"
)

func TestMain(t *testing.M) {
	os.Remove("test.db")
	code := t.Run()
	os.Exit(code)
}
