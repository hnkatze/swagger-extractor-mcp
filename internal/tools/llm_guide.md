# swagger-mcp — usage guide

## What this is

A **local index** for OpenAPI/Swagger specs plus a query layer over it. Every
answer comes from the project's own SQLite store (`./swa-mcp/swa-mcp.db`).
Queries never download anything: `index_spec` is the one tool that reads a source
(a local file or a URL) and the only one that writes, and everything else is
read-only against that store, so a repeated call is a query, not a network round
trip. Nothing here needs an account, a key or a hosted service.

## Step 0 — index first (two equivalent ways)

Nothing can be answered before a spec is in the store. Indexing is one explicit
write, and there are two equivalent ways to perform it.

**With a shell** — run it yourself; the store lands in the project's `swa-mcp/`
directory, which is exactly where the tools look for it:

```
swagger-mcp index <source>...            # read a spec once and write it to the store
swagger-mcp index --refresh <src>        # re-read a source and rewrite the stored copy
swagger-mcp index --name billing <src>   # store it under a name you choose
swagger-mcp unindex <name>               # remove one indexed spec and its catalog rows
```

**Without a shell** — call `index_spec`, which does exactly what
`swagger-mcp index` does: same size cap, same validation rules, same store, same
refusals.

```
index_spec(source="./openapi.yaml")                    → index a file
index_spec(source="https://api.example.com/api.json")  → index a URL
index_spec(source="./openapi.yaml", name="billing-v2") → choose the store name
index_spec(source="./openapi.yaml", refresh=true)      → rewrite the stored copy
```

Never ask the user to run a command you could run yourself or do through
`index_spec`. Indexing is the one step that reads a source; everything after it
is a store query. Do not pass a URL to a query tool: a spec is addressed by the
name the indexer stored, never by where it came from.

`index` accepts several sources in one call and is idempotent: an unchanged
document is reported and not rewritten unless you pass `--refresh` (or
`refresh=true`). Two different sources that derive the same name are refused and
the error names the source that owns it; pass `name=` (or `--name`) to keep both.
`unindex` removes exactly one spec (its document and its catalog rows) and
reports the name, title and counts it removed; an unknown name errors with the
list of indexed names and changes nothing. It is a CLI-only command.

## Orientation — `list_specs` first

Call `list_specs()` before anything else in a session. It returns one line per
indexed spec: name, title, version, endpoint/schema counts, source and fetch
time. It reads metadata only, so it is the cheapest call here.

- One spec indexed: stop looking, the other tools use it automatically.
- Several: read the names, then pass `spec=<name>` on every call that takes one.
- Nothing indexed: you get `NO_STORE` — go back to step 0.

## Addressing a spec

- **One spec indexed** — omit `spec`; it is used automatically. Repeating the name
  on a single-spec project is pure waste.
- **Several indexed** — pass `spec=<name>` (the names come from `list_specs`). No
  tool guesses among several.
- **`all_specs=true`** — `list_endpoints`, `search_spec`, `list_schemas` and
  `analyze_tags` accept it to sweep every indexed spec; each result line is then
  prefixed with its spec name (`petstore: GET /pets — ...`). It is the most
  expensive shape, so use it to locate something across specs, then go back to
  `spec=<name>`.
- **`diff_endpoints` is always explicit** — it takes `spec_old` and `spec_new`,
  because a diff against one spec is never what you meant.

## Exploration path (recommended)

1. `list_specs()` — which spec(s) exist, and their counts.
2. `analyze_tags()` — the map of the API: tags with endpoint counts and a method
   breakdown. Pick the tag that matches the question.
3. `list_endpoints(tag="Orders")` — filter by `tag` (preferred), `method`,
   `path_pattern` or `deprecated`, and page with `limit`/`offset`. The header
   tells you the range and the next offset.
4. `get_endpoint(method="POST", path="/orders")` — params, request body,
   responses and resolved schemas for that one endpoint.
5. `get_schema(name="Order")` — only to expand a `$ref(Order)` you actually need.

Alternatives along the way:

- `search_spec(query="create order")` — the shortcut when you already know a
  keyword. It ranks matches across paths, summaries, operation ids, descriptions,
  parameter names and body field names, and pages like `list_endpoints`.
- `list_schemas(filter="order")` — discover models without guessing names: field
  count and how many endpoints use each, most used first.
- `security_schemes()` — how to authenticate: type, bearer format, apiKey header
  or OAuth2/OIDC URLs, and how many endpoints use each scheme. A requirement list
  is an OR of alternatives (`Bearer OR ApiKey` means either works).
- `generate_types(tag="Orders", language="typescript")` — ready-to-paste
  TypeScript interfaces (`language="go"` for structs) instead of translating
  schemas by hand. `tag` mode covers every schema a tag uses in one call.
