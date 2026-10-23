package main

import (
	"fmt"
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
	// `swagger-mcp index ...` is the explicit write path into the local store
	// (S3). Every other invocation keeps the previous behavior: serve MCP over
	// stdio.
	if len(os.Args) > 1 && os.Args[1] == "index" {
		if err := runIndex(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "[swagger-mcp] index: %v\n", err)
			os.Exit(1)
		}
		return
	}
	serve()
}

func serve() {
	cfg := config.Load()

	mcpServer := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithToolCapabilities(true),
	)

	registry := tools.New(cfg)
	defer registry.Close()
	registry.Register(mcpServer)

	errLogger := log.New(os.Stderr, "[swagger-mcp] ", log.LstdFlags)
	// Say which project's index this session answers from, so a user who ran the
	// server in the wrong directory can see it instead of guessing.
	errLogger.Printf("store: %s", registry.StorePath())

	if err := server.ServeStdio(mcpServer, server.WithErrorLogger(errLogger)); err != nil {
		errLogger.Fatalf("server error: %v", err)
	}
}
