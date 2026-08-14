//go:build !nosqlite

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// A database written before the column has to arrive holding its history.
//
// The dollar value of a trade has always been stored inside the swap's JSON
// document, where SQL cannot reach it — this driver's amalgamation carries no
// JSON1, so json_extract is not available to read it either. Promoting it to a
// column is what lets the protocol's all-time volume be a SUM. A chain that has
// been indexing for a year meets that column for the first time on the start
// that introduces it, and if it arrived empty every trade before that moment
// would read as worthless: the fix would zero the very number it exists to
// correct.
func TestTradedSurvivesTheColumnArriving(t *testing.T) {
	dir := t.TempDir()

	// A store as it was: swaps with no amountUSD column, values in the document.
	old, err := New(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := old.db.Exec(`
		CREATE TABLE swaps (id TEXT PRIMARY KEY, data JSON, timestamp INTEGER, pool TEXT);
		CREATE INDEX idx_swaps_timestamp ON swaps(timestamp);
	`); err != nil {
		t.Fatalf("old schema: %v", err)
	}
	for id, usd := range map[string]string{"s1": "1000.00", "s2": "2000.00", "s3": "500.00"} {
		row, _ := json.Marshal(&SeedSwapData{Timestamp: 100, Pool: "0xpool", AmountUSD: usd})
		if _, err := old.db.Exec(`INSERT INTO swaps(id, data, timestamp, pool) VALUES(?,?,?,?)`,
			id, string(row), 100, "0xpool"); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	old.Close()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s.Close()
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	traded, volume, err := s.Traded()
	if err != nil {
		t.Fatalf("Traded: %v", err)
	}
	if traded != 3 {
		t.Errorf("trades = %d, want 3", traded)
	}
	if volume != 3500 {
		t.Errorf("volume = %v, want 3500 — the history was already in the rows", volume)
	}

	// And the fill runs once: a second Init must not undo a value the pass has
	// since written, which is what re-reading the documents on every start would
	// do to any row whose column has moved ahead of them.
	if _, err := s.ValueSwaps(map[string]string{"s1": "1500.00"}); err != nil {
		t.Fatalf("ValueSwaps: %v", err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	if _, volume, _ = s.Traded(); volume != 4000 {
		t.Errorf("volume = %v, want 4000 after repricing s1", volume)
	}
}

// Pricing a trade moves the column with the document. They are one value in two
// places — the document is the wire shape, the column is what SQL can add up —
// and a write that moves only one of them makes the total disagree with the
// trades it is a total of.
func TestValueSwapsMovesTheColumn(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	s.SeedSwap("s1", &SeedSwapData{Timestamp: 100, Pool: "0xpool", AmountUSD: "0"})

	if _, _, err := traded(t, s); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValueSwaps(map[string]string{"s1": "42.50"}); err != nil {
		t.Fatalf("ValueSwaps: %v", err)
	}
	count, volume, err := traded(t, s)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || volume != 42.5 {
		t.Errorf("traded = %d/%v, want 1/42.5", count, volume)
	}
}

func traded(t *testing.T, s *Store) (int64, float64, error) {
	t.Helper()
	return s.Traded()
}

// A history longer than any window has to be reachable a batch at a time.
//
// A valuation pass reads the newest N trades, so on a chain with a million of
// them the older ones were never valued and their stored worth stayed zero —
// which is what an all-time total was then a sum of. Walking forward from a mark
// covers the whole table in as many passes as it takes, and stops asking once it
// arrives.
func TestSwapsAfterWalksTheWholeTable(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	const total, batch = 25, 10
	for i := 0; i < total; i++ {
		s.SeedSwap(fmt.Sprintf("0x%02x#0x0", i), &SeedSwapData{
			Timestamp: int64(1_700_000_000 + i), Block: uint64(i + 1), Pool: "0xpool", AmountUSD: "0",
		})
	}

	seen, passes := map[string]bool{}, 0
	for mark := s.PricedThrough(); ; passes++ {
		got := s.SwapsAfter(mark, batch)
		if len(got) == 0 {
			break
		}
		for id, sw := range got {
			seen[id] = true
			if sw.Timestamp > mark {
				mark = sw.Timestamp
			}
		}
		s.SetPricedThrough(mark)
		if passes > total {
			t.Fatal("the walk did not converge — a mark that does not advance repeats a batch forever")
		}
	}
	if len(seen) != total {
		t.Errorf("reached %d of %d trades", len(seen), total)
	}
	if passes != 3 {
		t.Errorf("took %d passes over %d trades in batches of %d, want 3", passes, total, batch)
	}
	// And the mark is durable: a restart resumes rather than starting over.
	if got := s.PricedThrough(); got != 1_700_000_000+total-1 {
		t.Errorf("mark = %d, want the newest trade's time", got)
	}
}
