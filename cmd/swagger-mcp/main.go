package main

import (
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/tools"
)

const serverName = "swagger-mcp"

// serverVersion is the version reported in the MCP handshake. It is a var so a
// release build can override it with -X main.serverVersion=<tag>: as a const it
// drifted silently (releases shipped v0.6.0 while every client saw 0.4.0).
var serverVersion = "0.7.0"

func main() {
	cfg := config.Load()

	mcpServer := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithToolCapabilities(true),
	)

	registry := tools.New(cfg)
	registry.Register(mcpServer)

	errLogger := log.New(os.Stderr, "[swagger-mcp] ", log.LstdFlags)

	if err := server.ServeStdio(mcpServer, server.WithErrorLogger(errLogger)); err != nil {
		errLogger.Fatalf("server error: %v", err)
	}
}
