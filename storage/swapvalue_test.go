package storage

import (
	"context"
	"testing"
)

// The value has to survive the round trip. It rides inside the swap's JSON
// blob, so writing it means editing that document in place — and an edit that
// silently drops the fields beside it would trade one wrong number for several.
func TestValueSwapsPersists(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	s.SeedSwap("s1", &SeedSwapData{Timestamp: 100, Pool: "0xpool", AmountUSD: "0", Amount0: "5", Sender: "0xabc"})
	s.SeedSwap("s2", &SeedSwapData{Timestamp: 101, Pool: "0xpool", AmountUSD: "0", Amount0: "7", Sender: "0xdef"})

	n, err := s.ValueSwaps(map[string]string{"s1": "12.34", "s2": "56.78"})
	if err != nil {
		t.Fatalf("ValueSwaps: %v", err)
	}
	if n != 2 {
		t.Errorf("reported %d rows changed, want 2 — a write that changes nothing must say so", n)
	}

	for id, want := range map[string]string{"s1": "12.34", "s2": "56.78"} {
		got, err := s.GetSwap(nil, id)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		m, _ := got.(map[string]interface{})
		if m == nil {
			t.Fatalf("swap %s came back empty", id)
		}
		if m["amountUSD"] != want {
			t.Errorf("swap %s valued %v, want %s", id, m["amountUSD"], want)
		}
		if m["sender"] == nil || m["sender"] == "" {
			t.Errorf("swap %s lost its sender — the edit replaced the row instead of one field", id)
		}
	}
}

// The connection settings have to actually take. They are passed as text in a
// URL, so a driver that spells them differently accepts the string and ignores
// it — the store then runs without write-ahead logging or a busy timeout and
// says nothing, until two writers meet under load.
func TestConnectionSettingsApply(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode is %q, want wal — concurrent readers will block on the writer", mode)
	}
	var busy int
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busy != 5000 {
		t.Errorf("busy_timeout is %d, want 5000 — a contended write fails instead of waiting", busy)
	}
}

// An empty batch is the common case on a chain with no new trades, and it must
// not open a transaction or touch a row.
func TestValueSwapsEmptyIsInert(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	s.SeedSwap("s1", &SeedSwapData{Timestamp: 100, Pool: "0xpool", AmountUSD: "9.99"})
	if n, err := s.ValueSwaps(map[string]string{}); n != 0 || err != nil {
		t.Errorf("empty batch reported %d rows, err %v", n, err)
	}

	got, err := s.GetSwap(nil, "s1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	m, _ := got.(map[string]interface{})
	if m == nil || m["amountUSD"] != "9.99" {
		t.Errorf("empty batch disturbed the row: %v", m)
	}
}
