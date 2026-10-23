# swa-mcp: local spec index as the product

Pivot the product: instead of a server that fetches specs at query time, ship a
tool that indexes specs **inside the consuming project** and serves every tool
from that local index. The MCP becomes the query layer — like CodeGraph, but for
OpenAPI.

Branch: `feat/local-index` (from `main`). The unmerged `feat/railway-deployment`
work is input, not a base: its HTTP transport and session handling only matter if
a hosted mode survives.

## Why

- **Sensitive specs never leave the machine.** Today `internal/loader/loader.go`
  fetches and caches the document in the server process; hosted, that is
  someone else's disk.
- **No saturation surface.** One hosted process holding parsed docs (LRU by
  count, `internal/loader/cache.go:21,77`) dies on variety, and N cold clients
  cause N fetches (no coalescing). A local index removes both: each project
  queries its own store, off the network.
- **Public product without hosting.** A static binary plus a per-project index
  scales to 2k users with zero infrastructure and no key to manage.

## Specs

**S1 — The tool is local-first.** No hosted service is required for any feature.
Every query is answered from the project's own store; the only network use is an
explicit (re)index of a URL.

**S2 — Store layout.** A `swa-mcp/` directory in the project root holds
`swa-mcp.db` (SQLite). The MCP discovers the store by walking up from the working
directory, and an env override (`SWA_MCP_DIR`) wins. Created by the indexer, not
by hand.

**S3 — Indexing is explicit.** `swa-mcp index <url|file>...` reads one or more
specs once, parses and validates them, and writes them to the store with source
metadata (`url`, `etag`, `fetched_at`, `sha256`, title, version, counts).
Reindexing the same source updates in place. Several specs coexist in one store
and are addressable by name.

**S4 — The MCP queries the store, never the network.** The existing tool surface
stays: it consumes a parsed document, and a document now comes from the store
instead of an HTTP fetch. With no store present, a tool returns a clear error
naming `swa-mcp index`. `url` stops being the addressing key; a `spec` argument
(plus "the only spec" default) replaces it.

**S4a — Addressing is by spec name, never by URL.** A `spec` argument selects one
indexed spec. Omitted: if the store holds exactly one, that one is used; if it
holds none, the tool returns an error naming `swagger-mcp index`; if it holds
several, the tool returns an error listing the names instead of guessing. An
unknown name returns the same list. No tool accepts `url` on a query path — that
parameter disappears with the fetch path it belonged to.

**S4b — The store is the contract, the transport is a detail.** Local (stdio) and
published (HTTP) are the same binary over the same store: local points at
`./swa-mcp/swa-mcp.db`, published points at a store on a persisted volume. No
mode may fetch a spec to answer a query; only an explicit index writes.

**S5 — Discovery spans the store.** List indexed specs, and let list/search scope
to one spec or sweep all of them.

**S6 — Staleness is visible, not silent.** `spec_status` reports `fetched_at`,
the source `etag`, and whether the stored copy is behind; refreshing is an
explicit `swa-mcp index --refresh`, never an implicit fetch during a query.

**S7 — Distribution is the product.** A single static binary per platform
(goreleaser already builds it), installed with `go install` or a release asset,
and registered per project. Hosting one instance per team is a volume plus the
same image — no auth, no key, because the store is theirs.

**S7b — Multi-tenancy is not assumed.** One SQLite file is one tenant: it has a
single writer and does not serve many unrelated users concurrently. A public
multi-tenant service would need identity (per-tenant namespace or token) and a
server-grade database, which contradicts "no key". Out of scope until asked;
self-hosted single-tenant covers the public-tool story the same way CodeGraph and
Engram do.

**S8 — Reuse what already works.** `internal/analyzer`, `extractor`, `generator`
and `formatter` operate on `*openapi3.T`; they survive intact. What changes is
`internal/loader` (fetch → store) and the tool handlers' addressing (`url` →
`spec`).

**S9 — Discovery answers from the catalog, not from the document.** `list_specs`,
`list_endpoints`, `list_schemas`, `analyze_tags` and `search_spec` must answer from
the store's normalized tables (`endpoints`, `schemas`, `tags`), so a large spec is
not decompressed and re-parsed to list or search it. Detail tools
(`get_endpoint`, `get_schema`, `generate_types`, `diff_endpoints`,
`security_schemes`, `validate_spec`) still read the stored document — that is what
they are for. A document that cannot be parsed must therefore degrade detail
tools only, never discovery. Required schema v2: the catalog gains what search
needs (operation id, body field names); an existing v1 store is migrated
automatically on the writable path and reports a clear, actionable error on the
read path instead of answering with missing results.

