package main

import (
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/hnkatze/swagger-extractor-mcp/internal/config"
	"github.com/hnkatze/swagger-extractor-mcp/internal/tools"
)

const serverName = "swagger-mcp"

// cliUsage is the terminal help. It is the only place that lists the
// subcommands, because the alternative is an agent guessing one.
const cliUsage = `swagger-mcp — a local OpenAPI spec index behind an MCP query server

usage:
  swagger-mcp                                serve MCP over stdio for this project's store
  swagger-mcp index [--refresh] [--name NAME] <url|file>...
                                             read specs and write them to the store
  swagger-mcp unindex <name>                 remove one indexed spec and its catalog rows
  swagger-mcp help                           print this help

The store is ./swa-mcp/swa-mcp.db, discovered by walking up from the working
directory (SWA_MCP_DIR overrides it). Only index and unindex write it; queries
never fetch and never write.
`

// serverVersion is the version reported in the MCP handshake. It is a var so a
// release build can override it with -X main.serverVersion=<tag>: as a const it
// drifted silently (releases shipped v0.6.0 while every client saw 0.4.0).
// Uninjected builds report "dev" rather than a number that is wrong the moment it
// is written; goreleaser injects the real tag on release.
var serverVersion = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "index":
			run("index", runIndex(os.Args[2:], os.Stdout))
			return
		case "unindex":
			run("unindex", runUnindex(os.Args[2:], os.Stdout))
			return
		case "help", "-h", "--help":
			fmt.Print(cliUsage)
			return
		}
	}
	// Every other invocation keeps the previous behavior: serve MCP over stdio.
	serve()
}

// run executes one subcommand and exits non-zero with its message on stderr, so
// a script (or an agent with a shell) can tell success from failure.
func run(name string, err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "[swagger-mcp] %s: %v\n", name, err)
	os.Exit(1)
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
