package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver: the build is CGO_ENABLED=0
)

// SchemaVersion is the on-disk layout version, stored in PRAGMA user_version.
// Bump it together with a migration whenever a table changes.
const SchemaVersion = 1

// ErrNotFound is returned when a name is not indexed in this store.
var ErrNotFound = errors.New("spec is not indexed")

// schemaV1 is the initial layout: one row per indexed source plus the
// normalized discovery catalog. Every statement is idempotent, so creation is
// safe on an existing store.
var schemaV1 = []string{
	`CREATE TABLE IF NOT EXISTS specs (
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
		document       BLOB    NOT NULL
	) STRICT`,
	`CREATE TABLE IF NOT EXISTS endpoints (
		spec_id    INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		method     TEXT    NOT NULL,
		path       TEXT    NOT NULL,
		summary    TEXT    NOT NULL DEFAULT '',
		tags       TEXT    NOT NULL DEFAULT '',
		deprecated INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, method, path)
	) STRICT`,
	`CREATE INDEX IF NOT EXISTS idx_endpoints_path ON endpoints(path)`,
	`CREATE INDEX IF NOT EXISTS idx_endpoints_method ON endpoints(method)`,
	`CREATE TABLE IF NOT EXISTS schemas (
		spec_id     INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		name        TEXT    NOT NULL,
		field_count INTEGER NOT NULL DEFAULT 0,
		usage_count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, name)
	) STRICT`,
	`CREATE TABLE IF NOT EXISTS tags (
		spec_id        INTEGER NOT NULL REFERENCES specs(id) ON DELETE CASCADE,
		name           TEXT    NOT NULL,
		endpoint_count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (spec_id, name)
	) STRICT`,
}

// Store is an open local index.
type Store struct {
	dir  string
	path string
	db   *sql.DB
}

// Open opens (creating when missing) the store in dir. The directory and the
// database file are created by the indexer, never by hand.
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

// migrate creates the schema or validates the version of an existing one.
func (s *Store) migrate() error {
	version, err := s.schemaVersion()
	if err != nil {
		return err
	}
	if version == SchemaVersion {
		return nil
	}
	if version > SchemaVersion {
		return newerSchemaError(s.path, version)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin schema creation: %w", err)
	}
	defer tx.Rollback()
	for _, stmt := range schemaV1 {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("create schema in %s: %w", s.path, err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion)); err != nil {
		return fmt.Errorf("record schema version in %s: %w", s.path, err)
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
			endpoint_count, schema_count, document)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			source         = excluded.source,
			etag           = excluded.etag,
			sha256         = excluded.sha256,
			fetched_at     = excluded.fetched_at,
			title          = excluded.title,
			version        = excluded.version,
			endpoint_count = excluded.endpoint_count,
			schema_count   = excluded.schema_count,
			document       = excluded.document`,
		rec.Name, rec.Source, rec.ETag, rec.SHA256, rec.FetchedAt.UTC().Format(time.RFC3339Nano),
		rec.Title, rec.Version, len(rec.Endpoints), len(rec.Schemas), blob,
	); err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}

	var id int64
	if err := tx.QueryRow("SELECT id FROM specs WHERE name = ?", rec.Name).Scan(&id); err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
	}

	// The catalog is derived from the document, so replace it wholesale.
	for _, table := range []string{"endpoints", "schemas", "tags"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE spec_id = ?", id); err != nil {
			return fmt.Errorf("store spec %q: %w", rec.Name, err)
		}
	}
	for _, ep := range rec.Endpoints {
		if _, err := tx.Exec(
			"INSERT INTO endpoints (spec_id, method, path, summary, tags, deprecated) VALUES (?, ?, ?, ?, ?, ?)",
			id, ep.Method, ep.Path, ep.Summary, tagList(ep.Tags), boolInt(ep.Deprecated),
		); err != nil {
			return fmt.Errorf("store spec %q: %w", rec.Name, err)
		}
	}
	for _, sc := range rec.Schemas {
		if _, err := tx.Exec(
			"INSERT INTO schemas (spec_id, name, field_count, usage_count) VALUES (?, ?, ?, ?)",
			id, sc.Name, sc.FieldCount, sc.UsageCount,
		); err != nil {
			return fmt.Errorf("store spec %q: %w", rec.Name, err)
		}
	}
	for _, tg := range rec.Tags {
		if _, err := tx.Exec(
			"INSERT INTO tags (spec_id, name, endpoint_count) VALUES (?, ?, ?)",
			id, tg.Name, tg.EndpointCount,
		); err != nil {
			return fmt.Errorf("store spec %q: %w", rec.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store spec %q: %w", rec.Name, err)
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
// is reported with the same version error the indexer uses.
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

// newerSchemaError is the message an older binary reports for a store written by
// a newer one, whether it opened it for writing or for querying.
func newerSchemaError(path string, version int) error {
	return fmt.Errorf("store %s uses schema version %d, but this build supports %d: upgrade swagger-mcp",
		path, version, SchemaVersion)
}
