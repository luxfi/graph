package indexer

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luxfi/graph/storage"
)

// The day series is what a price chart draws. These assert it from the swaps on
// disk all the way to the wire the client reads, because every part in between
// was individually plausible while the charts were blank: the swaps were
// indexed, the type was declared, the resolver was registered, and the store
// answered every one of them with [].
//
// A client needs THREE points before it draws anything at all — fewer and it
// renders "Missing chart data", the same as an error. So "not empty" is not the
// bar; a series has to cover the history that is already indexed.

const (
	tLUSD = "0x000000000000000000000000000000000000c0de" // stable anchor, $1
	tLUX  = "0x000000000000000000000000000000000000face" // native, priced by pA
	tZOO  = "0x000000000000000000000000000000000000beef" // priced only via tLUX
	pStbl = "0x00000000000000000000000000000000000000a1" // LUSD/LUX
	pVol  = "0x00000000000000000000000000000000000000b2" // LUX/ZOO

	// day0 is a UTC midnight (2026-01-01). Every assertion below is relative to
	// it, so the arithmetic is readable rather than a wall of epoch seconds.
	day0 = 1767225600
)

// book is a two-pool market: a stablecoin pool that anchors the native unit at
// $0.50, and a volatile pool that prices ZOO one hop further out at $0.05.
// Nothing here quotes ZOO against a stablecoin — that is the point of it.
func book(t *testing.T) *Indexer {
	t.Helper()
	s := newMemSQLiteStore(t)
	s.SeedToken(tLUSD, &storage.SeedTokenData{Symbol: "LUSD", Name: "Lux Dollar", Decimals: 18})
	s.SeedToken(tLUX, &storage.SeedTokenData{Symbol: "WLUX", Name: "Wrapped LUX", Decimals: 18})
	s.SeedToken(tZOO, &storage.SeedTokenData{Symbol: "LZOO", Name: "Lux ZOO", Decimals: 18})
	s.SeedPool(pStbl, &storage.SeedPoolData{Token0: tLUSD, Token1: tLUX, FeeTier: 3000})
	s.SeedPool(pVol, &storage.SeedPoolData{Token0: tLUX, Token1: tZOO, FeeTier: 3000})

	srv := balanceRPC(t, map[string]string{
		tLUSD + "@" + pStbl: hexWord(1000),  // $1000 against
		tLUX + "@" + pStbl:  hexWord(2000),  // 2000 LUX  ⇒ LUX = $0.50
		tLUX + "@" + pVol:   hexWord(4000),  // $2000 against
		tZOO + "@" + pVol:   hexWord(40000), // 40000 ZOO ⇒ ZOO = $0.05
	})
	return NewWithConfig(Config{RPC: srv.URL, Native: tLUX}, s)
}

// wei renders a whole token amount in 18-decimal base units.
func wei(n int64) string {
	return new(big.Int).Mul(big.NewInt(n), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)).String()
}

// trade0 records one swap in the volatile pool: `in` LUX enters, `out` ZOO
// leaves. The execution price of ZOO is therefore in/out LUX, which at $0.50
// per LUX is what the day's candle must show.
func trade0(idx *Indexer, id string, ts int64, block uint64, in, out int64) {
	idx.store.SeedSwap(id, &storage.SeedSwapData{
		Timestamp: ts, Block: block, Pool: pVol,
		Amount0: wei(in), Amount1: "-" + wei(out), AmountUSD: "0", Sender: "0xabc",
	})
}

// history is three days of trading in the volatile pool. Day 0 moves: 0.05 →
// 0.10 → 0.025, so its candle opens at 0.05, peaks at 0.10, bottoms at 0.025
// and closes at 0.025.
func history(idx *Indexer) {
	trade0(idx, "0xd0a#0x1", day0+100, 10, 100, 1000) // ZOO @ $0.050
	trade0(idx, "0xd0b#0x1", day0+200, 11, 100, 500)  // ZOO @ $0.100
	trade0(idx, "0xd0c#0x1", day0+300, 12, 100, 2000) // ZOO @ $0.025
	trade0(idx, "0xd1a#0x1", day0+86400+10, 20, 200, 1000)
	trade0(idx, "0xd2a#0x1", day0+2*86400+10, 30, 300, 1000)
}

