package storage

import (
	"context"
	"fmt"
	"testing"
)

// The day-series readers, against whichever backend the build links. The
// indexer writes these rows once and both backends must answer for them
// identically — production runs on sqlite, and every test that drives the
// indexer runs on the in-memory store, so a divergence here is invisible from
// either side alone.

const (
	zoo  = "0x000000000000000000000000000000000000beef"
	lux  = "0x000000000000000000000000000000000000face"
	dayA = 1767225600
)

func seriesStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	for i, addr := range []string{zoo, zoo, zoo, lux} {
		date := int64(dayA + i*86400)
		if addr == lux {
			date = dayA
		}
		s.SetEntity(TokenDay, fmt.Sprintf("%s-%d", addr, date/86400), map[string]interface{}{
			"id":    fmt.Sprintf("%s-%d", addr, date/86400),
			"date":  date,
			"token": map[string]interface{}{"id": addr, "symbol": "T"},
			"close": fmt.Sprint(i + 1),
		})
	}
	return s
}

func list(t *testing.T, v interface{}, err error) []map[string]interface{} {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := v.([]interface{})
	if !ok {
		t.Fatalf("result is %T, want a list", v)
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		m, ok := r.(map[string]interface{})
		if !ok {
			t.Fatalf("row is %T, want an object — the backends disagree about what a stored entity is", r)
		}
		out = append(out, m)
	}
	return out
}

// A chart asks for one token, newest first, capped. All three arguments have to
// apply, and in that order.
func TestTokenDayDatasFilterOrderLimit(t *testing.T) {
	s := seriesStore(t)
	ctx := context.Background()
	read := func(v interface{}, err error) []map[string]interface{} { return list(t, v, err) }

	all := read(s.GetTokenDayDatas(ctx, 100, "date", "desc", nil))
	if len(all) != 4 {
		t.Fatalf("stored 4 rows, read %d", len(all))
	}

	mine := read(s.GetTokenDayDatas(ctx, 100, "date", "desc", map[string]interface{}{"token": zoo}))
	if len(mine) != 3 {
		t.Fatalf("where token matched %d rows, want 3", len(mine))
	}
	for i := 1; i < len(mine); i++ {
		if fmt.Sprint(mine[i-1]["date"]) <= fmt.Sprint(mine[i]["date"]) {
			t.Fatalf("not descending: %v then %v", mine[i-1]["date"], mine[i]["date"])
		}
	}

	// The newest two, not any two: the cut comes after the sort.
	two := read(s.GetTokenDayDatas(ctx, 2, "date", "desc", map[string]interface{}{"token": zoo}))
	if len(two) != 2 {
		t.Fatalf("first: 2 returned %d rows", len(two))
	}
	if fmt.Sprint(two[0]["date"]) != fmt.Sprint(mine[0]["date"]) {
		t.Errorf("first: 2 starts at %v, want the newest %v", two[0]["date"], mine[0]["date"])
	}

	// The address arrives however the caller had it.
	up := read(s.GetTokenDayDatas(ctx, 100, "date", "desc",
		map[string]interface{}{"token": "0x000000000000000000000000000000000000BEEF"}))
	if len(up) != 3 {
		t.Errorf("checksummed address matched %d rows, want 3", len(up))
	}

	// And the range filter the 24h tiles use.
	recent := read(s.GetTokenDayDatas(ctx, 100, "date", "desc",
		map[string]interface{}{"date_gte": dayA + 86400}))
	if len(recent) != 2 {
		t.Errorf("date_gte matched %d rows, want 2", len(recent))
	}
}

// An unpopulated series is an empty list, never null — see ListByType.
func TestEmptySeriesIsAList(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, get := range map[string]func(context.Context, int, string, string, map[string]interface{}) (interface{}, error){
		"tokenDayDatas":   s.GetTokenDayDatas,
		"poolDayDatas":    s.GetPoolDayDatas,
		"factoryDayDatas": s.GetFactoryDayDatas,
	} {
		v, err := get(context.Background(), 10, "date", "desc", nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rows, ok := v.([]interface{})
		if !ok || rows == nil {
			t.Errorf("%s answered %#v; an idle series is [], not null", name, v)
		}
	}
}