**S10 — Orphaned rows can be removed.** `swagger-mcp unindex <name>` deletes
one indexed spec and its catalog rows, confirms what it removed, and errors
clearly on an unknown name without touching anything else. Needed because a moved
or renamed source file leaves a row nobody can use.

**S11 — An unusable store explains the recovery.** When the store file cannot be
opened or read (corrupt, truncated, not a database), the error names the store
path and what to do about it instead of surfacing a raw driver message. A query
must keep failing safely: it never rewrites or deletes the file.

**S12 — The guide teaches the real workflow.** `usage_guide` (the embedded
`llm_guide.md`) must describe how this MCP is used now, end to end: index first
(the agent runs it itself when it has a shell), `list_specs`, `spec` addressing
and `all_specs`, staleness, and an error→action table for the codes a caller can
hit. Every tool in the registry must appear in it, and a test must fail when the
guide and the tool inventory drift apart.

**Non-goals**: hosted multi-tenant service; authentication; embeddings/vector
search; watching the filesystem for spec changes (an explicit reindex is enough
until someone asks).

## Design decision: what the store holds

Two shapes were considered. Chosen: **blob + normalized catalog**.

- `specs` — one row per indexed source: name, source, etag, sha256, fetched_at,
  title, version, endpoint/schema counts, plus the raw document (compressed).
- `endpoints`, `schemas`, `tags` — the discovery index: method/path/summary/tag/
  deprecated, schema names with field and usage counts, so list/search/analyze
  are SQL, not a parse.
- Detail queries (`get_endpoint`, `get_schema`, `generate_types`) load that one
  stored document and reuse the current extractor. Reimplementing kin-openapi's
  `$ref` resolution over normalized tables would be a rewrite with no user-visible
  gain.

## Tasks

| ID | Spec | Route | Commit |
|----|------|-------|--------|
| T1 | S2,S3 | done | `ba66e1e feat(store): sqlite spec index and the index command` |
| T2 | S4,S4a | done | `da8822b feat(tools): answer queries from the local spec store` |
| T3 | S5 | done | `8b76ed7 feat(tools): list_specs, cross-spec sweeps and index staleness` |
| T4 | S6 | done | (shipped with T3 in `8b76ed7`) |
| T5 | S7 | done | `eb20fd7 docs: rewrite the README for the local index product` |
| T6 | S1 | done | `d379c44 refactor(loader): delete the dead fetch path` |
| T7 | S9 | done | `87e2eff refactor(store): answer discovery from the catalog` |
| T8 | S10,S11 | in progress | `feat(store): unindex and a recovery hint` |
| T9 | S12 | in progress | `docs(guide): teach the real workflow` |

Slice boundary: slice 1 = `ba66e1e` + `60e8a49` (store, index command, validation
parity); slice 2 = `da8822b` (store-backed queries); slice 3 = `8b76ed7`
(list_specs, sweeps, staleness); slice 4 = `d379c44` + `eb20fd7` (dead fetch path
deleted, README rewritten).

Risk: item 1 (data) on T1 — a new on-disk format and a writer; item 2 (security)
on T2 — dropping URL addressing removes the SSRF surface entirely, so the check is
that nothing can still be fetched from a query path.

## Scope confirmation

L4 fixes the product shape: **local stores, zero server**. 2k users means 2k
project stores, each indexed by its owner. The hosted HTTP mode is not part of
this product now; the unmerged `feat/railway-deployment` branch stays parked as
reference and is not merged into this feature. Distribution is the release binary
plus the index command (S7).

## Decision taken

L2 settles the earlier open question: the store is how specs are stored and read
in **both** modes, so query-time fetching is not a supported mode. The loader's
fetch path moves out of the query path entirely; its HTTP code survives only
inside the indexer, which is an explicit write. Task T1 starts on that basis.

## Log

**L1** (user, verbatim): "yo queria construir algo publico, que el swagger mcp
sea una herramienta, la data puede vivir en el proyecto que lo consulten un
directorio swa-mcp y un sqlite nose algo asi pero que el mcp sea para traducir
todo eso como un codegraph pero para el swagger"