// rows reads a day series off the wire exactly as a resolver does.
func rows(t *testing.T, get func(context.Context, int, string, string, map[string]interface{}) (interface{}, error),
	limit int, where map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, err := get(context.Background(), limit, "date", "desc", where)
	if err != nil {
		t.Fatalf("read day series: %v", err)
	}
	list, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("day series is %T, want a list", raw)
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, r := range list {
		m, ok := r.(map[string]interface{})
		if !ok {
			t.Fatalf("day row is %T, want an object", r)
		}
		out = append(out, m)
	}
	return out
}

func num(t *testing.T, m map[string]interface{}, field string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(fmt.Sprint(m[field]), 64)
	if err != nil {
		t.Fatalf("%s = %v, not a number", field, m[field])
	}
	return v
}

func near(got, want float64) bool {
	if want == 0 {
		return math.Abs(got) < 1e-9
	}
	return math.Abs(got-want)/math.Abs(want) < 1e-6
}

// The swaps already indexed must become a series, not just the ones that arrive
// after this ships. A chart that starts today is not a chart.
func TestSeriesCoversIndexedHistory(t *testing.T) {
	idx := book(t)
	history(idx)
	idx.revalue(context.Background())

	got := rows(t, idx.store.GetTokenDayDatas, 100, map[string]interface{}{"token": tZOO})
	if len(got) < 3 {
		t.Fatalf("ZOO has %d day rows; a client draws nothing under 3 and renders \"Missing chart data\"", len(got))
	}

	// Three traded days, newest first, each a distinct UTC midnight. (The pass
	// also records today, which is where value locked comes from.)
	var traded []map[string]interface{}
	for _, r := range got {
		if d := int64(num(t, r, "date")); d >= day0 && d <= day0+2*86400 {
			traded = append(traded, r)
		}
	}
	if len(traded) != 3 {
		t.Fatalf("expected the 3 traded days, got %d of %d rows", len(traded), len(got))
	}
	for i, want := range []int64{day0 + 2*86400, day0 + 86400, day0} {
		if d := int64(num(t, traded[i], "date")); d != want {
			t.Errorf("row %d date = %d, want %d (descending UTC midnights)", i, d, want)
		}
		if d := int64(num(t, traded[i], "date")); d%86400 != 0 {
			t.Errorf("date %d is not a UTC midnight", d)
		}
	}
}

// Open is the first price of the day and close the last, whatever order the
// store hands the trades back in — and it hands them back from a map, whose
// order is deliberately random.
func TestDayCandleIsTheDaysPrices(t *testing.T) {
	idx := book(t)
	history(idx)
	idx.revalue(context.Background())

	var day map[string]interface{}
	for _, r := range rows(t, idx.store.GetTokenDayDatas, 100, map[string]interface{}{"token": tZOO}) {
		if int64(num(t, r, "date")) == day0 {
			day = r
		}
	}
	if day == nil {
		t.Fatal("no row for the first traded day")
	}
	for _, c := range []struct {
		field string
		want  float64
	}{
		{"open", 0.05},   // first trade of the day
		{"high", 0.10},   // second
		{"low", 0.025},   // third
		{"close", 0.025}, // last
		{"priceUSD", 0.025},
	} {
		if got := num(t, day, c.field); !near(got, c.want) {
			t.Errorf("%s = %g, want %g", c.field, got, c.want)
		}
	}
	// 1000 + 500 + 2000 ZOO changed hands, at $0.05.
	if got := num(t, day, "volume"); !near(got, 3500) {
		t.Errorf("volume = %g, want 3500 ZOO", got)
	}
	if got := num(t, day, "volumeUSD"); !near(got, 175) {
		t.Errorf("volumeUSD = %g, want 175", got)
	}

	// The other side of the same trades. Both tokens in a pool have a candle,
	// and each is priced off the OTHER leg — priced off its own, every candle in
	// the series would be a flat line at today's price.
	var lux map[string]interface{}
	for _, r := range rows(t, idx.store.GetTokenDayDatas, 100, map[string]interface{}{"token": tLUX}) {
		if int64(num(t, r, "date")) == day0 {
			lux = r
		}
	}
	if lux == nil {
		t.Fatal("the pool's other token has no day")
	}
	for _, c := range []struct {
		field string
		want  float64
	}{
		{"open", 0.5}, // 1000 ZOO out for 100 LUX in, at $0.05
		{"high", 1.0}, // 2000 ZOO out for 100 LUX in
		{"low", 0.25}, // 500 ZOO out for 100 LUX in
		{"close", 1.0},
	} {
		if got := num(t, lux, c.field); !near(got, c.want) {
			t.Errorf("LUX %s = %g, want %g", c.field, got, c.want)
		}
	}
}

