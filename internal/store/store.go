package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is the on-disk layout version, stored in PRAGMA user_version.
// Bump it together with a migration whenever a table changes.
//
//	v1  endpoint/schema/tag counts only: discovery still parsed the document
//	v2  the catalog carries what discovery answers: operation id, description,
//	    body field names, tag descriptions and per-tag method breakdown (S9)
const SchemaVersion = 2

// ErrNotFound is returned when a name is not indexed in this store.
var ErrNotFound = errors.New("spec is not indexed")

// ErrCatalogIncomplete is returned by the discovery queries when the catalog
// rows of a spec were not derived from the document the store holds: a v1 store
// that could not be migrated because its stored document does not parse, or a
// store written by a build that failed halfway. Answering from such a catalog
// would silently report zero endpoints, so the query fails instead and the tool
// layer names the command that rebuilds it.
var ErrCatalogIncomplete = errors.New("the discovery catalog was not built from the stored document")

// specsSchema is one row per indexed source: its metadata, the compressed
// document and the fingerprint of the document the catalog was derived from.
// catalog_sha256 equal to sha256 is what proves the catalog is current.
const specsSchema = `CREATE TABLE IF NOT EXISTS specs (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		name           TEXT    NOT NULL UNIQUE,
		source         TEXT    NOT NULL UNIQUE,
		etag           TEXT    NOT NULL DEFAULT '',
		sha256         TEXT    NOT NULL,
		fetched_at     TEXT    NOT NULL,
		title          TEXT    NOT NULL DEFAULT '',
		version        TEXT    NOT NULL DEFAULT '',
		endpoint_count INTEGER NOT NULL DEFAULT 0,
		schema_count   INTEGER NOT NULL DEFAULT 0,
		catalog_sha256 TEXT    NOT NULL DEFAULT '',
		document       BLOB    NOT NULL
	) STRICT`

// catalogSchema is the discovery catalog. Every statement is idempotent, so
// creating it is safe on an existing store, and it is exactly the set of tables
// the v1 migration re-derives from the stored documents.
var catalogSchema = []string{
	`CREATE TABLE IF NOT EXISTS endpoints (
		spec_id      INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		method       TEXT    NOT NULL,
		path         TEXT    NOT NULL,
		summary      TEXT    NOT NULL DEFAULT '',
		description  TEXT    NOT NULL DEFAULT '',
		operation_id TEXT    NOT NULL DEFAULT '',
		deprecated   INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, method, path)
	) STRICT`,
	`CREATE INDEX IF NOT EXISTS idx_endpoints_path ON endpoints(path)`,
	`CREATE INDEX IF NOT EXISTS idx_endpoints_method ON endpoints(method)`,
	`CREATE INDEX IF NOT EXISTS idx_endpoints_spec_path ON endpoints(spec_id, path, method)`,
	// One row per (endpoint, tag) keeps the tag filter an exact match instead of
	// a LIKE over a joined string, and pos preserves the order the document
	// listed the tags in, which is part of the list_endpoints output.
	`CREATE TABLE IF NOT EXISTS endpoint_tags (
		spec_id INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		method  TEXT    NOT NULL,
		path    TEXT    NOT NULL,
		pos     INTEGER NOT NULL,
		tag     TEXT    NOT NULL,
		PRIMARY KEY (spec_id, method, path, tag)
	) STRICT`,
	`CREATE INDEX IF NOT EXISTS idx_endpoint_tags_tag ON endpoint_tags(spec_id, tag)`,
	// The field names search_spec matches on, per endpoint and category:
	// 'parameter', 'request_body' or 'response_body'. Keeping them as rows makes
	// the substring test exact instead of a match across a joined string.
	`CREATE TABLE IF NOT EXISTS endpoint_fields (
		spec_id INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		method  TEXT    NOT NULL,
		path    TEXT    NOT NULL,
		kind    TEXT    NOT NULL,
		name    TEXT    NOT NULL,
		PRIMARY KEY (spec_id, method, path, kind, name)
	) STRICT`,
	`CREATE TABLE IF NOT EXISTS schemas (
		spec_id     INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		name        TEXT    NOT NULL,
		field_count INTEGER NOT NULL DEFAULT 0,
		used_by     INTEGER NOT NULL DEFAULT 0,
		description TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (spec_id, name)
	) STRICT`,
	`CREATE TABLE IF NOT EXISTS tags (
		spec_id        INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		name           TEXT    NOT NULL,
		description    TEXT    NOT NULL DEFAULT '',
		endpoint_count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, name)
	) STRICT`,
	`CREATE TABLE IF NOT EXISTS tag_methods (
		spec_id        INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		tag            TEXT    NOT NULL,
		method         TEXT    NOT NULL,
		endpoint_count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, tag, method)
	) STRICT`,
}