**L2** (user, verbatim): "es que siempre el mcp publicado deberia de usar ese
sqlite para guardar consultar y demas, pero el mcp sera el traductos de eso me
explico ? ya sea el local o el publiucado, vaya como hace codegraph o engram"

**L4** (user, verbatim): "esto es lo que busco,"

**L3** (author): reading of L2 — the SQLite store is the single source of truth
for storing and querying in both modes; the MCP is only the translator in front
of it. The transport (stdio/HTTP) does not change the data model. Multi-tenant
public hosting is explicitly not implied (S7b).

**L10** (evidence, this session):
- T3/T4 verified by smoke, not just tests: `list_specs` → `# 1 spec indexed`,
  `list_endpoints {all_specs:true}` → `# 5 endpoints across 1 spec` with
  `petstore:` prefixes, `diff_endpoints {}` → `DIFF_NEEDS_SPECS`, `spec_status`
  → age + `stale:false` + the exact refresh command. Fixed the sweep header's
  `across 1 specs` pluralisation.
- T5: README rewritten and cross-checked against `internal/tools/tools.go` by
  diffing every `WithString|Boolean|Number` against the documented table (sets
  match). Restored the literal `fetch_spec` / `SWAGGER_MCP_DEFAULT_URL` names in
  the migration note: a migration note that hides the old identifiers is useless,
  so the grep rule was wrong there and the note wins. Verified in code that
  `spec_status` really is always JSON (`formatter.FormatJSON`, tools.go:783)
  before the README claims it.
- T6: ~3.4k lines deleted (`cache.go`, `diskcache.go`, `progress.go`, most of
  `loader.go`, their tests) plus dead config fields and env vars; only
  `ParseDocument`, `PrepareDocument`, `ValidateDocument` remain. An AST guard
  (`nonetwork_test.go`) fails if the loader imports `net/http`/`net` or exports a
  function taking a url. Only remaining `net/http` import in non-test code is
  `cmd/swagger-mcp/index.go`, the explicit indexer read. Also removed dead types
  (`CacheEntry`, `DiskCacheMeta`, `DiskStats`, `SpecStatus`) that the deletion
  orphaned.
- Whole feature: 10 packages green with `CGO_ENABLED=0`, 13 tools, no query path
  can fetch.

**L11** (user, verbatim): "si si vamos con eso pls, todo lo que requiera ahorita, y te desbloqueo algo, en railway solo vamos a tener el servicio puro, sin volumen, no es la idea tener una db en el servidor,"

**L12** (author): reading of L11 — T7 (S9) proceeds now. The Railway note is a
constraint for a later decision, recorded here so it is not lost: with no volume
on the server there is no store to read, so a hosted mode could only work if the
store or its documents arrive from the client per request. Not in scope today.

**L13** (evidence, this session):
- T7 verified independently, not just by the worker: after corrupting the stored
  blob by hand, `list_endpoints` still answered from the catalog (5 endpoints)
  while `get_endpoint` reported `[STORE_ERROR]` naming the repair command.
- Parity: 49 answers captured from the pre-change implementation before the
  switch, all identical afterwards.
- Benchmarks (50 runs): `list_endpoints` 698µs → 94µs on the petstore fixture and
  23.4ms → 0.53ms on the 130-endpoint fixture (45x).
- Schema v2 migrates a v1 store on the writable path (blobs, metadata and
  `fetched_at` untouched) and refuses on the read path with the command to run.
- Module path corrected to `github.com/hnkatze/swagger-extractor-mcp` (`be1b9cc`),
  because `go install` resolves the module path as the repository URL: the
  published v0.8.0 README promised an install that could never work.
- Released with T7 as v0.8.1 once pushed; the doc's follow-ups stay open:
  `unindex` for orphaned rows, a recovery hint for a corrupt SQLite file.

**L14** (user, verbatim): "dale con eso resolvamos esas cosas, y que la tool de
usage exponga el uso correcto del mcp"

**L15** (author): reading of L14 — the follow-ups (unindex, recovery hint) and a
real `usage_guide` are in scope; `index_spec` stays out because the user runs the
agent locally with a shell (the guide now says so). Publishing stays parked.
