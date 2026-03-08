package main

import (
	"fmt"
	"os"

	mcpserver "weos/internal/mcp"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	if err := mcpserver.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}
