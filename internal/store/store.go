// Package store is the assistant's offline cache: a SQLite database under the
// OS cache directory holding what has been fetched, so it is fetched once
// (decisions 11 and 16). It does nothing but read and write; deciding when
// something is stale, or what to fetch, is the caller's. Everything in it can
// be fetched again, so deleting it is always safe.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	// The pure-Go SQLite driver, registered as "sqlite": no cgo, so Linux
	// and Windows builds stay cgo-free (decision 11).
	_ "modernc.org/sqlite"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// FileName is the database's name inside the cache directory.
const FileName = "cache.db"

// Store is the open cache. Use it from one goroutine, or several: database/sql
// serialises access, and the store holds one connection.
type Store struct {
	db *sql.DB
}

// migrations are the schema, one step per entry, applied in order. The
// database's user_version is how many have been applied. A step is never
// edited once released: a change to the schema is a new step.
var migrations = []string{
	// 1: sets and their cards, keyed by Arena id (decision 15).
	`CREATE TABLE sets (
		code       TEXT PRIMARY KEY,
		fetched_at INTEGER NOT NULL
	);
	CREATE TABLE cards (
		arena_id  INTEGER PRIMARY KEY,
		set_code  TEXT NOT NULL REFERENCES sets(code) ON DELETE CASCADE,
		name      TEXT NOT NULL,
		mana_cost TEXT NOT NULL,
		type_line TEXT NOT NULL,
		rarity    TEXT NOT NULL
	);
	CREATE INDEX cards_by_set ON cards(set_code);`,
	// 2: each card's picture (decision 12). Sets stored before it have
	// none, so they are marked as fetched long ago: the next draft of one
	// shows it at once and fetches it again for its pictures, rather than
	// going without them until the weekly refresh.
	`ALTER TABLE cards ADD COLUMN image_url TEXT NOT NULL DEFAULT '';
	UPDATE sets SET fetched_at = 0;`,
	// 3: the mana each card makes (decision 32), so the suggested build
	// can play the pool's lands. Sets stored before it are marked as
	// fetched long ago, as in step 2: the next draft of one shows it at
	// once and fetches it again, once, for what its lands make.
	`ALTER TABLE cards ADD COLUMN produced_mana TEXT NOT NULL DEFAULT '';
	UPDATE sets SET fetched_at = 0;`,
}

// ErrNewerSchema is a cache written by a newer version of the assistant,
// whose schema this one does not know. It is left as it is, not migrated
// backwards: the newer version will want it back.
var ErrNewerSchema = errors.New("it was made by a newer version of the assistant")

// Open opens the cache in dir, creating dir and the database if they do not
// exist, and brings its schema up to date.
func Open(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating the cache folder: %w", err)
	}
	path := filepath.Join(dir, FileName)
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	// The assistant has one goroutine writing at a time; one connection
	// keeps it from ever contending with itself.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.setUp(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// dsn is path as a file: URI, with the pragmas every connection runs. A
// plain path would be cut at its first "?" by the driver, which reads what
// follows as its parameters; in a URI the path is escaped, and SQLite
// decodes it. Another copy of the assistant may hold the database for a
// moment, and waiting for it (busy_timeout) beats failing.
func dsn(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // a Windows drive: file:///C:/Users/...
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"}
	return u.String()
}

func (s *Store) setUp(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("reading the cache: %w", err)
	}
	if version > len(migrations) {
		return ErrNewerSchema
	}
	for i := version; i < len(migrations); i++ {
		if err := s.migrate(ctx, i); err != nil {
			return fmt.Errorf("updating the cache: %w", err)
		}
	}
	return nil
}

// migrate applies migrations[i] and records it, in one transaction, so a
// step is either wholly applied or not at all.
func (s *Store) migrate(ctx context.Context, i int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
		return err
	}
	// PRAGMA takes no parameters; i is ours, not input.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
		return err
	}
	return tx.Commit()
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Set is a set's cards as stored, and when they were fetched.
type Set struct {
	Code      string
	FetchedAt time.Time
	Cards     []cards.Card
}

// Set returns the stored set code (as Arena writes it, any case), and false
// if it has never been stored. A set stored with no cards (Scryfall had none)
// is found, with none.
func (s *Store) Set(ctx context.Context, code string) (Set, bool, error) {
	code = strings.ToUpper(code)
	var fetched int64
	err := s.db.QueryRowContext(ctx, "SELECT fetched_at FROM sets WHERE code = ?", code).Scan(&fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return Set{}, false, nil
	}
	if err != nil {
		return Set{}, false, fmt.Errorf("reading %s from the cache: %w", code, err)
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT arena_id, name, mana_cost, type_line, rarity, image_url, produced_mana FROM cards WHERE set_code = ? ORDER BY arena_id", code)
	if err != nil {
		return Set{}, false, fmt.Errorf("reading %s's cards from the cache: %w", code, err)
	}
	defer func() { _ = rows.Close() }()
	set := Set{Code: code, FetchedAt: time.Unix(fetched, 0)}
	for rows.Next() {
		c := cards.Card{Set: code}
		if err := rows.Scan(&c.ArenaID, &c.Name, &c.ManaCost, &c.TypeLine, &c.Rarity, &c.ImageURL, &c.Produces); err != nil {
			return Set{}, false, fmt.Errorf("reading %s's cards from the cache: %w", code, err)
		}
		set.Cards = append(set.Cards, c)
	}
	if err := rows.Err(); err != nil {
		return Set{}, false, fmt.Errorf("reading %s's cards from the cache: %w", code, err)
	}
	return set, true, nil
}

// SaveSet stores set's cards in place of any stored before, as fetched at
// fetchedAt. It is one transaction: a set is never half stored.
func (s *Store) SaveSet(ctx context.Context, code string, fetchedAt time.Time, cs []cards.Card) (err error) {
	code = strings.ToUpper(code)
	defer func() {
		if err != nil {
			err = fmt.Errorf("saving %s to the cache: %w", code, err)
		}
	}()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO sets (code, fetched_at) VALUES (?, ?) ON CONFLICT (code) DO UPDATE SET fetched_at = excluded.fetched_at",
		code, fetchedAt.Unix()); err != nil {
		return err
	}
	// A card Scryfall no longer lists in the set is not kept in it.
	if _, err := tx.ExecContext(ctx, "DELETE FROM cards WHERE set_code = ?", code); err != nil {
		return err
	}
	ins, err := tx.PrepareContext(ctx,
		"INSERT OR REPLACE INTO cards (arena_id, set_code, name, mana_cost, type_line, rarity, image_url, produced_mana) VALUES (?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer func() { _ = ins.Close() }()
	for _, c := range cs {
		if _, err := ins.ExecContext(ctx, c.ArenaID, code, c.Name, c.ManaCost, c.TypeLine, c.Rarity, c.ImageURL, c.Produces); err != nil {
			return err
		}
	}
	return tx.Commit()
}