- `validate_spec()` — VALID/INVALID plus the spec's real violations. Use it when
  authoring or reviewing a spec, not when consuming one.
- `diff_endpoints(spec_old="billing-v1", spec_new="billing-v2")` — added, removed
  and changed endpoints between two indexed specs.
- `spec_status()` — is the stored copy current? Source, etag, fetch time, age, a
  `stale` verdict (older than 7 days) and the exact `refresh` command. It never
  refreshes anything.
- `usage_guide()` — this text, when a session starts on an unfamiliar server.

## Staleness and refreshing

The store is a snapshot, and `spec_status()` is how you see how old it is: it
reports `fetched_at`, the source `etag`, the age and `stale: true/false`. It is
read-only. Refreshing is always explicit — `swagger-mcp index --refresh <src>`
with a shell, or `index_spec(source=<src>, refresh=true)` without one. No query
triggers a read, and a stale spec keeps answering from what it has.

## Reading the output (TOON)

- Endpoint list: `METHOD /path — summary [tag1, tag2]`, one per line.
- Schema fields: `field: type` — a trailing `*` marks a **required** field, and
  `— text` after the type is the field's description. Example:
  `email*: string(email) — primary contact email`.
- `$ref(Name)` means that schema was already shown once in this response (or is
  shared across responses). Don't re-expand it unless you need its fields — and
  if you do, call `get_schema(name="Name")`.
- Schemas resolve **3 levels deep by default**. Pass `resolve_depth` (0–10) to go
  deeper on nested models, or `0` for names-only (cheapest).
- Default format is TOON (compact). Pass `format="json"` only when a consumer
  needs to parse it; `spec_status` is always JSON.

## Token rules

- Filter and page instead of dumping: `analyze_tags` → filtered
  `list_endpoints` costs roughly 30× less than an unfiltered list of everything.
- Page with `limit`/`offset` rather than `limit=0`. The header gives the next
  offset, so you never have to guess where you stopped.
- Lower `resolve_depth` (or `0`) when you only need field names, not nested
  shapes. Raise it only for the one model you are actually reading.
- `generate_types(tag=...)` once beats one call per schema.
- Don't ask for `all_specs` unless the thing you are looking for could be in
  another spec.
- Repeated calls are cheap (the store is local), so re-query instead of hoarding
  a huge response in the conversation.

## Tool reference

Every tool this server exposes, with the exact parameters it accepts. `(required)`
marks the ones a call must carry.

| Tool | Parameters | Use it for |
|------|------------|------------|
| `index_spec` | `source` (required), `name`, `refresh` | The explicit write path: index a file or URL with the same read, size cap, validation and refusals as `swagger-mcp index`, then report the name, title, version, counts and store path. The only tool that reads a source. |
| `list_specs` | `format` | The orientation call: every indexed spec with title, version, source, fetch time and counts. Answers from metadata only. |
| `list_endpoints` | `spec`, `tag`, `method`, `path_pattern`, `deprecated`, `all_specs`, `limit`, `offset`, `format` | Endpoints, one line each (`METHOD /path — summary [tags]`), filtered and paged. |
| `get_endpoint` | `spec`, `method` (required), `path` (required), `resolve_depth`, `format` | One endpoint in full: params, request body, responses, resolved schemas. |
| `get_schema` | `spec`, `name` (required), `resolve_depth`, `format` | One named schema/model with field types and descriptions — to expand a `$ref(Name)`. |
| `search_spec` | `spec`, `query` (required), `all_specs`, `limit`, `offset`, `format` | Ranked keyword search over paths, summaries, descriptions, operation ids, parameter and body field names. |
| `list_schemas` | `spec`, `filter`, `all_specs`, `limit`, `offset`, `format` | Component schemas with field count and usage (how many endpoints reference each). |
| `security_schemes` | `spec`, `format` | Auth schemes (http/bearer, apiKey, OAuth2/OIDC) and how many endpoints use each. |
| `validate_spec` | `spec`, `format` | VALID/INVALID plus the spec's real violations, as the document writes them. |
| `analyze_tags` | `spec`, `all_specs`, `format` | Tags with endpoint counts and method breakdown — the map to start exploring from. |
| `diff_endpoints` | `spec_old`, `spec_new`, `path`, `method`, `format` | Added, removed and changed endpoints between two indexed specs. Needs two. |
| `spec_status` | `spec` | Stored source, etag, fetch time, age, `stale` verdict and the exact refresh command. Read-only, always JSON. |
| `generate_types` | `spec`, `tag`, `method`, `path`, `schema`, `language`, `format` | TypeScript interfaces or Go structs from a tag, a schema, or one endpoint. |
| `usage_guide` | — | This guide. |

