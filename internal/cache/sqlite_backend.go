package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no CGO
)

// SQLiteBackend stores every (bucket, key) row in a single SQLite file.
// The schema is one wide cache table with a composite PK plus a
// per-bucket index for Clear / per-bucket scans:
//
//	CREATE TABLE cache (
//	    bucket     TEXT NOT NULL,
//	    key        TEXT NOT NULL,
//	    fetched_at INTEGER NOT NULL, -- unix nanos
//	    payload    BLOB    NOT NULL,
//	    PRIMARY KEY (bucket, key)
//	) WITHOUT ROWID;
//
// WAL journaling lets the TUI write concurrently without locking out a
// background poller.
type SQLiteBackend struct {
	db   *sql.DB
	once sync.Once
	err  error
}

// NewSQLiteBackend opens (or creates) the SQLite file at path and
// initialises the schema. Safe to call from multiple cloudnav
// processes — WAL mode handles the concurrency.
func NewSQLiteBackend(path string) (*SQLiteBackend, error) {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("cache dir: %w", err)
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite cache: %w", err)
	}
	// modernc/sqlite serialises queries on a single connection by
	// default — leaving the pool small avoids "database is locked"
	// during bursty writes.
	db.SetMaxOpenConns(1)
	b := &SQLiteBackend{db: db}
	if err := b.init(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init sqlite cache: %w", err)
	}
	return b, nil
}

func (b *SQLiteBackend) init() error {
	_, err := b.db.Exec(`
CREATE TABLE IF NOT EXISTS cache (
    bucket     TEXT NOT NULL,
    key        TEXT NOT NULL,
    fetched_at INTEGER NOT NULL,
    payload    BLOB    NOT NULL,
    PRIMARY KEY (bucket, key)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_cache_bucket ON cache(bucket);
`)
	return err
}

func (b *SQLiteBackend) Read(bucket, key string) ([]byte, time.Time, bool) {
	var (
		fetchedAt int64
		payload   []byte
	)
	row := b.db.QueryRow(
		`SELECT fetched_at, payload FROM cache WHERE bucket = ? AND key = ?`,
		bucket, key,
	)
	if err := row.Scan(&fetchedAt, &payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, time.Time{}, false
		}
		return nil, time.Time{}, false
	}
	return payload, time.Unix(0, fetchedAt), true
}

func (b *SQLiteBackend) Write(bucket, key string, payload []byte, fetchedAt time.Time) error {
	_, err := b.db.Exec(
		`INSERT INTO cache (bucket, key, fetched_at, payload)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(bucket, key) DO UPDATE SET
		     fetched_at = excluded.fetched_at,
		     payload    = excluded.payload`,
		bucket, key, fetchedAt.UnixNano(), payload,
	)
	return err
}

func (b *SQLiteBackend) Remove(bucket, key string) error {
	_, err := b.db.Exec(`DELETE FROM cache WHERE bucket = ? AND key = ?`, bucket, key)
	return err
}

func (b *SQLiteBackend) RemoveBucket(bucket string) error {
	_, err := b.db.Exec(`DELETE FROM cache WHERE bucket = ?`, bucket)
	return err
}

func (b *SQLiteBackend) Close() error {
	b.once.Do(func() {
		if b.db != nil {
			b.err = b.db.Close()
		}
	})
	return b.err
}

func ensureDir(dir string) error {
	if dir == "" {
		return nil
	}
	return mkdirAll(dir, 0o755)
}
