//go:build !nosqlite

package storage

import (
	"context"
	"strings"
	"testing"
)

// A pool's history is read through the index, not by scanning the chain.
//
// GetSwaps compares pool COLLATE NOCASE — the address arrives in whatever case
// the caller typed. SQLite consults an index only when its collation matches
// the comparison's, so an index on swaps(pool) alone was never used for that
// read, and a store that had already built it keeps it until told otherwise.
func TestPoolHistoryReadsTheIndex(t *testing.T) {
	dir := t.TempDir()
	old, err := New(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	// The store as it was, with the index that could not be used.
	if _, err := old.db.Exec(`
		CREATE TABLE swaps (id TEXT PRIMARY KEY, data JSON, timestamp INTEGER, pool TEXT, amountUSD REAL NOT NULL DEFAULT 0);
		CREATE INDEX idx_swaps_pool ON swaps(pool);
	`); err != nil {
		t.Fatalf("old schema: %v", err)
	}
	old.Close()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s.Close()
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT id, data FROM swaps WHERE pool = ? COLLATE NOCASE ORDER BY timestamp DESC LIMIT 10`, "0xPOOL")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, " | ")
	if !strings.Contains(joined, "idx_swaps_pool_time") {
		t.Fatalf("pool history is not read through idx_swaps_pool_time: %s", joined)
	}
	if strings.Contains(joined, "SCAN swaps") || strings.Contains(joined, "TEMP B-TREE") {
		t.Fatalf("pool history still scans or sorts: %s", joined)
	}
}