// A day ends at UTC midnight. Two trades two seconds apart across it are two
// days, and an off-by-one boundary merges them into one candle that never
// happened.
func TestMidnightSplitsTheDay(t *testing.T) {
	idx := book(t)
	trade0(idx, "0xlate#0x1", day0+86400-1, 40, 100, 1000)  // 23:59:59
	trade0(idx, "0xearly#0x1", day0+86400+1, 41, 200, 1000) // 00:00:01
	idx.revalue(context.Background())

	seen := map[int64]bool{}
	for _, r := range rows(t, idx.store.GetPoolDayDatas, 100, map[string]interface{}{"pool": pVol}) {
		seen[int64(num(t, r, "date"))] = true
	}
	if !seen[day0] || !seen[day0+86400] {
		t.Fatalf("two seconds either side of midnight must be two days; days present: %v", seen)
	}
	// And each keeps its own price rather than one swallowing the other.
	for _, c := range []struct {
		date  int64
		close float64
	}{{day0, 0.1}, {day0 + 86400, 0.2}} {
		for _, r := range rows(t, idx.store.GetPoolDayDatas, 100, map[string]interface{}{"pool": pVol}) {
			if int64(num(t, r, "date")) != c.date {
				continue
			}
			if got := num(t, r, "close"); !near(got, c.close) {
				t.Errorf("day %d close = %g, want %g", c.date, got, c.close)
			}
		}
	}
}

// The trade stream is the chain's order, not the store's. Two trades in one
// block are ordered by log index, which is hex — a string compare puts 0x10
// before 0x2.
func TestTradesAreChronological(t *testing.T) {
	// A log index is hex. Read as text, 0x10 sorts before 0x2 and a block's
	// trades come back shuffled — which is exactly what open and close are.
	for _, c := range []struct {
		id   string
		want uint64
	}{{"0xaa#0x2", 2}, {"0xaa#0x10", 16}, {"0xaa#0xff", 255}} {
		if got := logIndexOf(c.id); got != c.want {
			t.Errorf("logIndexOf(%s) = %d, want %d", c.id, got, c.want)
		}
	}

	idx := book(t)
	// Ids chosen so that sorting them as text reverses time.
	trade0(idx, "0xzz#0x1", day0+10, 10, 100, 1000)
	trade0(idx, "0xyy#0x1", day0+20, 11, 100, 1000)
	trade0(idx, "0xaa#0x2", day0+30, 12, 100, 1000)
	trade0(idx, "0xaa#0x10", day0+30, 12, 100, 1000) // same block, later log

	vps := []valuedPool{{id: pVol, t0: tLUX, t1: tZOO, bal0: 4000, bal1: 40000, held0: true, held1: true}}
	trades := idx.valuedTrades(vps, idx.store.TokensRaw(), map[string]float64{tLUX: 0.5})

	if len(trades) != 4 {
		t.Fatalf("got %d trades, want 4", len(trades))
	}
	for i := 1; i < len(trades); i++ {
		a, b := trades[i-1], trades[i]
		if a.ts > b.ts {
			t.Fatalf("trade %d (%d) is later than trade %d (%d)", i-1, a.ts, i, b.ts)
		}
		if a.ts == b.ts && logIndexOf(a.id) > logIndexOf(b.id) {
			t.Fatalf("same second: %s came before %s; log index orders a block", a.id, b.id)
		}
	}
	if trades[2].id != "0xaa#0x2" || trades[3].id != "0xaa#0x10" {
		t.Errorf("in-block order = %s, %s; want 0xaa#0x2 then 0xaa#0x10", trades[2].id, trades[3].id)
	}
}

// ZOO touches no stablecoin. Its price exists only because the pool graph
// relaxes one out from the anchor, and the series has to carry that through —
// otherwise every token but the two anchors charts as blank.
func TestTokenWithNoStablePairStillPrices(t *testing.T) {
	idx := book(t)
	history(idx)
	idx.revalue(context.Background())

	for _, r := range rows(t, idx.store.GetTokenDayDatas, 100, map[string]interface{}{"token": tZOO}) {
		if fmt.Sprint(r["priceUSD"]) == "" {
			t.Fatalf("day %v has no price; a token one hop from a stablecoin is still priced", r["date"])
		}
		if num(t, r, "volumeUSD") <= 0 && int64(num(t, r, "date")) <= day0+2*86400 {
			t.Errorf("day %v traded but is worth $0", r["date"])
		}
	}
}

