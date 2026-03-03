# swagger-mcp

A Model Context Protocol (MCP) server that provides granular access to OpenAPI/Swagger specifications. Built for LLM-powered development workflows — query endpoints, schemas, and diffs without leaving your editor.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-stdio-blueviolet)](https://modelcontextprotocol.io)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

## Why?

Large APIs can have hundreds of endpoints. When building a frontend module, you need to understand request/response shapes, required fields, auth schemes, and error codes — scattered across a massive Swagger UI.

**swagger-mcp** gives your LLM direct access to the spec so it can:

- Fetch and cache any OpenAPI 2.0/3.x spec from a URL
- List and filter endpoints by tag, method, or path
- Extract full endpoint details with all `$ref` pointers resolved
- Pull individual schemas with nested references expanded
- Search across paths, summaries, descriptions, and parameters
- Compare two spec versions to see what changed

All output is optimized for LLM consumption with an optional **TOON format** that uses ~40% fewer tokens than JSON.

## Tools

| Tool | Description |
|------|-------------|
| `fetch_spec` | Download and cache a spec. Returns title, version, endpoint/tag/schema counts. |
| `list_endpoints` | List endpoints with optional filters (tag, method, path pattern). |
| `get_endpoint` | Full detail for one endpoint — params, request body, responses, resolved schemas. |
| `get_schema` | Get a named schema with all nested `$ref` fully resolved. |
| `search_spec` | Full-text search across paths, summaries, operation IDs, and parameters. |
| `analyze_tags` | Tag summary with endpoint counts and HTTP method breakdown. |
| `diff_endpoints` | Compare two spec versions. Shows added, removed, and changed endpoints. |

Every tool accepts an optional `format` parameter: `json` (default) or `toon`.

## Installation

### Option 1: Go Install (requires Go 1.25+)

```bash
go install github.com/hnkatze/swagger-mcp-go/cmd/swagger-mcp@latest
```

The binary will be placed in `$GOPATH/bin` (usually `~/go/bin`). Make sure it's on your `PATH`.

### Option 2: Download Binary

Download the latest release for your platform from [GitHub Releases](https://github.com/hnkatze/swagger-extractor-mcp/releases).

```bash
# Linux / macOS
tar -xzf swagger-mcp_<version>_<os>_<arch>.tar.gz
sudo mv swagger-mcp /usr/local/bin/

# Windows
# Extract the .zip and add the directory to your PATH
```

### Option 3: Build from Source

```bash
git clone https://github.com/hnkatze/swagger-extractor-mcp.git
cd swagger-extractor-mcp
go build -o swagger-mcp ./cmd/swagger-mcp/
```

## Configuration

### Claude Code (CLI)

```bash
claude mcp add swagger-mcp --transport stdio -- swagger-mcp
```

### Claude Code (.mcp.json)

Add to your project root or `~/.claude/.mcp.json`:

```json
{
  "mcpServers": {
    "swagger-mcp": {
      "type": "stdio",
      "command": "swagger-mcp",
      "args": []
    }
  }
}
```

If the binary is not on your `PATH`, use the full path:

```json
{
  "mcpServers": {
    "swagger-mcp": {
      "type": "stdio",
      "command": "/path/to/swagger-mcp",
      "args": []
    }
  }
}
```

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "swagger-mcp": {
      "command": "swagger-mcp",
      "args": []
    }
  }
}
```

## Usage Examples

Once configured, the tools are available to the LLM automatically. You interact naturally:

### Explore an API

> "Fetch the spec at https://petstore.swagger.io/v2/swagger.json and tell me what's in it"

The LLM calls `fetch_spec` and returns a summary with endpoint count, tags, and schemas.

### Investigate a Module

> "List all endpoints tagged 'pet' and show me the POST /pet details"

The LLM calls `list_endpoints` filtered by tag, then `get_endpoint` for the full request/response shape.

### Search for Something Specific

> "Search for anything related to 'authentication' in the spec"

The LLM calls `search_spec` and returns matching endpoints ranked by relevance.

### Compare API Versions

> "What changed between v1 and v2 of this API?"

The LLM calls `diff_endpoints` with both spec URLs and reports added, removed, and changed endpoints.

### Build a Frontend Module

> "I need to build the user management module. What endpoints and schemas should I know about?"

The LLM calls `list_endpoints` + `get_endpoint` + `get_schema` to map out the full data model, dependencies, and API contract.

## Output Formats

### JSON (default)

Standard JSON output, suitable for programmatic consumption.

### TOON (Token-Optimized Object Notation)

A compact, human-readable format that reduces token usage by ~40%:

```
GET /pets/{petId} — Get a pet by ID [pets]
parameters:
  petId* (path): integer(int64) — The pet ID
responses:
  200: Successful response
    name: string
    tag: string
    id*: integer(int64)
  404: Pet not found
```

Key conventions:
- `*` marks required fields
- Types use compact notation: `string(email)`, `[]Pet`, `enum(active, inactive)`
- Diff output uses `+` (added), `-` (removed), `~` (changed) prefixes

## Architecture

```
cmd/server/main.go          Entry point — creates MCP server, registers tools, serves stdio
internal/
├── config/config.go         Default configuration (cache TTL, timeouts, limits)
├── loader/
│   ├── loader.go            HTTP fetch, kin-openapi parsing, URL normalization
│   └── cache.go             LRU cache with TTL (mutex-protected)
├── analyzer/analyzer.go     List, search, tag analysis, spec diffing
├── extractor/extractor.go   Endpoint detail, schema extraction, recursive $ref resolution
├── formatter/
│   ├── json.go              JSON output formatting
│   └── toon.go              TOON output formatting
├── tools/tools.go           MCP tool definitions and handlers
└── types/types.go           Shared type definitions and error codes
```

### Key Design Decisions

- **kin-openapi** for parsing — handles OpenAPI 2.0/3.x with automatic `$ref` resolution
- **LRU cache** with 5-minute TTL — avoids re-fetching specs on every tool call
- **Recursive `$ref` resolution** with depth limit (10) and circular reference protection
- **stdio transport** — universal compatibility with MCP clients
- **Zero configuration** — no env vars or config files required, just point at a URL

## Defaults

| Setting | Value |
|---------|-------|
| Cache TTL | 5 minutes |
| Max cached specs | 20 |
| Max spec size | 20 MB |
| Fetch timeout | 30 seconds |
| Default format | JSON |

## Development

```bash
# Run locally
go run ./cmd/swagger-mcp/

# Build
go build -o swagger-mcp ./cmd/swagger-mcp/

# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...
```

## Tech Stack

- [Go](https://go.dev) 1.25+
- [mcp-go](https://github.com/mark3labs/mcp-go) — MCP server SDK
- [kin-openapi](https://github.com/getkin/kin-openapi) — OpenAPI spec parsing and validation

## License

MIT
