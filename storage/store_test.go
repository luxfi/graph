package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestFilterResults_ExactMatch(t *testing.T) {
	results := []interface{}{
		map[string]interface{}{"id": "1", "pool": "0xA"},
		map[string]interface{}{"id": "2", "pool": "0xB"},
		map[string]interface{}{"id": "3", "pool": "0xA"},
	}

	where := map[string]interface{}{"pool": "0xA"}
	filtered := FilterResults(results, where)

	if len(filtered) != 2 {
		t.Fatalf("expected 2, got %d", len(filtered))
	}
}

func TestFilterResults_GTE(t *testing.T) {
	results := []interface{}{
		map[string]interface{}{"id": "1", "date": int64(100)},
		map[string]interface{}{"id": "2", "date": int64(200)},
		map[string]interface{}{"id": "3", "date": int64(300)},
	}

	where := map[string]interface{}{"date_gte": "200"}
	filtered := FilterResults(results, where)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 (date >= 200), got %d", len(filtered))
	}
}

func TestFilterResults_LT(t *testing.T) {
	results := []interface{}{
		map[string]interface{}{"id": "1", "amount": "50.5"},
		map[string]interface{}{"id": "2", "amount": "150.0"},
		map[string]interface{}{"id": "3", "amount": "200.0"},
	}

	where := map[string]interface{}{"amount_lt": "100"}
	filtered := FilterResults(results, where)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 (amount < 100), got %d", len(filtered))
	}
}

func TestFilterResults_Empty(t *testing.T) {
	results := []interface{}{
		map[string]interface{}{"id": "1"},
	}
	filtered := FilterResults(results, nil)
	if len(filtered) != 1 {
		t.Fatal("nil where should return all")
	}

	filtered = FilterResults(results, map[string]interface{}{})
	if len(filtered) != 1 {
		t.Fatal("empty where should return all")
	}
}

func TestFilterResults_NoMatch(t *testing.T) {
	results := []interface{}{
		map[string]interface{}{"id": "1", "pool": "0xA"},
	}
	where := map[string]interface{}{"pool": "0xNONE"}
	filtered := FilterResults(results, where)
	if len(filtered) != 0 {
		t.Fatalf("expected 0, got %d", len(filtered))
	}
}

func TestSortResults_Numeric(t *testing.T) {
	results := []interface{}{
		map[string]interface{}{"id": "a", "volumeUSD": "9.0"},
		map[string]interface{}{"id": "b", "volumeUSD": "100000.0"},
		map[string]interface{}{"id": "c", "volumeUSD": "50.0"},
	}

	sortResults(results, "volumeUSD", "desc")

	first := results[0].(map[string]interface{})
	if first["id"] != "b" {
		t.Errorf("expected b (100000) first in desc sort, got %v", first["id"])
	}

	last := results[2].(map[string]interface{})
	if last["id"] != "a" {
		t.Errorf("expected a (9) last in desc sort, got %v", last["id"])
	}
}

func TestSortResults_Asc(t *testing.T) {
	results := []interface{}{
		map[string]interface{}{"id": "x", "date": int64(300)},
		map[string]interface{}{"id": "y", "date": int64(100)},
		map[string]interface{}{"id": "z", "date": int64(200)},
	}

	sortResults(results, "date", "asc")

	first := results[0].(map[string]interface{})
	if first["id"] != "y" {
		t.Errorf("expected y (100) first in asc sort, got %v", first["id"])
	}
}

func TestSortResults_StringFallback(t *testing.T) {
	results := []interface{}{
		map[string]interface{}{"id": "c", "name": "Charlie"},
		map[string]interface{}{"id": "a", "name": "Alice"},
		map[string]interface{}{"id": "b", "name": "Bob"},
	}

	sortResults(results, "name", "asc")

	first := results[0].(map[string]interface{})
	if first["name"] != "Alice" {
		t.Errorf("expected Alice first, got %v", first["name"])
	}
}

func TestCompareValues_NumericStrings(t *testing.T) {
	// "9" vs "100000" — must sort numerically, not lexicographically
	cmp := compareValues("9.0", "100000.0")
	if cmp >= 0 {
		t.Error("9.0 should be less than 100000.0")
	}

	cmp = compareValues("100000", "9")
	if cmp <= 0 {
		t.Error("100000 should be greater than 9")
	}

	// Equal
	cmp = compareValues("42", "42")
	if cmp != 0 {
		t.Error("42 should equal 42")
	}
}

func TestCompareValues_MixedTypes(t *testing.T) {
	// int64 vs string
	cmp := compareValues(int64(100), "50")
	if cmp <= 0 {
		t.Error("100 should be greater than 50")
	}

	// Non-numeric strings fall back to lexicographic
	cmp = compareValues("abc", "xyz")
	if cmp >= 0 {
		t.Error("abc should be less than xyz lexicographically")
	}
}

// A quiet pool's trades are its own, however much the busy pool beside it trades.
//
// The limit belongs to the database, so a filter that runs afterwards filters
// what the limit already threw away: asking for one pool's last N trades read
// the N newest chain-wide and kept the few that matched. One pair does most of
// the trading on a live chain and fills any recent window by itself, so every
// other pool answered with nothing — its history present, and unreachable.
func TestGetSwapsFiltersBeforeItLimits(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	const busy, quiet = "0xbusy", "0xquiet"

	// The quiet pool trades first, then the busy one buries it.
	for i := 0; i < 5; i++ {
		s.SeedSwap(fmt.Sprintf("q%d", i), &SeedSwapData{
			Timestamp: int64(1000 + i), Block: uint64(1 + i), Pool: quiet,
			Amount0: "1", Amount1: "-2", AmountUSD: "1",
		})
	}
	for i := 0; i < 500; i++ {
		s.SeedSwap(fmt.Sprintf("b%d", i), &SeedSwapData{
			Timestamp: int64(2000 + i), Block: uint64(100 + i), Pool: busy,
			Amount0: "1", Amount1: "-2", AmountUSD: "1",
		})
	}

	got, err := s.GetSwaps(context.Background(), 100, "timestamp", "desc",
		map[string]interface{}{"pool": quiet})
	if err != nil {
		t.Fatalf("GetSwaps: %v", err)
	}
	rows, _ := got.([]interface{})
	if len(rows) != 5 {
		t.Errorf("quiet pool returned %d of its 5 trades — the limit ran before the filter", len(rows))
	}
	for _, r := range rows {
		m := r.(map[string]interface{})
		id, _ := m["id"].(string)
		if !strings.HasPrefix(id, "q") {
			t.Errorf("row %q is not the filtered pool's", id)
		}
	}
}