// catalogTables is every table the catalog is made of, in the order the write
// path empties and fills them.
var catalogTables = []string{"endpoints", "endpoint_tags", "endpoint_fields", "schemas", "tags", "tag_methods"}

// v1CatalogObjects are the catalog objects version 1 created. The migration
// drops them before creating the v2 catalog: the v1 columns cannot carry the
// discovery answers, so the rows are rebuilt rather than altered.
var v1CatalogObjects = []string{
	"DROP INDEX IF EXISTS idx_endpoints_path",
	"DROP INDEX IF EXISTS idx_endpoints_method",
	"DROP TABLE IF EXISTS endpoints",
	"DROP TABLE IF EXISTS schemas",
	"DROP TABLE IF EXISTS tags",
}

// Store is an open local index.
type Store struct {
	dir  string
	path string
	db   *sql.DB
}

// Open opens (creating when missing) the store in dir. The directory and the
// database file are created by the indexer, never by hand. Opening for writing
// also migrates an older store (S9).
func Open(dir string) (*Store, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve store directory %s: %w", dir, err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("create store directory %s: %w", absDir, err)
	}
	path := filepath.Join(absDir, DBFileName)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}
	// One writer per store (S7b): a single connection keeps the per-connection
	// pragmas below in effect and serializes writes.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("prepare store %s (%s): %w", path, pragma, err)
		}
	}

	s := &Store{dir: absDir, path: path, db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Dir is the store directory (the swa-mcp directory holding the database).
func (s *Store) Dir() string { return s.dir }

// Path is the database file path.
func (s *Store) Path() string { return s.path }

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// migrate creates the schema, upgrades an older one, or refuses a newer one.
// It is the writable path only: a query never migrates (S1, S3, S9).
func (s *Store) migrate() error {
	version, err := s.schemaVersion()
	if err != nil {
		return err
	}
	switch {
	case version == SchemaVersion:
		return nil
	case version > SchemaVersion:
		return newerSchemaError(s.path, version)
	case version == 1:
		return s.upgradeV1()
	case version == 0:
		return s.createSchema()
	default:
		return fmt.Errorf("store %s uses unknown schema version %d: run `swagger-mcp index <url|file>` on a new store directory", s.path, version)
	}
}

// createSchema writes the current layout into an empty store.
func (s *Store) createSchema() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin schema creation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(specsSchema); err != nil {
		return fmt.Errorf("create schema in %s: %w", s.path, err)
	}
	for _, stmt := range catalogSchema {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("create schema in %s: %w", s.path, err)
		}
	}
	if err := setUserVersion(tx, SchemaVersion); err != nil {
		return err
	}
	return tx.Commit()
}

