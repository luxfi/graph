package storage

import (
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
