# MCP capability upgrades

Ten improvements to swagger-mcp's tools, approved in bulk: two bugs, four
discovery gaps, the structural fix for slow fetches, and three capabilities that
matter when the goal is building a client against a large spec.

Branch: `feat/mcp-capability-upgrades`. Baseline: the verified fetch-timeout fix
(`SWAGGER_MCP_FETCH_TIMEOUT` + 120s default + client `timeout: 180`).

## Specs

**S1 — Numeric and boolean params must not be declared as strings.**
Author's words: "Los parámetros numéricos y booleanos son strings. Todos los `limit`,
`resolve_depth`, `refresh` están declarados `mcp.WithString(...)` en
`internal/tools/tools.go`. mcp-go tiene `WithNumber`/`WithBoolean`."
Requirement: `limit`, `offset`, `resolve_depth` declared as numbers; `refresh` and
`deprecated` as booleans. Handlers must keep accepting the old string forms
(`"50"`, `"true"`) so existing clients and `usage_guide` text do not break.

**S2 — Version rendering must not double the prefix.**
Author's words: "`internal/formatter/toon.go:374-376` hace `" v" + version` sin chequear
si la versión ya trae la `v`. En la salida real de hoy: `spec: BipBip Api backoffice vv1`."
Requirement: render `BipBip Api backoffice v1` when `info.version` is `"v1"`, and
`... v1.2.3` when it is `"1.2.3"`.

**S3 — Schemas must be discoverable.**
Author's words: "`fetch_spec` te dice `schema_count: 1025`, pero esos 1025 son invisibles:
solo los descubrís chocándote con un `$ref(Name)` en un endpoint y adivinando el nombre en
`get_schema`."
Requirement: a `list_schemas` tool listing component schema names with a field
count and a usage count (how many endpoints reference the schema), supporting
`filter` and `limit`/`offset`.

**S4 — `url` must be avoidable per session.**
Author's words: "`url` obligatoria en las 11 tools ... Un `SWAGGER_MCP_DEFAULT_URL` +
`fetch_spec` que la fije como activa lo borra."
Requirement: `SWAGGER_MCP_DEFAULT_URL` env sets the starting active URL;
`fetch_spec` makes the fetched URL active for the rest of the session; every other
tool treats `url` as optional and uses the active one. A tool call with no URL
anywhere returns a clear error naming `fetch_spec`.

**S5 — `deprecated` must be filterable.**
Author's words: "`EndpointSummary.Deprecated` existe y sale en la salida, pero
`list_endpoints` solo filtra `tag`/`method`/`path_pattern`."
Requirement: `list_endpoints` accepts `deprecated=true|false`; absent means both.

**S6 — Results must page.**
Author's words: "`limit: 50` + `# showing 50 of 840` y no hay forma de pedir el bloque
siguiente salvo angostar filtros."
Requirement: `list_endpoints` and `search_spec` accept `offset`; the header reports
the absolute range (`# showing 51-100 of 840`).

**S7 — Slow fetches must report progress instead of blocking silently.**
Author's words: "La solución de verdad es que el server mande progress notifications
durante el fetch. Pi resetea su timeout con cada notificación ... un heartbeat cada
5-10s vuelve el `timeout: 180` innecesario."
Requirement: when the incoming request carries a progress token, the loader emits
`notifications/progress` every 5s while fetching. No token, no notifications, and no
behavior change.

**S8 — `generate_types` must work at tag granularity.**
Author's words: "`generate_types` por tag o paquete. Hoy es un schema o un endpoint.
Para el backoffice querés `generate_types(tag: "Autenticacion y Acceso")` → el paquete
entero de una."
Requirement: `generate_types` accepts `tag`, emitting every distinct schema reachable
from that tag's endpoints, deduplicated.

**S9 — Security scheme definitions must be exposed.**
Author's words: "`EndpointDetail.Security` te dice `[{bearerAuth: []}]`, pero la
*definición* del scheme (tipo, scheme, flow, URL de OIDC) nunca se expone — no está en
`types.go`."
Requirement: a `security_schemes` tool returning each scheme's type/scheme/bearer
format/flow URLs plus the endpoints that use it, and whether an endpoint requires auth
when the operation overrides global security.

**S10 — Validation must be reportable, not only relaxed.**
Author's words: "`validate_spec` que reporte errores reales en vez de solo relajar la
validación: útil para el que escribe el spec."
Requirement: a `validate_spec` tool returning a pass/fail verdict plus the list of
violations, using the same relaxed rules the loader uses so it cannot disagree with
loading.

**Non-goals** (author's words): "Un tool que ejecute requests HTTP", "Búsqueda
vectorial / embeddings", "Más tools ... preferiría parámetros nuevos en las existentes
antes que tools nuevas" — the three genuinely new tools authorized here are
`list_schemas`, `security_schemes`, and `validate_spec`; everything else is params.

## Tasks

| ID | Spec | Route | Commit |
|----|------|-------|--------|
| T0 | baseline | inline | `fix(config): make fetch timeout configurable` |
| T1 | S1,S2 | inline | `fix(tools): real param types + version prefix` |
| T2 | S3,S5,S6 | inline | `feat(tools): list_schemas, deprecated filter, offset paging` |
| T3 | S4 | inline | `feat(tools): session default URL` |
| T4 | S9 | inline | `feat(tools): security_schemes tool` |
| T5 | S8 | inline | `feat(generator): generate_types by tag` |
| T6 | S10 | inline | `feat(tools): validate_spec tool` |
| T7 | S7 | inline | `feat(loader): progress heartbeats during fetch` |

All tasks route inline: the units overlap in `internal/tools/tools.go`,
`internal/types/types.go`, and `internal/formatter/toon.go`, so parallel writers
would have colliding edit surfaces.

## Log

**L1** (user, verbatim): "si si, dale co todas las mejoras propuestas plp"

**L2** (user, verbatim, the proposal this authorizes): "**Mi orden si tuviera que
elegir**: `list_schemas` → `url` por defecto → progress notifications →
`deprecated`/`offset` → `generate_types` por tag. Los dos primeros son una tarde; el
tercero es el único que toca el hot path y merece su propio test."

**L3** (author): scope decision — all ten items proceed as separate work-unit
commits; T7 lands last and alone because it touches the fetch hot path.
