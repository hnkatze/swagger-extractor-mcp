package main

import (
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/hnkatze/swagger-mcp-go/internal/config"
	"github.com/hnkatze/swagger-mcp-go/internal/tools"
)

const (
	serverName    = "swagger-mcp"
	serverVersion = "0.4.0"
)

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
