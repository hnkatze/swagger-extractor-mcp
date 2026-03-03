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
- Check cache status without making HTTP requests

All output is **token-optimized by default** using TOON format (~40% fewer tokens than JSON), with auto-limiting and guided tool descriptions that reduce typical workflows from ~41K tokens to ~1.3K tokens.

## Tools

| Tool | Description |
|------|-------------|
| `fetch_spec` | Download and cache a spec. Returns title, version, endpoint/tag/schema counts. |
| `analyze_tags` | Tag summary with endpoint counts and method breakdown. **Start here** to understand the API. |
| `list_endpoints` | List endpoints with filters (tag, method, path pattern). Auto-limited to 50 results. |
| `get_endpoint` | Full detail for one endpoint — params, request body, responses, resolved schemas. |
| `get_schema` | Get a named schema with all nested `$ref` fully resolved. |
| `search_spec` | Full-text search across paths, summaries, operation IDs, and parameters. Auto-limited to 50. |
| `diff_endpoints` | Compare two spec versions. Shows added, removed, and changed endpoints. |
| `spec_status` | Check cache status (memory/disk), fingerprint, age, ETag. No HTTP requests. |

### Recommended Workflow

For large APIs, the tools guide the LLM toward an efficient pattern:

```
1. analyze_tags     → Understand API structure (~500 tokens)
2. list_endpoints   → Filter by tag/method (~200 tokens)
3. get_endpoint     → Full details for one endpoint (~500 tokens)
                      Total: ~1,200 tokens vs ~41,000+ without filters
```

### Parameters

All tools accept `format`: `toon` (default, compact) or `json`.

`list_endpoints` and `search_spec` also accept `limit` (default: 50, 0 = unlimited).

## Installation

### Option 1: Go Install (requires Go 1.25+)

```bash
go install github.com/hnkatze/swagger-mcp-go/cmd/swagger-mcp@latest
```

The binary will be placed in `$GOPATH/bin` (usually `~/go/bin`). Make sure it's on your `PATH`.

### Option 2: Download Binary

