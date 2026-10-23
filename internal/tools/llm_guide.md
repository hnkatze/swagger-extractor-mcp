# swagger-mcp — usage guide

This server lets you explore an OpenAPI/Swagger spec without loading the whole
document into context. It is built to minimize tokens: responses are compact
(TOON), lists are auto-limited, and repeated schemas are de-duplicated. Follow
the workflow below to stay efficient.

## Workflow

0. `fetch_spec` — once per spec. It returns title/version/counts **and makes this
   the session default**, so every later call can omit `url` entirely.
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

`validate_spec` reports what is wrong with a spec (VALID/INVALID plus
violations). Use it when you are authoring or reviewing a spec, not when
consuming one.

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

- Omit `url`. After the first `fetch_spec` every call inherits the session
  default; repeating a 70-character URL on every call is pure waste.
- Filter before listing. `analyze_tags` → filtered `list_endpoints` beats one
  unfiltered dump by ~30×.
- Page instead of raising the limit. `limit`/`offset` beats `limit=0` on a large
  API, and the header tells you the next offset.
- Reuse the cache. Don't re-`fetch_spec` a URL you already loaded; check
  `spec_status` if unsure.
- Default format is TOON (compact). Only pass `format=json` when a tool consumer
  needs structured JSON.
- Lower `resolve_depth` (or 0) when you only need field names, not nested shapes.
- Use `generate_types(tag=...)` once instead of one call per schema.

## Example sequences

**"What can this API do with employees?"**
```
fetch_spec(url)                            → summary + sets the session default
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

**"Did the spec change since last time?"**
```
refresh_spec()   → reports changed/unchanged via fingerprint, plus a fresh summary
```

## Anti-patterns

- Don't `list_endpoints` without filters on a large API — it wastes tokens.
- Don't `get_endpoint` every endpoint to "look around" — narrow with tags/search.
- Don't expand every `$ref(Name)` — only the ones whose fields you actually need.
- Don't re-fetch a cached spec — use `spec_status` to check first.
- Don't raise `resolve_depth` to 10 by reflex — the default of 3 is enough for
  most models, and the dedup keeps shared schemas from repeating anyway.
