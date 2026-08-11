package indexer

// The day series — what a price chart is made of.
//
// A chart asks for a token or a pool, gets back one row per UTC day, and draws
// a candle from each. Every one of those rows was an empty list, so every chart
// on the site read "Missing chart data" — the client needs three points before
// it will draw anything at all.
//
// The rows are a FOLD OVER THE TRADES ALREADY INDEXED, not a new source. That
// is what makes the history appear the moment this ships rather than starting
// from the day it ships: the swaps have been on disk all along, dated by
// blocks.go, and a day is just how they are grouped.
//
// WHAT IS EXACT AND WHAT IS NOT
//
// A pool's candle is the pool's own execution ratio, amount0/amount1 — a fact
// of the trade, exact for every day however far back. A token's candle is that
// ratio carried to USD through the OTHER side of the trade, so it is exact
// against a stablecoin leg and, against a volatile leg, exact in ratio but
// anchored on what that leg is worth NOW. The alternative is an archive node
// and a price read at every historical block; the shape of the curve is the
// same either way, which is what a chart is for.
//
// Value locked is not reconstructible from trades at all — it is what the pools
// HOLD, which only the current balances say. So each day records it while it is
// today, and days that passed before this ran carry the volume and the candles
// they can be shown, and no locked value they cannot.

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/storage"
)

// trade is one stored swap, valued at the pass's prices. Amounts are absolute
// and decimal-applied; usd is meaningful only where ok says so.
type trade struct {
	id         string
	ts         int64
	block      uint64
	dated      bool
	pool       string
	t0, t1     string
	amt0, amt1 float64
	usd0, usd1 float64
	ok0, ok1   bool
}

// usd is a trade's size in dollars: the mid of the two quotes when both sides
// are priced, otherwise whichever side is.
func (t *trade) usd() (float64, bool) {
	switch {
	case t.ok0 && t.ok1:
		return (t.usd0 + t.usd1) / 2, true
	case t.ok0:
		return t.usd0, true
	case t.ok1:
		return t.usd1, true
	}
	return 0, false
}

// valuedTrades reads the stored swap window and values every leg, oldest first.
//
// Chronological order is not a nicety: open is the first price of the day and
// close is the last, and the store hands back a map, whose iteration order is
// deliberately random. Ordering by (time, block, log index) is the chain's own
// order — a log index is unique and increasing within its block.
func (idx *Indexer) valuedTrades(vps []valuedPool, tokens map[string]*storage.SeedTokenData, prices map[string]float64) []trade {
	byPool := map[string]*valuedPool{}
	for i := range vps {
		byPool[vps[i].id] = &vps[i]
	}
	swaps := idx.store.RecentSwapsRaw(maxValuedSwaps)
	out := make([]trade, 0, len(swaps))
	for id, sw := range swaps {
		vp := byPool[strings.ToLower(sw.Pool)]
		if vp == nil {
			continue
		}
		t := trade{id: id, ts: sw.Timestamp, block: sw.Block, dated: sw.Dated(), pool: vp.id, t0: vp.t0, t1: vp.t1}
		t.amt0, t.usd0, t.ok0 = swapLeg(sw.Amount0, vp.t0, tokens, prices)
		t.amt1, t.usd1, t.ok1 = swapLeg(sw.Amount1, vp.t1, tokens, prices)
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := &out[i], &out[j]
		if a.ts != b.ts {
			return a.ts < b.ts
		}
		if a.block != b.block {
			return a.block < b.block
		}
		return logIndexOf(a.id) < logIndexOf(b.id)
	})
	return out
}

// logIndexOf reads the log index out of a swap's id, which every handler writes
// as `txHash#logIndex`. Within a block the log index is unique and increasing
// across all transactions, so it IS the chain's ordering of two trades that
// share a second.
func logIndexOf(id string) uint64 {
	h := id[strings.LastIndex(id, "#")+1:]
	n, err := strconv.ParseUint(strings.TrimPrefix(h, "0x"), 16, 64)
	if err != nil {
		return 0
	}
	return n
}

// cell accumulates one subject's activity inside one UTC day.
type cell struct {
	subject                string
	date                   int64
	volume, volumeUSD      float64
	txCount                int64
	open, high, low, close float64
	priced                 bool
	locked, lockedUSD      float64
	held                   bool
}