## Errors and what to do

Failures come back as `[CODE] message`. The message names the names, paths or
commands you need; the action column is what to do next.

| Code | When you see it | What to do |
|------|-----------------|------------|
| `NO_STORE` | No store in this project, or a store with no specs. The message names the path it looked for. | Run `swagger-mcp index <url\|file>` in the project directory (step 0), or check that the client's working directory is the project you indexed. |
| `SPEC_AMBIGUOUS` | Several specs are indexed and no `spec` was passed. The message lists them. | Call `list_specs`, then retry with `spec=<name>`, or `all_specs=true` on a tool that supports it. |
| `SPEC_NOT_FOUND` | The `spec` name is not indexed. The message lists the indexed names. | Retry with one of the listed names; if the spec you meant is missing, index it. |
| `DIFF_NEEDS_SPECS` | `diff_endpoints` was called with fewer than two specs indexed. | Index the second version under its own name (`swagger-mcp index --name billing-v2 <src>`), then diff `spec_old`/`spec_new`. |
| `INVALID_QUERY` | An argument was rejected: a call that still sends the removed url argument, or `search_spec` without `query`. | Drop the url argument — specs are indexed with `swagger-mcp index` and addressed with `spec=<name>` — and pass the required arguments. |
| `STORE_ERROR` | The store exists but cannot be opened or read: a corrupt or truncated file, a permissions problem, a store written by a newer schema version. | Read the message: a file that is not a usable database names the store path and the fix — move or delete the `swa-mcp/` directory and reindex. Never edit the file by hand. |
| `INDEX_FAILED` | `index_spec` refused a source: it cannot be read, it is over the size cap, it is not a valid OpenAPI document, or its name already belongs to a different source. The message names the source, the cap, the owner, or the validation violation. Nothing was written. | Fix the source; pass `name=<another name>` for a collision; index a smaller document for the cap. If you have a shell, `swagger-mcp index` reports the same refusal. |

## Example sequences

**"Index ./openapi.yaml without a shell"**
```
index_spec(source="./openapi.yaml")   → indexed <name>: <title> <version> (N endpoints, M schemas)
list_specs()                          → confirm the row and its fetch time
```

**"What can this API do with employees?"**
```
list_specs()                               → which spec(s) are indexed, and their counts
analyze_tags()                             → see an "Employee" tag, 40 endpoints
list_endpoints(tag="Employee")             → the 40 endpoints, one line each
get_endpoint(method="POST", path="/api/v1/Employee")
                                           → full request/response shape
```

**"Which models are central to this API?"**
```
list_schemas(limit=10)                     → schemas sorted by how many endpoints use them
get_schema(name="ProblemDetails")          → fields for the one you care about
```

**"I need to call the create-user endpoint."**
```
search_spec(query="create user")           → ranked matches
get_endpoint(method="POST", path="/users") → params + body + responses
get_schema(name="CreateUserDto")           → only if a $ref(CreateUserDto) needs expanding
security_schemes()                         → what to send: Bearer OR ApiKey, and the header name
```

**"Give me the TypeScript types for the orders tag."**
```
generate_types(tag="Orders", language="typescript")   → every schema that tag uses, sorted
```

**"Is the indexed copy current?"**
```
spec_status()   → source, etag, fetch time, age, stale yes/no; the `refresh`
                  field is the `swagger-mcp index --refresh <src>` command a
                  human (or you, with a shell) runs — never a query
```

**"Which specs does this project index?"**
```
list_specs()    → name, title, version, source, fetched_at, counts
```

## Anti-patterns

- Don't `list_endpoints` without filters on a large API — it wastes tokens.
- Don't `get_endpoint` every endpoint to "look around" — narrow with tags/search.
- Don't expand every `$ref(Name)` — only the ones whose fields you actually need.
- Don't guess a spec name — `list_specs` and `spec_status` list the indexed names.
- Don't reach for `all_specs=true` as a default — it reads the catalog of every
  stored spec and returns the most rows.
- Don't ask for a diff when only one spec is indexed — `diff_endpoints` needs two.
- Don't raise `resolve_depth` to 10 by reflex — the default of 3 is enough for
  most models, and the dedup keeps shared schemas from repeating anyway.
- Don't ask the user to run `swagger-mcp index` — run it yourself with a shell,
  or call `index_spec` when you have none.
- Don't call `index_spec` to answer a question: it writes the store. Query tools
  read it, and a call that changes nothing is better spent on `list_endpoints`.
- Don't try to repair or delete the store file to fix `STORE_ERROR` — tell the
  user the message, which already names the recovery action.