// The chart asks for one token by address, and the address it sends is whatever
// its wallet or URL carried — checksummed as often as not.
func TestWhereFiltersByTokenWhateverTheCase(t *testing.T) {
	idx := book(t)
	history(idx)
	idx.revalue(context.Background())

	all := rows(t, idx.store.GetTokenDayDatas, 1000, nil)
	if len(all) < 4 {
		t.Fatalf("expected rows for several tokens, got %d", len(all))
	}
	checksummed := "0x" + strings.ToUpper(strings.TrimPrefix(tZOO, "0x"))
	for _, where := range []map[string]interface{}{{"token": tZOO}, {"token": checksummed}} {
		got := rows(t, idx.store.GetTokenDayDatas, 1000, where)
		if len(got) == 0 {
			t.Fatalf("where %v matched nothing", where)
		}
		if len(got) == len(all) {
			t.Fatalf("where %v matched every row — the filter is not applied", where)
		}
		for _, r := range got {
			tok, _ := r["token"].(map[string]interface{})
			if tok == nil || !strings.EqualFold(fmt.Sprint(tok["id"]), tZOO) {
				t.Fatalf("where %v returned a row for %v", where, r["token"])
			}
		}
	}
}

// first + orderBy are what turn a series into "the last 90 days".
func TestOrderAndLimit(t *testing.T) {
	idx := book(t)
	history(idx)
	idx.revalue(context.Background())

	where := map[string]interface{}{"token": tZOO}
	desc := rows(t, idx.store.GetTokenDayDatas, 1000, where)
	for i := 1; i < len(desc); i++ {
		if num(t, desc[i-1], "date") < num(t, desc[i], "date") {
			t.Fatalf("orderDirection desc is not descending: %v then %v", desc[i-1]["date"], desc[i]["date"])
		}
	}

	two := rows(t, idx.store.GetTokenDayDatas, 2, where)
	if len(two) != 2 {
		t.Fatalf("first: 2 returned %d rows", len(two))
	}
	// The cut must come AFTER the sort, or "the newest two" is "any two".
	for i := range two {
		if num(t, two[i], "date") != num(t, desc[i], "date") {
			t.Errorf("first: 2 row %d is day %v, want the newest %v", i, two[i]["date"], desc[i]["date"])
		}
	}

	raw, err := idx.store.GetTokenDayDatas(context.Background(), 100, "date", "asc", where)
	if err != nil {
		t.Fatal(err)
	}
	asc := raw.([]interface{})
	first := asc[0].(map[string]interface{})
	if num(t, first, "date") != num(t, desc[len(desc)-1], "date") {
		t.Errorf("asc starts at %v, desc ends at %v", first["date"], desc[len(desc)-1]["date"])
	}
}

// The pool series is the pool's own execution ratio, and it carries the value
// locked that the explore page reads.
func TestPoolSeriesAndProtocolTotal(t *testing.T) {
	idx := book(t)
	history(idx)
	idx.revalue(context.Background())

	var day map[string]interface{}
	for _, r := range rows(t, idx.store.GetPoolDayDatas, 100, map[string]interface{}{"pool": pVol}) {
		if int64(num(t, r, "date")) == day0 {
			day = r
		}
	}
	if day == nil {
		t.Fatal("no pool row for the first traded day")
	}
	// token0 per token1 across the day: 0.1 → 0.2 → 0.05.
	for _, c := range []struct {
		field string
		want  float64
	}{{"open", 0.1}, {"high", 0.2}, {"low", 0.05}, {"close", 0.05}} {
		if got := num(t, day, c.field); !near(got, c.want) {
			t.Errorf("pool %s = %g, want %g", c.field, got, c.want)
		}
	}
	if got := num(t, day, "txCount"); got != 3 {
		t.Errorf("txCount = %g, want 3", got)
	}
	// $50.00 + $37.50 + $75.00, each the mid of the two priced legs.
	if got := num(t, day, "volumeUSD"); !near(got, 162.5) {
		t.Errorf("pool volumeUSD = %g, want 162.50", got)
	}
	if pool, _ := day["pool"].(map[string]interface{}); pool == nil || !strings.EqualFold(fmt.Sprint(pool["id"]), pVol) {
		t.Errorf("pool row does not carry its pool: %v", day["pool"])
	}

	// What the pool held on a day that passed before this ran is not in the
	// trades. Reporting it as $0 would draw a cliff to zero liquidity.
	if got := fmt.Sprint(day["tvlUSD"]); got != "" {
		t.Errorf("a past day claims tvlUSD %q; it was never observed", got)
	}

	// The protocol total behind the explore page's tiles.
	protocol := rows(t, idx.store.GetFactoryDayDatas, 1000, nil)
	if len(protocol) < 3 {
		t.Fatalf("protocol series has %d days, want the traded history", len(protocol))
	}
	for _, r := range protocol {
		if int64(num(t, r, "date")) == day0 && !near(num(t, r, "volumeUSD"), 162.5) {
			t.Errorf("protocol volumeUSD = %v, want 162.50", r["volumeUSD"])
		}
	}
	today := dayStart(time.Now().Unix())
	var tvl float64
	for _, r := range protocol {
		if int64(num(t, r, "date")) == today {
			tvl = num(t, r, "tvlUSD")
		}
	}
	// $1000 + 2000 LUX @ $0.50, and $2000 + 40000 ZOO @ $0.05.
	if !near(tvl, 6000) {
		t.Errorf("protocol tvlUSD today = %g, want 6000", tvl)
	}
}

