# swagger-mcp

A local index for OpenAPI/Swagger specs plus an MCP query layer on top of it.
Index a spec once into `./swa-mcp/swa-mcp.db` inside your project, and every MCP
tool answers from that SQLite file — no server, no hosting, no auth, no key. The
specs never leave your machine.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![MCP](https://img.shields.io/badge/MCP-stdio-blueviolet)](https://modelcontextprotocol.io)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

## Why?

Large APIs have hundreds of endpoints. To build one frontend module you need
request/response shapes, required fields, auth schemes and error codes that are
scattered across a giant Swagger UI — and a naive LLM reading the whole document
burns tens of thousands of tokens on things it never uses.

`swagger-mcp` is to OpenAPI what a code graph is to source: one explicit indexing
step, then cheap structured queries.

- **Local by default.** One SQLite store per project, written by the CLI. Queries
  read the store and never touch the network.
- **No infrastructure.** A static binary per platform; nothing to deploy, no
  token to rotate, nothing to trust with your private spec.
- **Token-optimized by default.** Compact TOON output, auto-limiting and guided
  tool descriptions keep explorations small instead of dumping whole documents.
- **Multi-spec.** Index several specs side by side, address them by name, sweep
  all of them, or diff two of them.

## Quickstart

### 1. Install

```bash
# Go 1.25+
go install github.com/hnkatze/swagger-mcp-go/cmd/swagger-mcp@latest
```

Or take the release asset for your platform from
[GitHub Releases](https://github.com/hnkatze/swagger-mcp-go/releases):

```bash
# Linux / macOS — archives are swagger-mcp_<version>_<os>_<arch>.tar.gz
tar -xzf swagger-mcp_<version>_linux_amd64.tar.gz
sudo mv swagger-mcp /usr/local/bin/

# Windows — extract the .zip and put swagger-mcp.exe on your PATH
```

From source:

```bash
git clone https://github.com/hnkatze/swagger-mcp-go.git
cd swagger-mcp-go
go build -o swagger-mcp ./cmd/swagger-mcp/
```

### 2. Index your spec(s)

Run this in the project that will query the API:

```bash
cd ~/proyectos/my-app
swagger-mcp index https://api.example.com/openapi.json
# indexed openapi: Example API 1.2.0 (43 endpoints, 17 schemas)
# store: /home/you/proyectos/my-app/swa-mcp/swa-mcp.db
```

Sources can be URLs or local files, and you can pass several at once:

```bash
swagger-mcp index ./openapi.yaml https://api.example.com/openapi.json
```

The command creates `./swa-mcp/swa-mcp.db` if it does not exist. Nothing else
writes to that file.

### 3. Register the MCP server

The server reads the store it finds from its working directory, so register it
**per project** (cwd = project root) with no arguments.

Claude Code — `.mcp.json` in the project root (or `~/.claude/.mcp.json`):

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

Claude Desktop — `claude_desktop_config.json`:

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

If the binary is not on your `PATH`, use its absolute path as `command`. On
startup the server logs the store it resolved to stderr, so a wrong working
directory is visible instead of silent:

```
[swagger-mcp] 2026/02/10 09:11:02 store: /home/you/proyectos/my-app/swa-mcp/swa-mcp.db
```

### 4. The first three calls

| Call | Answers |
|------|---------|
| `list_specs` | What is indexed: name, title, version, source, fetch time, endpoint/schema counts. |
| `list_endpoints` | The endpoints, one line each — `GET /pets — List all pets [pets]`. Filter by `tag`, `method`, `path_pattern`, `deprecated`. |
| `get_endpoint` | One endpoint in full: parameters, request body, responses, resolved schemas. |

A minimal, cheap sequence:

```
list_specs()                                      → the indexed specs and their counts
analyze_tags()                                    → tags with endpoint counts; pick one
list_endpoints(tag="pets")                        → one line per endpoint in that tag
get_endpoint(method="GET", path="/pets/{petId}")  → params, body, responses
```

## The store

```
my-app/
└── swa-mcp/
    └── swa-mcp.db      # SQLite: the whole index for this project
```

- **One file per project.** The store belongs to the project that queries it, and
  `internal/store` keeps a single writer per file, so it is not a shared
  multi-tenant database.
- **Created by the indexer, never by hand.** A query that finds no store fails
  with `NO_STORE` instead of creating one, and the query layer opens the database
  read-only: it never creates the file, the schema or a migration.
- **Discovery walks up.** Starting from the working directory, the server and the
  indexer use the nearest `swa-mcp/` directory it finds above it, falling back to
  `<cwd>/swa-mcp`.
- **`SWA_MCP_DIR` overrides discovery** with an explicit store directory (useful
  for a shared or out-of-tree index).
- **Gitignored.** `swa-mcp/` is in this repo's `.gitignore`; add the same entry to
  yours. The index is generated state, not source.

### Reindexing

```bash
swagger-mcp index https://api.example.com/openapi.json          # idempotent
swagger-mcp index --refresh https://api.example.com/openapi.json # force a rewrite
swagger-mcp index --name petstore-v3 ./openapi.json             # store it under a name you choose
```

- Reindexing the same source updates its row in place — it never duplicates a
  spec, with or without `--refresh`.
- A name addresses exactly one source. Indexing a source whose name is derived to
  one that already belongs to a **different** source fails instead of replacing
  it:

  ```
  $ swagger-mcp index v1/api.json v2/api.json
  indexed api: Billing API 1.0.0 (5 endpoints, 4 schemas)
  v2/api.json: cannot index v2/api.json as "api": that name already belongs to
    v1/api.json; pass --name to index this spec under a different name
  ```

  Nothing is written for the refused source, and the first spec is untouched.
- Without `--refresh`, an unchanged document is reported and **not** rewritten:

  ```
  unchanged openapi: Example API 1.2.0 (content identical, use --refresh to rewrite)
  ```

- `--refresh` re-reads the source and rewrites the stored copy (and its
  `fetched_at`), even when the content is identical.
- `--name` sets the addressable name instead of the derived one. It accepts a
  single source only. The default name is the base name of the file or URL path
  without `.json`/`.yaml`/`.yml` (a URL with no path falls back to the hostname).
- A source larger than the size limit is refused, never truncated: a cut-off
  document could otherwise parse into a different API.
- `spec_status` prints the refresh command that keeps the stored name:
  `swagger-mcp index --refresh --name billing https://api.example.com/v1/api.json`
  whenever the name was not derived from the source, so copying it can neither
  rename the spec nor collide with another source.

Indexing validates first: a spec that fails validation is rejected and nothing is
written. It is validated with the same relaxed rules the query layer uses, so a
spec you can index is a spec you can query.

## Tools

All answers come from the store. There is no fetch tool and no refresh tool —
indexing is a CLI command.

| Tool | What it answers | Parameters |
|------|-----------------|------------|
| `list_specs` | Every indexed spec with title, version, source, fetch time and counts. Start here when a call says several specs are indexed. | `format` |
| `list_endpoints` | Endpoints, one line each with method, path, summary and tags. | `spec`, `tag`, `method`, `path_pattern`, `deprecated`, `all_specs`, `limit`, `offset`, `format` |
| `get_endpoint` | One endpoint in full: params, request body, responses, resolved schemas. Shared schemas are shown once then referenced as `$ref(Name)`. | `spec`, `method`*, `path`*, `resolve_depth`, `format` |
| `get_schema` | A named schema/model with field types and descriptions. Use it to expand a `$ref(Name)` from `get_endpoint`. | `spec`, `name`*, `resolve_depth`, `format` |
| `search_spec` | Keyword search across paths, summaries, descriptions, operation IDs and body field names, ranked by relevance. | `spec`, `query`*, `all_specs`, `limit`, `offset`, `format` |
| `list_schemas` | Component schemas with field count and how many endpoints use each. | `spec`, `filter`, `all_specs`, `limit`, `offset`, `format` |
| `security_schemes` | Auth scheme definitions (http/bearer/apiKey/OAuth2/OIDC) with how many endpoints use each. A requirement list is an OR of alternatives. | `spec`, `format` |
| `validate_spec` | VALID/INVALID plus the spec's real violations, reported exactly as the document wrote them. | `spec`, `format` |
| `analyze_tags` | Tags with endpoint counts and method breakdown — the map of the API. | `spec`, `all_specs`, `format` |
| `diff_endpoints` | Added, removed and changed endpoints between two indexed specs. | `spec_old`*, `spec_new`*, `path`, `method`, `format` |
| `spec_status` | Source, etag, fetch time, age, a stale verdict and the exact refresh command. Read-only; always JSON. | `spec` |
| `generate_types` | Copy-paste-ready TypeScript interfaces or Go structs from a tag, a named schema, or an endpoint. | `spec`, `tag`, `method`, `path`, `schema`, `language`, `format` |
| `usage_guide` | This server's workflow, token rules, output notation and example sequences, for an LLM that lacks setup context. | — |

`*` = required.

### Addressing a spec

`spec` selects one indexed spec by the name the indexer stored.

- **One spec indexed** — omit `spec`; that spec is used.
- **Several indexed, no `spec`** — the call fails with `SPEC_AMBIGUOUS` and the
  message lists the names. It never guesses.
- **Unknown name** — the call fails with `SPEC_NOT_FOUND`, and the same list of
  indexed names is returned so the caller can correct itself.
- **Nothing indexed** — the call fails with `NO_STORE` and names
  `swagger-mcp index`.
- No tool accepts a `url`. Addressing is by name, never by URL, and a call that
  still sends one fails with `INVALID_QUERY` explaining that specs are indexed
  with `swagger-mcp index` and queried with `spec=<name>`.

### Sweeping every spec

`list_endpoints`, `search_spec`, `list_schemas` and `analyze_tags` accept
`all_specs=true`, which reads every indexed spec and prefixes each result line
with the spec it came from (`petstore: GET /pets — ...`). It is the most
expensive call shape; prefer `spec=<name>` unless you really want the whole store.

### Parameter details

- `format` — `toon` (default, compact) or `json`. `spec_status` is always JSON and
  `usage_guide` takes no arguments.
- `limit` / `offset` — on `list_endpoints`, `search_spec` and `list_schemas`.
  `limit` defaults to 50 and `0` means unlimited; `offset` defaults to 0. The
  result header shows what you got and the next offset, so page instead of
  raising the limit.
- `resolve_depth` — on `get_endpoint` and `get_schema`: schema nesting depth
  `0`–`10`, default `3`. `0` = names only (cheapest); raise it for deep models.
- `deprecated` — on `list_endpoints`: `true` = only deprecated, `false` = only
  live endpoints, omit for both.

## Staleness

`spec_status` reports what the store knows about one spec — no network access:

```json
{
  "name": "billing",
  "source": "https://api.example.com/v1/api.json",
  "etag": "\"abc123\"",
  "fetched_at": "2026-02-10T09:11:02Z",
  "age": "9d3h",
  "age_seconds": 795420,
  "stale": true,
  "stale_after_days": 7,
  "refresh": "swagger-mcp index --refresh --name billing https://api.example.com/v1/api.json",
  "endpoint_count": 43,
  "schema_count": 17,
  "store": "/home/you/proyectos/my-app/swa-mcp/swa-mcp.db"
}
```

Keys and the `stale_after_days` value are exact; the values are an illustration.

A stored copy older than **7 days** is reported as `stale: true`. Nothing is
refreshed automatically: `spec_status` is read-only, no tool fetches anything, and
the `refresh` field carries the exact command a human runs — including
`--name <stored name>` when the name was chosen rather than derived, so the
command refreshes that spec instead of renaming it. Reindexing is always explicit.

## Errors you may hit

Failures come back as tool errors prefixed with a stable code.

| Code | Meaning | What to do |
|------|---------|------------|
| `NO_STORE` | No store in this project, or an empty one — the message names the path it looked for. | Run `swagger-mcp index <url\|file>` in this project; check the client's working directory (the server logs the store it resolved). |
| `INVALID_QUERY` | An argument was rejected: a tool call that still sends `url`, or `search_spec` without a `query`. | Drop `url` (specs are indexed with `swagger-mcp index`, addressed with `spec=<name>`) and pass the required arguments. |
| `SPEC_AMBIGUOUS` | Several specs are indexed and no `spec` was passed. The message lists them. | Pass `spec=<name>` (names come from `list_specs`), or `all_specs=true` to sweep. |
| `SPEC_NOT_FOUND` | The requested name is not indexed. The message lists the indexed names. | Retry with one of the listed names, or index the spec you meant. |
| `DIFF_NEEDS_SPECS` | Fewer than two specs are indexed, so there is nothing to compare. | `swagger-mcp index` the second spec (or the older version), then diff by name. |
| `STORE_ERROR` | The store exists but cannot be opened or read — permissions, a corrupt file, or a store written by a newer schema version. | Fix access to `swa-mcp/`, or upgrade `swagger-mcp` if the store reports a newer schema version. Removing `swa-mcp/` and reindexing rebuilds the store. |

## Migration from v0.7.0

v0.7.0 fetched specs on demand: the `fetch_spec` tool downloaded a URL and made it
the session default, every read tool accepted a `url` parameter, and
`SWAGGER_MCP_DEFAULT_URL` seeded that default at startup.

`fetch_spec`, `refresh_spec`, the `url` parameter and that environment variable
are all gone. Indexing is an explicit command,
and queries are addressed by the name of an indexed spec:

```bash
# before: the LLM fetched a URL to start a session
# now:
swagger-mcp index https://api.example.com/openapi.json
```

Then drop any `url` arguments and the old default-URL variable from your client
config. Pass `spec=<name>` only when the project indexes more than one spec.

## Output formats

### TOON (default)

Token-Optimized Object Notation — compact and readable, and cheaper per response
than indented JSON.

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

Conventions:

- `*` marks required fields
- Types use compact notation: `string(email)`, `[]Pet`, `enum(active, inactive)`
- List views strip descriptions — the summary is enough for browsing
- A shared schema is shown once, then referenced as `$ref(Name)`
- Truncated lists start with a metadata header and the next offset
- Diffs use `+` (added), `-` (removed), `~` (changed)

### JSON

Standard indented JSON for programmatic consumption (`format=json`). List answers
carry truncation metadata:

```json
{
  "total": 790,
  "showing": 50,
  "offset": 0,
  "truncated": true,
  "endpoints": [...]
}
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SWA_MCP_DIR` | (empty) | Explicit store directory, overriding walk-up discovery. |
| `SWAGGER_MCP_DEFAULT_FORMAT` | `toon` | Default output format (`toon` or `json`). |
| `SWAGGER_MCP_DEFAULT_LIMIT` | `50` | Default `limit` for list/search tools (`0` = unlimited). |
| `SWAGGER_MCP_FETCH_TIMEOUT` | `120s` | Timeout for a URL read during `swagger-mcp index` (Go duration, must be > 0). |

## LLM context guide

Add a snippet like this to `CLAUDE.md`, `.cursorrules`, or your system prompt so
the model uses the tools efficiently:

```markdown
## API exploration (swagger-mcp)

Answers come from a local index. If a call reports no store or several specs,
ask the human to run `swagger-mcp index` instead of guessing.

### Workflow
1. `list_specs` — what is indexed, its source and how old it is
2. `analyze_tags` — map the API before listing anything
3. `list_endpoints` with tag/method/path filters
4. `get_endpoint` for the one endpoint you need in full
5. `generate_types` to emit TypeScript or Go instead of translating schemas
6. `get_schema` only to expand a `$ref(Name)` whose fields you actually need

### Rules
- Omit `spec` when the project indexes one spec; pass `spec=<name>` only when several are indexed
- Filter before listing: `analyze_tags` → filtered `list_endpoints` beats an unfiltered dump
- Page with `limit`/`offset` instead of raising the limit
- Default output is TOON; use `format=json` only when you need structured JSON
- Schemas resolve 3 levels deep by default; lower `resolve_depth` (or 0) to save tokens
- Refreshing is explicit: `spec_status` shows age and a `stale` verdict, and its
  `refresh` field holds the command a human must run — never assume it happened

### Anti-patterns
- Don't call `list_endpoints` unfiltered on a large API
- Don't call `get_endpoint` for every endpoint — narrow down first
- Don't call `all_specs=true` unless you really need the whole store
- Don't expand every `$ref(Name)` — only the ones whose fields you need
```

For Cursor, put the same content in `.cursorrules`. The `usage_guide` tool returns
this guidance on demand, so a model with no setup context can self-orient.

## Architecture

```
cmd/swagger-mcp/main.go      Entry point — `index` subcommand, else MCP over stdio
cmd/swagger-mcp/index.go     The indexer: read, validate, write the store
internal/
├── config/config.go         Defaults and env overrides
├── store/                   SQLite index: layout/discovery, schema, records
├── tools/                   MCP tool definitions, handlers, spec addressing
├── loader/                  kin-openapi parsing, normalization, validation
├── analyzer/                List, search, tag analysis, spec diffing
├── extractor/               Endpoint detail, schema extraction, $ref resolution
├── generator/               TypeScript and Go type emitters
├── formatter/               TOON and JSON output
└── types/types.go           Shared types and error codes
```

### Design decisions

- **The store is the product's data model.** One SQLite file per project holds the
  raw documents plus a normalized catalog (specs, endpoints, schemas, tags) for
  discovery. Detail queries load that one stored document and reuse the existing
  extractor instead of reimplementing `$ref` resolution in SQL.
- **Writes are explicit, reads never touch the network.** Only
  `swagger-mcp index` writes and only it may read a URL. Dropping URL addressing
  from the query path removes the whole class of query-time fetch problems.
- **Token-first defaults.** TOON, auto-limits and per-endpoint `$ref` dedup keep
  real-world explorations small.
- **Description-forward schemas.** Field descriptions are inlined while noisy
  `example`/`default` payloads are dropped: signal, not bulk.
- **Guided tool descriptions.** Descriptions steer a model toward
  filter-first workflows instead of full dumps.
- **stdio transport.** Universal client compatibility; the same binary is also the
  indexer.

## Development

```bash
go run ./cmd/swagger-mcp/                    # serve MCP over stdio
go run ./cmd/swagger-mcp/ index ./testdata/petstore.json   # writes ./swa-mcp/swa-mcp.db
go build -o swagger-mcp ./cmd/swagger-mcp/   # build
go test ./...                                # tests
go test -short ./...                         # unit tests only
go test -cover ./...                         # with coverage
```

## Tech stack

- [Go](https://go.dev) 1.25+
- [mcp-go](https://github.com/mark3labs/mcp-go) — MCP server SDK
- [kin-openapi](https://github.com/getkin/kin-openapi) — OpenAPI parsing and validation
- [modernc.org/sqlite](https://modernc.org/sqlite) — pure-Go SQLite (builds with `CGO_ENABLED=0`)

## License

MIT
