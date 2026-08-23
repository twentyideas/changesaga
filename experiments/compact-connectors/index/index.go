// Package index builds a disposable SQLite index over a saga's connector
// shards.
//
// The index is a cache and nothing else. It is never committed, it is never
// required for correctness, and it can be deleted at any moment: every answer
// it gives is derivable from the shards alone, and Open rebuilds whatever part
// of it the shards no longer agree with. It lives outside the saga directory
// precisely so no one can commit it by accident.
//
// What it buys is the cold-start cost. Answering "which target owns this line"
// from shards alone means parsing every shard; answering it from the index
// means one indexed lookup. Because a shard is the unit of invalidation, a
// reviewer who records one new mapping re-parses one file rather than all of
// them.
package index

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/twentyideas/changesaga/experiments/compact-connectors/connector"
	"github.com/twentyideas/changesaga/experiments/compact-connectors/reader"
)

// SchemaVersion is bumped whenever the derived tables change shape. A stored
// index with a different value is discarded rather than migrated: it is a
// cache, so rebuilding is always cheaper than being careful.
const SchemaVersion = 1

const schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;

CREATE TABLE shard (
  id            INTEGER PRIMARY KEY,
  path          TEXT NOT NULL UNIQUE,
  owner         TEXT NOT NULL,
  source        TEXT NOT NULL,
  size          INTEGER NOT NULL,
  modified_ns   INTEGER NOT NULL,
  digest        TEXT NOT NULL
) STRICT;

CREATE TABLE comparison (
  id         INTEGER PRIMARY KEY,
  repository TEXT NOT NULL,
  base       TEXT NOT NULL,
  head       TEXT NOT NULL,
  UNIQUE (repository, base, head)
) STRICT;

CREATE TABLE note (
  id   INTEGER PRIMARY KEY,
  text TEXT NOT NULL UNIQUE
) STRICT;

CREATE TABLE record (
  id            INTEGER PRIMARY KEY,
  shard_id      INTEGER NOT NULL REFERENCES shard(id) ON DELETE CASCADE,
  ordinal       INTEGER NOT NULL,
  comparison_id INTEGER NOT NULL REFERENCES comparison(id),
  note_id       INTEGER REFERENCES note(id),
  kind          TEXT NOT NULL,
  side          TEXT,
  start_line    INTEGER,
  end_line      INTEGER,
  event         TEXT,
  path          TEXT,
  old_path      TEXT,
  new_path      TEXT
) STRICT;

CREATE INDEX record_shard ON record (shard_id);
CREATE INDEX record_lookup ON record (comparison_id, path, side, start_line, end_line);

CREATE VIEW owned_lines AS
  SELECT shard.owner AS owner, shard.path AS shard_path, record.path AS source_path,
         record.side AS side, record.start_line AS start_line, record.end_line AS end_line,
         note.text AS note
  FROM record
  JOIN shard ON shard.id = record.shard_id
  LEFT JOIN note ON note.id = record.note_id
  WHERE record.kind = 'lines';
`

// Index is an open handle to a saga's derived index.
type Index struct {
	db   *sql.DB
	root string
	path string
	// Rebuilt and Reused count shards touched by the last Open, which is what
	// the incremental-invalidation test asserts on.
	Rebuilt int
	Reused  int
	Removed int
}

// Path returns where the index for a saga root is cached. It is deliberately
// outside the saga so `git status` can never show it and no one can commit it.
func Path(sagaRoot string) (string, error) {
	absolute, err := filepath.Abs(sagaRoot)
	if err != nil {
		return "", err
	}
	base := os.Getenv("CHANGE_SAGA_INDEX_DIR")
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(cache, "change-saga", "connector-index")
	}
	sum := sha256.Sum256([]byte(absolute))
	return filepath.Join(base, hex.EncodeToString(sum[:])+".sqlite"), nil
}

// Open returns an index for a saga root, building or refreshing it so that it
// matches the shards on disk. A shard whose size and modification time are
// unchanged is not re-read.
func Open(ctx context.Context, sagaRoot string) (*Index, error) {
	path, err := Path(sagaRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	index := &Index{root: sagaRoot, path: path}
	if err := index.open(ctx); err != nil {
		return nil, err
	}
	if err := index.Refresh(ctx); err != nil {
		index.Close()
		return nil, err
	}
	return index, nil
}

func (i *Index) open(ctx context.Context) error {
	db, err := openDatabase(ctx, i.path)
	if err != nil {
		return err
	}
	i.db = db

	var version string
	err = db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&version)
	switch {
	case err == nil && version == fmt.Sprint(SchemaVersion):
		return nil
	case err != nil && !errors.Is(err, sql.ErrNoRows) && !isMissingTable(err):
		db.Close()
		return err
	}
	// Either there is no index yet or it was built by a different schema. Both
	// are handled the same way, because the file is disposable.
	db.Close()
	if err := removeDatabase(i.path); err != nil {
		return err
	}
	db, err = openDatabase(ctx, i.path)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('schema_version', ?)`, fmt.Sprint(SchemaVersion)); err != nil {
		db.Close()
		return err
	}
	i.db = db
	return nil
}