// A pass recomputes the whole history every interval. Rewriting all of it every
// time is the write storm that once stopped this pass from ever finishing, so a
// day that did not move must not be written at all.
func TestUnchangedDaysAreNotRewritten(t *testing.T) {
	idx := book(t)
	history(idx)
	idx.revalue(context.Background())

	// Vandalise a settled day. If the next pass writes it back, it wrote a row
	// nothing had changed.
	id := dayID(pVol, day0)
	idx.store.SetEntity(storage.PoolDay, id, map[string]interface{}{"id": id, "date": day0, "close": "vandal"})
	idx.revalue(context.Background())

	got, err := idx.store.GetByType(storage.PoolDay, id)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := got.(map[string]interface{})
	if m == nil || fmt.Sprint(m["close"]) != "vandal" {
		t.Fatalf("a settled day was rewritten though nothing about it changed: %v", got)
	}

	// A day that DID move must still be written.
	trade0(idx, "0xnew#0x1", day0+400, 13, 100, 4000)
	idx.revalue(context.Background())
	got, _ = idx.store.GetByType(storage.PoolDay, id)
	m, _ = got.(map[string]interface{})
	if m == nil || fmt.Sprint(m["close"]) == "vandal" {
		t.Fatalf("a new trade on that day did not reach it: %v", got)
	}
	if got := fmt.Sprint(m["low"]); got != "0.025" {
		t.Errorf("low = %s, want 0.025 (the new trade is the day's cheapest)", got)
	}
}

// An undated swap belongs to no day. It is what an older build wrote, and
// healSwapTimes dates it from its block — until then it must not invent one.
func TestUndatedSwapIsNotADay(t *testing.T) {
	idx := book(t)
	idx.store.SeedSwap("0xold#0x1", &storage.SeedSwapData{
		Timestamp: 1136227, Pool: pVol, // a BLOCK NUMBER, as the old build stored it
		Amount0: wei(100), Amount1: "-" + wei(1000), AmountUSD: "0",
	})
	idx.revalue(context.Background())

	today := dayStart(time.Now().Unix())
	for _, r := range rows(t, idx.store.GetPoolDayDatas, 100, map[string]interface{}{"pool": pVol}) {
		if d := int64(num(t, r, "date")); d != today {
			t.Errorf("a swap dated by block number produced day %d (%s)", d,
				time.Unix(d, 0).UTC().Format("2006-01-02"))
		}
	}
}

// A row's token reference is the token, not a stub of it. A client that selects
// through it reads the same figures the `tokens` collection serves.
func TestTokenReferenceCarriesTheToken(t *testing.T) {
	idx := book(t)
	history(idx)
	idx.revalue(context.Background())

	rowsFor := rows(t, idx.store.GetTokenDayDatas, 100, map[string]interface{}{"token": tZOO})
	tok, _ := rowsFor[0]["token"].(map[string]interface{})
	if tok == nil {
		t.Fatal("no token on the row")
	}
	live, ok := idx.store.TokensRaw()[tZOO]
	if !ok {
		t.Fatal("token absent from the store")
	}
	for _, c := range []struct{ field, want string }{
		{"symbol", live.Symbol},
		{"derivedETH", live.DerivedETH},
		{"volumeUSD", live.VolumeUSD},
		{"totalValueLockedUSD", live.TotalValueLockedUSD},
	} {
		if got := fmt.Sprint(tok[c.field]); got != c.want {
			t.Errorf("token.%s = %q, want %q", c.field, got, c.want)
		}
	}
	if live.DerivedETH == "" {
		t.Error("this test proves nothing while the token has no price")
	}
}
