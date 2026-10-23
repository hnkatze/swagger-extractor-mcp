# swagger-mcp — usage guide

This server lets you explore an OpenAPI/Swagger spec without loading the whole
document into context. It is built to minimize tokens: responses are compact
(TOON), lists are auto-limited, and repeated schemas are de-duplicated. Follow
the workflow below to stay efficient.

Every answer comes from the project's **local spec index** — a `swa-mcp/` store
written by the CLI, never fetched from the network. Index a spec once, in the
project:

```
swagger-mcp index <url|file>...     # writes ./swa-mcp/swa-mcp.db
swagger-mcp index --refresh <src>   # re-read and rewrite the stored copy
```

Then every tool answers from that store. If nothing is indexed, the tools tell
you to run `swagger-mcp index`.

## Addressing the spec (`spec`)

- One indexed spec: omit `spec`, the tools use it automatically.
- Several indexed specs: pass `spec=<name>`, the name the indexer stored (by
  default the file or URL base name, e.g. `petstore`).
- `spec_status` shows what is indexed: name, source, etag, sha256, fetch time and
  counts. Start there when a call reports that several specs are indexed.

## Workflow

0. `spec_status` — which specs are indexed, their source and fetch time.
1. `analyze_tags` — maps the API into tags with endpoint counts so you know where
   to look.
2. `list_endpoints` — filter by `tag` (preferred), `method`, `path_pattern`, or
   `deprecated`. Page with `limit`/`offset`; the header shows the range and the
   next offset.
3. `get_endpoint` — full detail for one endpoint: params, request body,
   responses, and resolved schemas.
4. `get_schema` — only to expand a `$ref(Name)` you saw in `get_endpoint` and
   whose fields you actually need.
5. `generate_types` — emit ready-to-paste TypeScript interfaces or Go structs
   instead of translating schemas by hand. Pass `tag` to get every schema a tag
   uses in one call instead of one call per model.

`search_spec` is the shortcut when you already know a keyword (it searches
paths, summaries, operation IDs, params, and body field names).

`list_schemas` is how you discover models: it lists component schemas with field
count and how many endpoints reference each, so the core models surface first
without you guessing names.

`security_schemes` tells you how to authenticate: each scheme's type, bearer
format, apiKey header name or OAuth2/OIDC URLs, and how many endpoints use it.
Call it before writing client code. A requirement list is an OR — `Bearer OR
ApiKey` means either one works, not both.

`validate_spec` reports what is wrong with an indexed spec (VALID/INVALID plus
violations). Use it when you are authoring or reviewing a spec, not when
consuming one.

`diff_endpoints` compares two indexed specs (`spec_old`, `spec_new`).

## Reading the output (TOON)

- Endpoint list: `METHOD /path — summary [tag1, tag2]`, one per line.
- Schema fields: `field: type` — a trailing `*` marks a **required** field, and
  `— text` after the type is the field's description. Example:
  `email*: string(email) — primary contact email`.
- `$ref(Name)` means that schema was already shown once in this response (or is
  shared across responses). Don't re-expand it unless you need its fields — and
  if you do, call `get_schema Name`.
- Schemas resolve **3 levels deep by default**. Pass `resolve_depth` (0–10) to
  go deeper on nested models, or `0` for names-only (cheapest).

## Token-efficiency rules

- Omit `spec` when the project indexes one spec. Repeating a spec name on every
  call is pure waste, and on a single-spec project it is never needed.
- Filter before listing. `analyze_tags` → filtered `list_endpoints` beats one
  unfiltered dump by ~30×.
- Page instead of raising the limit. `limit`/`offset` beats `limit=0` on a large
  API, and the header tells you the next offset.
- Trust the store. Queries never touch the network, so there is nothing to
  re-fetch and no cache to warm; repeated calls are cheap.
- Default format is TOON (compact). Only pass `format=json` when a tool consumer
  needs structured JSON.
- Lower `resolve_depth` (or 0) when you only need field names, not nested shapes.
- Use `generate_types(tag=...)` once instead of one call per schema.

## Example sequences

**"What can this API do with employees?"**
```
spec_status()                              → which spec is indexed, and its counts
analyze_tags()                             → see an "Employee" tag, 40 endpoints
list_endpoints(tag="Employee")             → the 40 endpoints, one line each
get_endpoint("POST", "/api/v1/Employee")   → full request/response shape
```

**"Which models are central to this API?"**
```
list_schemas(limit=10)                     → schemas sorted by how many endpoints use them
get_schema("ProblemDetails")               → fields for the one you care about
```

**"I need to call the create-user endpoint."**
```
search_spec(query="create user")           → ranked matches
get_endpoint("POST", "/users")            → params + body + responses
get_schema("CreateUserDto")                → only if a $ref(CreateUserDto) needs expanding
security_schemes()                         → what to send: Bearer OR ApiKey, and the header name
```

**"Give me the TypeScript types for the orders tag."**
```
generate_types(tag="Orders", language="typescript")   → every schema that tag uses, sorted
```

**"Is the indexed copy current?"**
```
spec_status()   → source, etag, fetch time and age; `swagger-mcp index --refresh <src>`
                  is how a user updates it, never a query
```

## Anti-patterns

- Don't `list_endpoints` without filters on a large API — it wastes tokens.
- Don't `get_endpoint` every endpoint to "look around" — narrow with tags/search.
- Don't expand every `$ref(Name)` — only the ones whose fields you actually need.
- Don't guess a spec name — `spec_status` lists the indexed names.
- Don't raise `resolve_depth` to 10 by reflex — the default of 3 is enough for
  most models, and the dedup keeps shared schemas from repeating anyway.
