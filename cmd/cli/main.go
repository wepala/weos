package main

import (
	"weos/internal/cli"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
