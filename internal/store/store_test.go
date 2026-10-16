package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

var (
	fetched = time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
	elf     = cards.Card{ArenaID: 106325, Set: "FRA", Name: "Verdant Oathkeeper", ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid", Rarity: "uncommon"}
	forest  = cards.Card{ArenaID: 106485, Set: "FRA", Name: "Forest", TypeLine: "Basic Land — Forest", Rarity: "common"}
)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestASavedSetIsReadBackWithItsCardsAndWhenItWasFetched(t *testing.T) {
	s := open(t, t.TempDir())
	if err := s.SaveSet(t.Context(), "FRA", fetched, []cards.Card{forest, elf}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Set(t.Context(), "FRA")
	if err != nil || !ok {
		t.Fatalf("Set = %v, %v", ok, err)
	}
	if !got.FetchedAt.Equal(fetched) || len(got.Cards) != 2 || got.Cards[0] != elf || got.Cards[1] != forest {
		t.Errorf("read back %+v", got)
	}
}

func TestTheCacheOutlivesTheProcess(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSet(t.Context(), "FRA", fetched, []cards.Card{elf}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got, ok, err := open(t, dir).Set(t.Context(), "FRA")
	if err != nil || !ok || len(got.Cards) != 1 || got.Cards[0] != elf {
		t.Errorf("after reopening, Set = %+v, %v, %v", got, ok, err)
	}
}

func TestASetNeverStoredIsNotFound(t *testing.T) {
	_, ok, err := open(t, t.TempDir()).Set(t.Context(), "DSK")
	if err != nil || ok {
		t.Errorf("Set of a set never stored = %v, %v; want not found", ok, err)
	}
}

func TestASetWithNoCardsIsStoredAsKnownToHaveNone(t *testing.T) {
	// So a set Scryfall has nothing for is not asked about on every draft.
	s := open(t, t.TempDir())
	if err := s.SaveSet(t.Context(), "ZZZ", fetched, nil); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Set(t.Context(), "ZZZ")
	if err != nil || !ok || len(got.Cards) != 0 || !got.FetchedAt.Equal(fetched) {
		t.Errorf("Set = %+v, %v, %v; want found, with no cards", got, ok, err)
	}
}

func TestSavingASetAgainReplacesItsCards(t *testing.T) {
	s := open(t, t.TempDir())
	if err := s.SaveSet(t.Context(), "FRA", fetched, []cards.Card{elf, forest}); err != nil {
		t.Fatal(err)
	}
	renamed := elf
	renamed.Name = "Verdant Oathkeeper (errata)"
	later := fetched.Add(7 * 24 * time.Hour)
	if err := s.SaveSet(t.Context(), "FRA", later, []cards.Card{renamed}); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Set(t.Context(), "FRA")
	if err != nil || len(got.Cards) != 1 || got.Cards[0] != renamed || !got.FetchedAt.Equal(later) {
		t.Errorf("after saving again, Set = %+v, %v", got, err)
	}
}

func TestSetsAreKeptApart(t *testing.T) {
	s := open(t, t.TempDir())
	other := cards.Card{ArenaID: 90001, Set: "DSK", Name: "Other", TypeLine: "Instant", Rarity: "rare"}
	if err := s.SaveSet(t.Context(), "FRA", fetched, []cards.Card{elf}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSet(t.Context(), "DSK", fetched, []cards.Card{other}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSet(t.Context(), "DSK", fetched, nil); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Set(t.Context(), "FRA"); len(got.Cards) != 1 {
		t.Errorf("emptying DSK left FRA with %+v", got.Cards)
	}
}

func TestSetCodesMatchWhateverTheirCase(t *testing.T) {
	s := open(t, t.TempDir())
	if err := s.SaveSet(t.Context(), "fra", fetched, []cards.Card{elf}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Set(t.Context(), "FRA")
	if err != nil || !ok || got.Code != "FRA" || got.Cards[0].Set != "FRA" {
		t.Errorf("Set(FRA) after saving fra = %+v, %v, %v", got, ok, err)
	}
}

func TestOpeningCreatesTheCacheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache", "mtga-draft-assistant")
	open(t, dir)
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Errorf("no database in a new cache folder: %v", err)
	}
}

func TestAPathWithSpacesAndQuestionMarksIsOpenedAsAPath(t *testing.T) {
	// Not a URI: the database is where the path says, not cut off at "?".
	dir := filepath.Join(t.TempDir(), "Application Support?x=1#y")
	open(t, dir)
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Errorf("no database at the literal path: %v", err)
	}
}

func TestACacheFolderThatCannotBeCreatedIsAnError(t *testing.T) {
	// A file where the folder should be cannot be written through, by root
	// or anyone, unlike a folder without write permission.
	parent := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(parent, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), filepath.Join(parent, "mtga-draft-assistant")); err == nil {
		t.Error("opened a cache under a file")
	}
}

func TestACorruptDatabaseIsAnErrorNotAPanic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("this is not a database, though it is long enough to look like a header to a careless reader"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), dir); err == nil {
		t.Error("opened a file that is not a database")
	}
}

func TestReopeningDoesNotRunTheMigrationsAgain(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	// A second CREATE TABLE would fail, so reopening at all is the test.
	s, err = Open(t.Context(), dir)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	var v int
	if err := s.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&v); err != nil || v != len(migrations) {
		t.Errorf("user_version = %d (%v), want %d", v, err, len(migrations))
	}
	_ = s.Close()
}

func TestACacheFromANewerVersionIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := Open(t.Context(), dir); !errors.Is(err, ErrNewerSchema) {
		t.Errorf("Open = %v, want ErrNewerSchema", err)
	}
}

func TestAFailedSaveLeavesTheSetAsItWas(t *testing.T) {
	s := open(t, t.TempDir())
	if err := s.SaveSet(t.Context(), "FRA", fetched, []cards.Card{elf}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.SaveSet(ctx, "FRA", fetched.Add(time.Hour), []cards.Card{forest}); err == nil {
		t.Fatal("a cancelled save succeeded")
	}
	got, _, err := s.Set(t.Context(), "FRA")
	if err != nil || len(got.Cards) != 1 || got.Cards[0] != elf || !got.FetchedAt.Equal(fetched) {
		t.Errorf("after a failed save, Set = %+v, %v", got, err)
	}
}

func TestTheConnectionWaitsForAnotherCopyOfTheAssistantRatherThanFailing(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "100% cache"))
	var timeout, fk int
	if err := s.db.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if timeout != 5000 || fk != 1 {
		t.Errorf("busy_timeout = %d, foreign_keys = %d; want 5000 and 1", timeout, fk)
	}
}