// observe records a price seen at this point in the day. Called in
// chronological order, so the first fixes open and the last fixes close.
func (c *cell) observe(price float64) {
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return
	}
	if !c.priced {
		c.open, c.high, c.low, c.priced = price, price, price, true
	}
	if price > c.high {
		c.high = price
	}
	if price < c.low {
		c.low = price
	}
	c.close = price
}

// empty reports a day with nothing to say — no candle, no volume, no holdings.
// Writing it would put a blank point on a chart.
func (c *cell) empty() bool { return !c.priced && c.volumeUSD == 0 && !c.held }

// days is one subject's cells keyed by entity id.
type days map[string]*cell

// at returns the subject's cell for the day containing ts, creating it once.
func (d days) at(subject string, ts int64) *cell {
	date := dayStart(ts)
	id := dayID(subject, date)
	c := d[id]
	if c == nil {
		c = &cell{subject: subject, date: date}
		d[id] = c
	}
	return c
}

// snapshot is one valuation pass's view of the book: the trades it read, what
// everything is worth now, and what is held now. The day series is a fold over
// it, so it travels whole rather than as nine arguments.
type snapshot struct {
	now      int64
	trades   []trade
	prices   map[string]float64 // token → USD, now
	ratio    map[string]float64 // pool  → token0 per token1, now
	poolTVL  map[string]float64 // pool  → USD
	tokenTVL map[string]float64 // token → USD
	tokenBal map[string]float64 // token → token-denominated
	pools    map[string]*storage.SeedPoolData
	tokens   map[string]*storage.SeedTokenData
}

// writeSeries folds the snapshot into per-day cells and persists the ones that
// moved.
//
// Only what moved: a pass recomputes the whole history every interval, and
// rewriting every day of it every thirty seconds is the same O(rows) write
// storm that once stopped this pass from ever finishing. Yesterday cannot
// change unless a trade for yesterday arrives, so in the steady state exactly
// one day is written per subject that traded.
func (idx *Indexer) writeSeries(s snapshot) {
	tokenDays, poolDays, factoryDays := rollDays(s)

	if idx.written == nil {
		idx.written = map[string]cell{}
	}
	put := func(entityType string, c *cell, row interface{}) {
		key := entityType + ":" + dayID(c.subject, c.date)
		if prev, ok := idx.written[key]; ok && prev == *c {
			return
		}
		idx.store.SetEntity(entityType, dayID(c.subject, c.date), row)
		idx.written[key] = *c
	}

	for _, c := range tokenDays {
		if c.empty() {
			continue
		}
		put(storage.TokenDay, c, &engine.TokenDayData{
			ID:                  dayID(c.subject, c.date),
			Date:                c.date,
			Token:               tokenRef(c.subject, s.tokens),
			Volume:              fmtAmount(c.volume),
			VolumeUSD:           fmtUSD(c.volumeUSD),
			TotalValueLocked:    held(c.held, fmtAmount(c.locked)),
			TotalValueLockedUSD: held(c.held, fmtUSD(c.lockedUSD)),
			PriceUSD:            fmtPrice(c.close),
			Open:                fmtPrice(c.open),
			High:                fmtPrice(c.high),
			Low:                 fmtPrice(c.low),
			Close:               fmtPrice(c.close),
		})
	}

	for _, c := range poolDays {
		if c.empty() {
			continue
		}
		put(storage.PoolDay, c, &engine.PoolDayData{
			ID:                  dayID(c.subject, c.date),
			Date:                c.date,
			Pool:                poolRef(c.subject, s.pools, s.tokens),
			VolumeUSD:           fmtUSD(c.volumeUSD),
			TotalValueLockedUSD: held(c.held, fmtUSD(c.lockedUSD)),
			TxCount:             c.txCount,
			Open:                fmtPrice(c.open),
			High:                fmtPrice(c.high),
			Low:                 fmtPrice(c.low),
			Close:               fmtPrice(c.close),
		})
	}

	for _, c := range factoryDays {
		if c.empty() {
			continue
		}
		put(storage.FactoryDay, c, &engine.FactoryDayData{
			ID:                  dayID(c.subject, c.date),
			Date:                c.date,
			VolumeUSD:           fmtUSD(c.volumeUSD),
			TotalValueLockedUSD: held(c.held, fmtUSD(c.lockedUSD)),
			TxCount:             c.txCount,
		})
	}
}

// factorySubject is the singleton the protocol-wide series is keyed by, the
// same "1" the Factory entity uses.
const factorySubject = "1"