func openDatabase(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection keeps the cache single-writer and makes the timings
	// reflect the index rather than SQLite's connection pool.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

// isMissingTable also covers a corrupt or truncated cache file. Both mean the
// same thing here: throw it away and build a new one.
func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "no such table") || strings.Contains(message, "file is not a database")
}

func removeDatabase(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Close releases the index handle. It never deletes the file: the point of a
// cache is that the next process finds it warm.
func (i *Index) Close() error {
	if i.db == nil {
		return nil
	}
	return i.db.Close()
}

// Discard closes and deletes the index, which is what "disposable" means in
// practice and what the cold-start benchmark uses.
func (i *Index) Discard() error {
	if err := i.Close(); err != nil {
		return err
	}
	return removeDatabase(i.path)
}

// Refresh reconciles the index with the shards on disk.
func (i *Index) Refresh(ctx context.Context) error {
	onDisk, err := reader.Stat(i.root)
	if err != nil {
		return err
	}
	known, err := i.knownShards(ctx)
	if err != nil {
		return err
	}

	i.Rebuilt, i.Reused, i.Removed = 0, 0, 0
	transaction, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()

	present := make(map[string]bool, len(onDisk))
	for _, shard := range onDisk {
		present[shard.Path] = true
		if previous, ok := known[shard.Path]; ok && previous.Size == shard.Size && previous.ModTimeNanos == shard.ModTimeNanos {
			i.Reused++
			continue
		}
		decoded, err := reader.ReadShardAt(i.root, shard.Path)
		if err != nil {
			return err
		}
		if err := i.replaceShard(ctx, transaction, decoded); err != nil {
			return err
		}
		i.Rebuilt++
	}
	for path := range known {
		if present[path] {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `DELETE FROM shard WHERE path = ?`, path); err != nil {
			return err
		}
		i.Removed++
	}
	return transaction.Commit()
}

type shardState struct {
	Size         int64
	ModTimeNanos int64
}

func (i *Index) knownShards(ctx context.Context) (map[string]shardState, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT path, size, modified_ns FROM shard`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]shardState{}
	for rows.Next() {
		var path string
		var state shardState
		if err := rows.Scan(&path, &state.Size, &state.ModTimeNanos); err != nil {
			return nil, err
		}
		known[path] = state
	}
	return known, rows.Err()
}

func (i *Index) replaceShard(ctx context.Context, transaction *sql.Tx, shard reader.Shard) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM shard WHERE path = ?`, shard.Path); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", shard.Path, shard.Size, shard.ModTimeNanos)))
	result, err := transaction.ExecContext(ctx,
		`INSERT INTO shard (path, owner, source, size, modified_ns, digest) VALUES (?, ?, ?, ?, ?, ?)`,
		shard.Path, shard.File.Owner, shard.File.Source, shard.Size, shard.ModTimeNanos, hex.EncodeToString(digest[:]))
	if err != nil {
		return err
	}
	shardID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	records := append([]connector.Record(nil), shard.File.Records...)
	connector.SortRecords(records)
	for ordinal, record := range records {
		comparisonID, err := upsertComparison(ctx, transaction, record.Comparison)
		if err != nil {
			return err
		}
		var noteID any
		if record.Note != "" {
			id, err := upsertNote(ctx, transaction, record.Note)
			if err != nil {
				return err
			}
			noteID = id
		}
		path := record.Path
		if record.Kind == "lines" {
			path = shard.File.Source
		}
		if _, err := transaction.ExecContext(ctx,
			`INSERT INTO record (shard_id, ordinal, comparison_id, note_id, kind, side, start_line, end_line, event, path, old_path, new_path)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			shardID, ordinal+1, comparisonID, noteID, record.Kind,
			nullable(record.Side), nullableInt(record.Start), nullableInt(record.End),
			nullable(record.Event), nullable(path), nullable(record.OldPath), nullable(record.NewPath)); err != nil {
			return err
		}
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func upsertComparison(ctx context.Context, transaction *sql.Tx, comparison connector.Comparison) (int64, error) {
	var id int64
	err := transaction.QueryRowContext(ctx,
		`SELECT id FROM comparison WHERE repository = ? AND base = ? AND head = ?`,
		comparison.Repository, comparison.Base, comparison.Head).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	result, err := transaction.ExecContext(ctx,
		`INSERT INTO comparison (repository, base, head) VALUES (?, ?, ?)`,
		comparison.Repository, comparison.Base, comparison.Head)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func upsertNote(ctx context.Context, transaction *sql.Tx, text string) (int64, error) {
	var id int64
	err := transaction.QueryRowContext(ctx, `SELECT id FROM note WHERE text = ?`, text).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO note (text) VALUES (?)`, text)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Overview is the cheap whole-saga summary the index exists to make cheap.
type Overview struct {
	Shards      int
	Records     int
	LineAtoms   int
	EventAtoms  int
	Owners      int
	SourceFiles int
	Notes       int
	Comparisons int
}

// Overview answers the saga-wide counts without touching the shards.
func (i *Index) Overview(ctx context.Context) (Overview, error) {
	var overview Overview
	row := i.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM shard),
		       (SELECT COUNT(*) FROM record),
		       (SELECT COALESCE(SUM(end_line - start_line + 1), 0) FROM record WHERE kind = 'lines'),
		       (SELECT COUNT(*) FROM record WHERE kind = 'event'),
		       (SELECT COUNT(DISTINCT owner) FROM shard),
		       (SELECT COUNT(DISTINCT source) FROM shard),
		       (SELECT COUNT(*) FROM note),
		       (SELECT COUNT(*) FROM comparison)`)
	err := row.Scan(&overview.Shards, &overview.Records, &overview.LineAtoms, &overview.EventAtoms,
		&overview.Owners, &overview.SourceFiles, &overview.Notes, &overview.Comparisons)
	return overview, err
}

// Owner is one narrative target that owns a line.
type Owner struct {
	Target    string
	ShardPath string
	Note      string
	Start     int
	End       int
}

// OwnersOfLine answers the reviewer's question — "what explains this line?" —
// with one indexed lookup instead of a scan of every shard.
func (i *Index) OwnersOfLine(ctx context.Context, sourcePath, side string, line int) ([]Owner, error) {
	rows, err := i.db.QueryContext(ctx, `
		SELECT owner, shard_path, COALESCE(note, ''), start_line, end_line
		FROM owned_lines
		WHERE source_path = ? AND side = ? AND start_line <= ? AND end_line >= ?
		ORDER BY owner, start_line`, sourcePath, side, line, line)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var owners []Owner
	for rows.Next() {
		var owner Owner
		if err := rows.Scan(&owner.Target, &owner.ShardPath, &owner.Note, &owner.Start, &owner.End); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}

// TargetCoverage is one target's atom count, the number the reviewer sees
// beside every chapter and fragment.
type TargetCoverage struct {
	Target string
	Atoms  int
	Files  int
}

// CoverageByTarget answers the overview's per-target counts from the index.
func (i *Index) CoverageByTarget(ctx context.Context) ([]TargetCoverage, error) {
	rows, err := i.db.QueryContext(ctx, `
		SELECT shard.owner,
		       COALESCE(SUM(CASE WHEN record.kind = 'lines' THEN record.end_line - record.start_line + 1 ELSE 1 END), 0),
		       COUNT(DISTINCT shard.source)
		FROM shard LEFT JOIN record ON record.shard_id = shard.id
		GROUP BY shard.owner ORDER BY shard.owner`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var coverage []TargetCoverage
	for rows.Next() {
		var value TargetCoverage
		if err := rows.Scan(&value.Target, &value.Atoms, &value.Files); err != nil {
			return nil, err
		}
		coverage = append(coverage, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(coverage, func(a, b int) bool { return coverage[a].Target < coverage[b].Target })
	return coverage, nil
}

// FileSize reports the index's own size on disk, which is the number that says
// whether keeping it is cheaper than rebuilding it.
func (i *Index) FileSize() (int64, error) {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(i.path + suffix)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, err
		}
		total += info.Size()
	}
	return total, nil
}