// upgradeV1 migrates a version-1 store in place (S9): the layout gains the
// catalog columns discovery needs and the catalog is re-derived from the
// documents the store already holds, so nothing is lost and no manual reindex is
// needed. A document that does not parse leaves its spec's catalog empty and
// marked with an empty catalog_sha256: the upgrade still succeeds, because
// `swagger-mcp index` — the only command that can rewrite that document — has to
// be able to open the store, and the discovery queries report the marked spec
// instead of answering from an empty catalog.
func (s *Store) upgradeV1() error {
	type storedSpec struct {
		id   int64
		name string
		sha  string
		blob []byte
	}
	var specs []storedSpec

	rows, err := s.db.Query("SELECT id, name, sha256, document FROM specs ORDER BY id")
	if err != nil {
		return fmt.Errorf("migrate %s: read stored specs: %w", s.path, err)
	}
	for rows.Next() {
		var spec storedSpec
		if err := rows.Scan(&spec.id, &spec.name, &spec.sha, &spec.blob); err != nil {
			rows.Close()
			return fmt.Errorf("migrate %s: read stored specs: %w", s.path, err)
		}
		specs = append(specs, spec)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("migrate %s: read stored specs: %w", s.path, err)
	}
	rows.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration of %s: %w", s.path, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("ALTER TABLE specs ADD COLUMN catalog_sha256 TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("migrate %s: add catalog fingerprint: %w", s.path, err)
	}
	for _, stmt := range v1CatalogObjects {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migrate %s: drop the v1 catalog: %w", s.path, err)
		}
	}
	for _, stmt := range catalogSchema {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migrate %s: create the v2 catalog: %w", s.path, err)
		}
	}

	for _, spec := range specs {
		rec, err := reparse(spec.name, spec.blob)
		if err != nil {
			// Reported per spec by the discovery queries; the migration itself
			// must not strand the store.
			continue
		}
		if err := writeCatalog(tx, spec.id, rec); err != nil {
			return fmt.Errorf("migrate %s: rebuild the catalog of %q: %w", s.path, spec.name, err)
		}
		if _, err := tx.Exec(
			"UPDATE specs SET catalog_sha256 = ?, endpoint_count = ?, schema_count = ? WHERE id = ?",
			spec.sha, len(rec.Endpoints), len(rec.Schemas), spec.id,
		); err != nil {
			return fmt.Errorf("migrate %s: record the catalog fingerprint of %q: %w", s.path, spec.name, err)
		}
	}

	if err := setUserVersion(tx, SchemaVersion); err != nil {
		return err
	}
	return tx.Commit()
}

// Put writes one spec: the source metadata, the compressed document and the
// normalized catalog. A row with the same name is updated in place, and the
// same source never occupies two rows, so reindexing cannot duplicate a spec.
func (s *Store) Put(rec Record) error {
	if rec.Name == "" {
		return errors.New("spec name is required")
	}
	if rec.Source == "" {
		return fmt.Errorf("spec %q has no source", rec.Name)
	}
	if rec.FetchedAt.IsZero() {
		rec.FetchedAt = time.Now().UTC()
	}
	blob, err := compress(rec.Document)
	if err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}
	defer tx.Rollback()

	// A name addresses exactly one spec: taking over the name of another source
	// would delete that spec from the index, so it is refused and the caller is
	// told how to store this one under a name of its own.
	var existingSource string
	switch err := tx.QueryRow("SELECT source FROM specs WHERE name = ?", rec.Name).Scan(&existingSource); {
	case err == nil && existingSource != rec.Source:
		return fmt.Errorf("cannot index %s as %q: that name already belongs to %s; pass --name to index this spec under a different name",
			rec.Source, rec.Name, existingSource)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}

	// A source addresses exactly one row: indexing it under a new name replaces
	// the old row instead of leaving a duplicate behind.
	if _, err := tx.Exec("DELETE FROM specs WHERE source = ? AND name <> ?", rec.Source, rec.Name); err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}
	if _, err := tx.Exec(`
		INSERT INTO specs (name, source, etag, sha256, fetched_at, title, version,
			endpoint_count, schema_count, catalog_sha256, document)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			source         = excluded.source,
			etag           = excluded.etag,
			sha256         = excluded.sha256,
			fetched_at     = excluded.fetched_at,
			title          = excluded.title,
			version        = excluded.version,
			endpoint_count = excluded.endpoint_count,
			schema_count   = excluded.schema_count,
			catalog_sha256 = excluded.catalog_sha256,
			document       = excluded.document`,
		rec.Name, rec.Source, rec.ETag, rec.SHA256, rec.FetchedAt.UTC().Format(time.RFC3339Nano),
		rec.Title, rec.Version, len(rec.Endpoints), len(rec.Schemas), rec.SHA256, blob,
	); err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}

	var id int64
	if err := tx.QueryRow("SELECT id FROM specs WHERE name = ?", rec.Name).Scan(&id); err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}

	// The catalog is derived from the document, so replace it wholesale.
	if err := writeCatalog(tx, id, rec); err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}
	return nil
}