Download the latest release for your platform from [GitHub Releases](https://github.com/hnkatze/swagger-mcp-go/releases).

```bash
# Linux / macOS
tar -xzf swagger-mcp_<version>_<os>_<arch>.tar.gz
sudo mv swagger-mcp /usr/local/bin/

# Windows
# Extract the .zip and add the directory to your PATH
```

### Option 3: Build from Source

```bash
git clone https://github.com/hnkatze/swagger-mcp-go.git
cd swagger-mcp-go
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

The LLM calls `analyze_tags` to discover tags, then `list_endpoints` filtered by tag, then `get_endpoint` for the full request/response shape.

### Search for Something Specific

> "Search for anything related to 'authentication' in the spec"

The LLM calls `search_spec` and returns matching endpoints ranked by relevance, auto-limited to 50 results.

### Compare API Versions

> "What changed between v1 and v2 of this API?"

The LLM calls `diff_endpoints` with both spec URLs and reports added, removed, and changed endpoints.

### Build a Frontend Module

> "I need to build the user management module. What endpoints and schemas should I know about?"

The LLM calls `analyze_tags` + `list_endpoints` + `get_endpoint` + `get_schema` to map out the full data model, dependencies, and API contract.

## Output Formats

### TOON (default)

Token-Optimized Object Notation — a compact, human-readable format that reduces token usage by ~40% compared to JSON:

```
# showing 50 of 790 — use tag/method/path_pattern filters to narrow results
GET /pets — List all pets [pets]
POST /pets — Create a pet [pets]
GET /pets/{petId} — Get a pet by ID [pets]
```

Detailed endpoint view:

```
GET /pets/{petId} — Get a pet by ID [pets]
parameters:
  - petId (path, required): integer(int64) — The pet ID
responses:
  200: Successful response
    application/json:
      id*: integer(int64)
      name*: string
      tag: string
  404: Pet not found
```

Key conventions:
- `*` marks required fields
- Types use compact notation: `string(email)`, `[]Pet`, `enum(active, inactive)`
- List views strip description (summary is enough for browsing)
- Truncated results show a metadata header with filter guidance
- Diff output uses `+` (added), `-` (removed), `~` (changed) prefixes

### JSON

Standard indented JSON output. Use `format=json` when you need programmatic consumption. List/search responses include truncation metadata:

```json
{
  "total": 790,
  "showing": 50,
  "truncated": true,
  "endpoints": [...]
}
```

## Caching

swagger-mcp uses a two-level cache for fast spec access:

### L1: In-Memory LRU

- TTL: 5 minutes
- Max entries: 20 specs
- Instant access within a session

### L2: Disk Cache

- Location: `~/.swagger-mcp/cache/`
- TTL: 24 hours
- Max entries: 50 specs
- Persists across sessions
- SHA-256 filenames for safe storage
- Atomic writes prevent corruption

### HTTP Conditional Requests

When disk-cached data is stale, swagger-mcp sends conditional HTTP requests using `ETag`/`If-None-Match` and `Last-Modified`/`If-Modified-Since`. If the server returns `304 Not Modified`, the cached data is reused without re-downloading.

### Cache Hierarchy

```
Request → L1 Memory (hit? return)
        → L2 Disk (fresh? promote to L1, return)
        → Conditional HTTP (304? refresh L2, promote to L1, return)
        → Full HTTP fetch (update L1 + L2, return)
```

### Fallback Behavior

- Disk errors: graceful degradation to memory-only cache
- Network errors: fall back to stale disk data
- HTTP 4xx: error (no stale data fallback)
- HTTP 5xx: fall back to stale disk data

## Environment Variables

All settings have sensible defaults. Environment variables are optional overrides:

| Variable | Default | Description |
|----------|---------|-------------|
| `SWAGGER_MCP_DEFAULT_FORMAT` | `toon` | Default output format (`toon` or `json`) |
| `SWAGGER_MCP_DEFAULT_LIMIT` | `50` | Default max results for list/search (0 = unlimited) |
| `SWAGGER_MCP_CACHE_DIR` | `~/.swagger-mcp/cache` | Disk cache directory |
| `SWAGGER_MCP_DISK_CACHE_TTL` | `24h` | Disk cache TTL (Go duration format) |
| `SWAGGER_MCP_CONDITIONAL_FETCH` | `true` | Enable HTTP conditional requests |
| `SWAGGER_MCP_MAX_DISK_ENTRIES` | `50` | Max specs cached on disk |

## Defaults

| Setting | Value |
|---------|-------|
| Default format | TOON |
| Default limit | 50 results |
| Cache TTL (memory) | 5 minutes |
| Cache TTL (disk) | 24 hours |
| Max cached specs (memory) | 20 |
| Max cached specs (disk) | 50 |
| Max spec size | 20 MB |
| Fetch timeout | 30 seconds |
| Conditional fetch | Enabled |

## Architecture

```
cmd/swagger-mcp/main.go          Entry point — config, MCP server, stdio
internal/
├── config/config.go              Configuration with env var overrides
├── loader/
│   ├── loader.go                 L1→L2→HTTP fetch flow, kin-openapi parsing
│   ├── cache.go                  L1 in-memory LRU cache (mutex-protected)
│   └── diskcache.go              L2 disk cache (SHA-256 keys, atomic writes)
├── analyzer/analyzer.go          List, search, tag analysis, spec diffing
├── extractor/extractor.go        Endpoint detail, schema extraction, $ref resolution
├── formatter/
│   ├── json.go                   JSON output (with ListResult wrapper)
│   └── toon.go                   TOON output (headers, strip descriptions)
├── tools/tools.go                MCP tool definitions, handlers, format/limit logic
└── types/types.go                Shared types, error codes, ListResult
```

### Key Design Decisions

- **Token-first defaults** — TOON format + auto-limits minimize LLM token consumption
- **Guided tool descriptions** — tool descriptions steer LLMs toward efficient filter-first workflows
- **Two-level cache** — memory + disk with HTTP conditional requests for instant cross-session access
- **kin-openapi** for parsing — handles OpenAPI 2.0/3.x with automatic `$ref` resolution
- **Recursive `$ref` resolution** with depth limit (10) and circular reference protection
- **stdio transport** — universal compatibility with MCP clients
- **Zero configuration** — sensible defaults, env vars for optional tuning

## Development

```bash
# Run locally
go run ./cmd/swagger-mcp/

# Build
go build -o swagger-mcp ./cmd/swagger-mcp/

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run only unit tests (skip integration)
go test -short ./...

# Run tests with coverage
go test -cover ./...
```

## Tech Stack

- [Go](https://go.dev) 1.25+
- [mcp-go](https://github.com/mark3labs/mcp-go) — MCP server SDK
- [kin-openapi](https://github.com/getkin/kin-openapi) — OpenAPI spec parsing and validation

## License

MIT