// rollDays folds the snapshot into the three day series. Trades first, in
// order, then what is true right now stamped onto today — the pass runs at the
// chain head, so the current price and the current holdings are the day's
// latest observation, and a day nobody traded in still has both.
func rollDays(s snapshot) (tokenDays, poolDays, factoryDays days) {
	tokenDays, poolDays, factoryDays = days{}, days{}, days{}

	for i := range s.trades {
		t := &s.trades[i]
		if !t.dated {
			// A trade whose time has not been read belongs to no day. Placing it
			// by the number an older build left there would put a candle in
			// January 1970, and nothing would ever come back to take it down.
			continue
		}
		usd, _ := t.usd()

		p := poolDays.at(t.pool, t.ts)
		p.txCount++
		p.volumeUSD += usd
		if t.amt1 > 0 {
			p.observe(t.amt0 / t.amt1)
		}

		f := factoryDays.at(factorySubject, t.ts)
		f.txCount++
		f.volumeUSD += usd

		// A token's price at this trade comes from the other leg: what the
		// counterparty was worth, over how much of this token changed hands.
		c0 := tokenDays.at(t.t0, t.ts)
		c0.volume += t.amt0
		if t.ok0 {
			c0.volumeUSD += t.usd0
		}
		if t.ok1 && t.amt0 > 0 {
			c0.observe(t.usd1 / t.amt0)
		}

		c1 := tokenDays.at(t.t1, t.ts)
		c1.volume += t.amt1
		if t.ok1 {
			c1.volumeUSD += t.usd1
		}
		if t.ok0 && t.amt1 > 0 {
			c1.observe(t.usd0 / t.amt1)
		}
	}

	var totalTVL float64
	for pool, tvl := range s.poolTVL {
		c := poolDays.at(pool, s.now)
		c.lockedUSD, c.held = tvl, true
		c.observe(s.ratio[pool])
		totalTVL += tvl
	}
	for token, price := range s.prices {
		c := tokenDays.at(token, s.now)
		c.observe(price)
	}
	for token, tvl := range s.tokenTVL {
		c := tokenDays.at(token, s.now)
		c.lockedUSD, c.locked, c.held = tvl, s.tokenBal[token], true
	}
	f := factoryDays.at(factorySubject, s.now)
	f.lockedUSD, f.held = totalTVL, true

	return tokenDays, poolDays, factoryDays
}

// tokenRef is the token a day belongs to, as the entity a client selects
// through — `tokenDayDatas { token { id } }` — and as what a `where: {token:
// "0x…"}` filter matches on.
func tokenRef(addr string, tokens map[string]*storage.SeedTokenData) *engine.Token {
	t := tokens[addr]
	if t == nil {
		t = tokens[strings.ToLower(addr)]
	}
	if t == nil {
		return &engine.Token{ID: addr}
	}
	// The whole token, not a stub of it: a client that selects through the
	// reference reads the same figures the `tokens` collection serves, rather
	// than a row claiming this token has never traded.
	return &engine.Token{
		ID: addr, Symbol: t.Symbol, Name: t.Name, Decimals: t.Decimals,
		VolumeUSD: t.VolumeUSD, TotalValueLockedUSD: t.TotalValueLockedUSD,
		DerivedETH: t.DerivedETH, TxCount: t.TxCount,
	}
}

// poolRef is the pool a day belongs to — see tokenRef.
func poolRef(id string, pools map[string]*storage.SeedPoolData, tokens map[string]*storage.SeedTokenData) *engine.Pool {
	p := pools[id]
	if p == nil {
		return &engine.Pool{ID: id}
	}
	return &engine.Pool{
		ID:      id,
		Token0:  tokenRef(strings.ToLower(p.Token0), tokens),
		Token1:  tokenRef(strings.ToLower(p.Token1), tokens),
		FeeTier: p.FeeTier,
	}
}

// held reports what was observed on a day that recorded it, and nothing at all
// on a day that did not.
//
// What a pool holds is not in its trades — only the balances say, and they only
// say what is true now. So a day that passed before this ran has volume and
// candles it can be shown and no locked value it cannot. "0.00" there would
// claim the pool was empty, and a chart would draw that claim as a cliff.
func held(observed bool, v string) string {
	if !observed {
		return ""
	}
	return v
}

// fmtAmount renders a token-denominated quantity at full significance. fmtUSD's
// two decimals are a currency format: a token amount is legitimately a
// millionth of a unit, and unlike a price, zero of it is a real answer.
func fmtAmount(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	return strconv.FormatFloat(v, 'g', 12, 64)
}