// writeCatalog replaces the catalog rows of one spec. It is shared by Put and by
// the v1 migration so an upgraded store and a freshly indexed one are built from
// the same derivation, never from two subtly different ones.
func writeCatalog(tx *sql.Tx, id int64, rec Record) error {
	for _, table := range catalogTables {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE spec_id = ?", id); err != nil {
			return err
		}
	}
	for _, ep := range rec.Endpoints {
		if _, err := tx.Exec(
			`INSERT INTO endpoints (spec_id, method, path, summary, description, operation_id, deprecated)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, ep.Method, ep.Path, ep.Summary, ep.Description, ep.OperationID, boolInt(ep.Deprecated),
		); err != nil {
			return err
		}
		for pos, tag := range ep.Tags {
			if _, err := tx.Exec(
				"INSERT OR IGNORE INTO endpoint_tags (spec_id, method, path, pos, tag) VALUES (?, ?, ?, ?, ?)",
				id, ep.Method, ep.Path, pos, tag,
			); err != nil {
				return err
			}
		}
		for kind, names := range ep.fields() {
			for _, name := range names {
				if _, err := tx.Exec(
					"INSERT OR IGNORE INTO endpoint_fields (spec_id, method, path, kind, name) VALUES (?, ?, ?, ?, ?)",
					id, ep.Method, ep.Path, kind, name,
				); err != nil {
					return err
				}
			}
		}
	}
	for _, sc := range rec.Schemas {
		if _, err := tx.Exec(
			"INSERT INTO schemas (spec_id, name, field_count, used_by, description) VALUES (?, ?, ?, ?, ?)",
			id, sc.Name, sc.FieldCount, sc.UsedBy, sc.Description,
		); err != nil {
			return err
		}
	}
	for _, tg := range rec.Tags {
		if _, err := tx.Exec(
			"INSERT INTO tags (spec_id, name, description, endpoint_count) VALUES (?, ?, ?, ?)",
			id, tg.Name, tg.Description, tg.EndpointCount,
		); err != nil {
			return err
		}
		for method, count := range tg.MethodBreakdown {
			if _, err := tx.Exec(
				"INSERT INTO tag_methods (spec_id, tag, method, endpoint_count) VALUES (?, ?, ?, ?)",
				id, tg.Name, method, count,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

const metaColumns = "name, source, etag, sha256, fetched_at, title, version, endpoint_count, schema_count"

// List returns every indexed spec's metadata, ordered by name, without loading
// any document blob.
func (s *Store) List() ([]SpecMeta, error) {
	rows, err := s.db.Query("SELECT " + metaColumns + " FROM specs ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list indexed specs: %w", err)
	}
	defer rows.Close()

	specs := []SpecMeta{}
	for rows.Next() {
		meta, err := scanMeta(rows)
		if err != nil {
			return nil, err
		}
		specs = append(specs, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list indexed specs: %w", err)
	}
	return specs, nil
}

// Meta returns one spec's metadata, or ErrNotFound.
func (s *Store) Meta(name string) (SpecMeta, error) {
	row := s.db.QueryRow("SELECT "+metaColumns+" FROM specs WHERE name = ?", name)
	meta, err := scanMeta(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SpecMeta{}, fmt.Errorf("spec %q: %w", name, ErrNotFound)
	}
	return meta, err
}

// Get returns the stored spec with its document decompressed. Detail queries
// reuse this document with the existing extractor instead of reimplementing
// $ref resolution over SQL.
func (s *Store) Get(name string) (*Record, error) {
	var (
		rec       Record
		fetchedAt string
		blob      []byte
	)
	err := s.db.QueryRow(
		"SELECT name, source, etag, sha256, fetched_at, title, version, document FROM specs WHERE name = ?", name,
	).Scan(
		&rec.Name, &rec.Source, &rec.ETag, &rec.SHA256, &fetchedAt,
		&rec.Title, &rec.Version, &blob,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("spec %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read spec %q: %w", name, err)
	}
	if rec.FetchedAt, err = time.Parse(time.RFC3339Nano, fetchedAt); err != nil {
		return nil, fmt.Errorf("read spec %q: invalid fetched_at %q: %w", name, fetchedAt, err)
	}
	if rec.Document, err = decompress(blob); err != nil {
		return nil, fmt.Errorf("read spec %q: %w", name, err)
	}
	return &rec, nil
}

// rowScanner covers both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanMeta(row rowScanner) (SpecMeta, error) {
	var (
		meta      SpecMeta
		fetchedAt string
	)
	err := row.Scan(&meta.Name, &meta.Source, &meta.ETag, &meta.SHA256, &fetchedAt,
		&meta.Title, &meta.Version, &meta.EndpointCount, &meta.SchemaCount)
	if err != nil {
		return SpecMeta{}, err
	}
	if meta.FetchedAt, err = time.Parse(time.RFC3339Nano, fetchedAt); err != nil {
		return SpecMeta{}, fmt.Errorf("spec %q has an invalid fetched_at %q: %w", meta.Name, fetchedAt, err)
	}
	return meta, nil
}

// OpenReadOnly opens an existing store for queries. Unlike Open it never creates
// the directory, the database file or the schema, and it never migrates: a query
// is not allowed to write (S1, S3). A store whose layout this build cannot read
// is reported with a message naming the command that fixes it.
func OpenReadOnly(dir string) (*Store, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve store directory %s: %w", dir, err)
	}
	path := filepath.Join(absDir, DBFileName)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}

	db, err := sql.Open("sqlite", readOnlyDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", path, err)
	}
	// One reader per store keeps the per-connection pragmas in effect, exactly
	// like the writer side.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("prepare read-only store %s (%s): %w", path, pragma, err)
		}
	}

	s := &Store{dir: absDir, path: path, db: db}
	version, err := s.schemaVersion()
	if err != nil {
		db.Close()
		return nil, err
	}
	if version != SchemaVersion {
		db.Close()
		if version > SchemaVersion {
			return nil, newerSchemaError(path, version)
		}
		if version == 1 {
			// An old store is answerable, but only after the migration.
			return nil, fmt.Errorf("store %s uses schema version 1, and this build needs version %d: run `swagger-mcp index <url|file>` once to upgrade it, then query again (a query never writes)",
				path, SchemaVersion)
		}
		return nil, fmt.Errorf("store %s has no usable schema (version %d, want %d): run `swagger-mcp index` to create it",
			path, version, SchemaVersion)
	}
	return s, nil
}

// readOnlyDSN is the connection string that makes SQLite refuse every write. It
// is a URI so the mode parameter reaches SQLite (the driver opens with
// SQLITE_OPEN_URI).
func readOnlyDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?mode=ro"
}

// schemaVersion reads PRAGMA user_version of this store.
func (s *Store) schemaVersion() (int, error) {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version of %s: %w", s.path, err)
	}
	return version, nil
}

// setUserVersion records the layout version inside the migration transaction, so
// a failed upgrade leaves the store at the version it started from.
func setUserVersion(tx *sql.Tx, version int) error {
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return fmt.Errorf("record schema version %d: %w", version, err)
	}
	return nil
}

// newerSchemaError is the message an older binary reports for a store written by
// a newer one, whether it opened it for writing or for querying.
func newerSchemaError(path string, version int) error {
	return fmt.Errorf("store %s uses schema version %d, but this build supports %d: upgrade swagger-mcp",
		path, version, SchemaVersion)
}
