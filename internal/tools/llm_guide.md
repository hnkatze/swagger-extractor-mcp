# swagger-mcp — usage guide

This server lets you explore an OpenAPI/Swagger spec without loading the whole
document into context. It is built to minimize tokens: responses are compact
(TOON), lists are auto-limited, and repeated schemas are de-duplicated. Follow
the workflow below to stay efficient.

## Workflow

1. `fetch_spec` — once per spec URL. Returns title/version/counts. Establishes
   the cache so later calls are instant.
2. `analyze_tags` — **always start here**. Maps the API into tags with endpoint
   counts so you know where to look.
3. `list_endpoints` — filter by `tag` (preferred), `method`, or `path_pattern`.
   Never browse unfiltered on a large API.
4. `get_endpoint` — full detail for one endpoint: params, request body,
   responses, and resolved schemas.
5. `get_schema` — only to expand a `$ref(Name)` you saw in `get_endpoint` and
   whose fields you actually need.
6. `generate_types` — emit ready-to-paste TypeScript interfaces or Go structs
   instead of translating schemas by hand.

`search_spec` is the shortcut when you already know a keyword (it searches
paths, summaries, operation IDs, params, and body field names).

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

- Filter before listing. `analyze_tags` → filtered `list_endpoints` beats one
  unfiltered dump by ~30×.
- Reuse the cache. Don't re-`fetch_spec` a URL you already loaded; check
  `spec_status` if unsure.
- Default format is TOON (compact). Only pass `format=json` when a tool consumer
  needs structured JSON.
- Lower `resolve_depth` (or 0) when you only need field names, not nested shapes.

## Example sequences

**"What can this API do with employees?"**
```
analyze_tags(url)                          → see an "Employee" tag, 40 endpoints
list_endpoints(url, tag="Employee")        → the 40 endpoints, one line each
get_endpoint(url, "POST", "/api/v1/Employee")  → full request/response shape
```

**"I need to call the create-user endpoint."**
```
search_spec(url, query="create user")      → ranked matches
get_endpoint(url, "POST", "/users")        → params + body + responses
get_schema(url, "CreateUserDto")           → only if a $ref(CreateUserDto) needs expanding
```

**"Give me the TypeScript types for the orders response."**
```
generate_types(url, method="GET", path="/orders", language="typescript")
```

**"Did the spec change since last time?"**
```
refresh_spec(url)   → reports changed/unchanged via fingerprint, plus a fresh summary
```

## Anti-patterns

- Don't `list_endpoints` without filters on a large API — it wastes tokens.
- Don't `get_endpoint` every endpoint to "look around" — narrow with tags/search.
- Don't expand every `$ref(Name)` — only the ones whose fields you actually need.
- Don't re-fetch a cached spec — use `spec_status` to check first.
- Don't raise `resolve_depth` to 10 by reflex — the default of 3 is enough for
  most models, and the dedup keeps shared schemas from repeating anyway.
