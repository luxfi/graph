package indexer

// Valuation — the one owner of the AMM graph's derived USD aggregates.
//
// WHY THIS IS A PASS AND NOT AN EVENT HANDLER
//
// Reserves are a fact about the chain's CURRENT state, not an event stream. The
// event-sourced route to them (V2 `Sync`, plus V3/V4 Mint/Burn/Swap liquidity
// math) has three defects here:
//
//   - It is not universal. `Sync` exists only on V2 pairs. A V3 or V4 pool never
//     emits it, so an event-only TVL would silently cover a fraction of the book.
//   - It only converges by replaying ALL history. A pool whose last `Sync` is a
//     million blocks back reads as empty until it trades again — and on an idle
//     chain it never trades again.
//   - It is a second way to learn one fact. `balanceOf(pool)` answers "what does
//     this pool hold" for V2, V3 and V4 identically, in one call, at head.
//
// So the log indexer owns EVENTS (who created a pool, who swapped) and this pass
// owns DERIVED STATE (what the pool holds, what it is worth). processLog's
// Sync/Mint/Burn arm stays a no-op on purpose: honoring it would be a second,
// partial writer for a field this pass already owns.
//
// PRICING
//
// No external oracle, no paid feed, no hardcoded price. Stablecoins anchor at
// $1 by symbol; every other token's price is relaxed outward from that anchor
// through the pools themselves, each unknown token taking its price from the
// DEEPEST pool that already has a priced side. Depth-ordering makes the result
// deterministic and picks the least manipulable quote when several pools
// disagree. A token no pool connects to a stable stays unpriced and contributes
// nothing — an unknown value is reported as unknown, never guessed.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/luxfi/graph/storage"
)

// valuationInterval is how often the derived USD aggregates are recomputed.
// The pass is O(pools) eth_calls, so it runs on its own slow cadence rather
// than on the 2s block poll.
const valuationInterval = 30 * time.Second

// maxValuedPools caps one pass. Far above any real book; a guard, not a policy.
const maxValuedPools = 5000

// maxValuedSwaps bounds the swap history one pass summarises, newest first.
//
// This one IS a policy, so it is stated rather than buried: a chain's swap
// table grows forever, and a pass that rescans all of it does more work every
// day the chain lives. Beyond this many trades the reported volume is the
// volume of the most recent maxValuedSwaps of them, and the pass says so in its
// log rather than presenting a partial sum as a total.
const maxValuedSwaps = 50000

// selBalanceOf is `balanceOf(address)`.
const selBalanceOf = "0x70a08231"

// stableSymbols are the USD-pegged symbols that anchor the price graph at $1.
// Keyed by upper-cased ERC20 symbol: a peg is a property of the ASSET, and the
// same asset is deployed at many addresses across bridges and chains.
var stableSymbols = map[string]bool{
	"USDC": true, "USDT": true, "DAI": true, "LUSD": true, "USDS": true,
	"BUSD": true, "FRAX": true, "TUSD": true, "USDP": true, "PYUSD": true,
	"USDE": true, "GUSD": true, "ZUSD": true,
}

// priceRelaxRounds bounds the outward relaxation from the stable anchors. Each
// round prices every token one pool-hop further out, so this is the maximum
// path length from a stablecoin to a priced token. Six hops is far beyond any
// real routing depth and the loop exits early on a fixpoint anyway.
const priceRelaxRounds = 6

// pool is one pool's valuation input: identity, token decimals, and the
// balances actually held on-chain right now.
type valuedPool struct {
	id           string
	t0, t1       string  // lower-cased token addresses
	bal0, bal1   float64 // token-denominated (decimals applied)
	held0, held1 bool    // whether the balance read succeeded
}

// valuationBudget bounds one pass. A pass that cannot finish inside it is
// cancelled and says so. Nothing here may run unboundedly: this loop shares a
// process with six other chains' indexers, and a pass that quietly never
// returns looks exactly like a pass that was never started.
const valuationBudget = 60 * time.Second

// runValuation drives the valuation pass on its own ticker until ctx ends. It
// is started by Run alongside the log poller: orthogonal concerns, one store.
func (idx *Indexer) runValuation(ctx context.Context) {
	ticker := time.NewTicker(valuationInterval)
	defer ticker.Stop()
	// Value once at start so a restart does not serve stale aggregates for a
	// full interval.
	idx.revalue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			idx.revalue(ctx)
		}
	}
}

// logf prefixes a message with the indexer's label so a multi-chain process can
// tell whose pass emitted it. An unlabeled indexer (a test, a single-chain
// binary) prints the bare message.
func (idx *Indexer) logf(format string, args ...interface{}) {
	if idx.label == "" {
		log.Printf(format, args...)
		return
	}
	log.Printf("["+idx.label+"] "+format, args...)
}

// revalue recomputes every derived USD aggregate from on-chain balances and
// writes them back. Read-modify-write throughout: this pass owns the USD and
// price fields and must preserve everything the log handlers own.
//
// Every exit is logged. A pass that reports nothing is indistinguishable from
// one that was never scheduled, and that ambiguity costs more to debug than the
// log lines cost to emit.
func (idx *Indexer) revalue(parent context.Context) {
	defer func() {
		if r := recover(); r != nil {
			idx.logf("[valuation] recovered: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(parent, valuationBudget)
	defer cancel()
	started := time.Now()

	pools := idx.store.PoolsRaw()
	if len(pools) == 0 {
		return // nothing indexed yet for this subgraph; silence is correct here
	}
	tokens := idx.store.TokensRaw()

	vps := idx.readBalances(ctx, pools, tokens)
	if len(vps) == 0 {
		idx.logf("[valuation] %d pools but no balances read — skipping (aggregates left as-is)", len(pools))
		return
	}
	prices := priceTokens(vps, tokens)

	// ── The USD anchor, published in the shape clients read ────────────
	//
	// This pass prices every token in USD directly, but the subgraph wire
	// format has no such field: a client reads a price as the PRODUCT
	// `token.derivedETH * bundle.ethPriceUSD`. Both were left unwritten, so
	// every token served an empty derivedETH against a null bundle and the
	// whole site rendered TVL and volume beside a blank price and a blank
	// market cap.
	//
	// "ETH" is the wire format's name for the chain's own coin — here LUX. So
	// the bundle carries the native coin's USD price and each token its price
	// in native units, which multiply back to exactly the USD price computed
	// above. The wrapped native token quotes 1 by construction.
	//
	// Unpriced stays unpriced: no anchor means no bundle and no derivedETH,
	// never a zero standing in for a number nobody knows.
	nativeUSD := idx.publishAnchor(prices)

	// ── Pools: TVL, spot prices, volume ────────────────────────────────
	//
	// One read of the swap window, valued once, folded twice: into the running
	// totals below, and into the day series (series.go). Two questions about the
	// same trades, one answer about what each trade was worth.
	trades := idx.valuedTrades(vps, tokens, prices)
	tokenTVL := map[string]float64{}
	tokenBal := map[string]float64{}
	tokenVol := map[string]float64{}
	poolTVL := map[string]float64{}
	ratio := map[string]float64{}
	swapUSD := map[string]string{}
	poolVol := valueSwaps(trades, vps, tokenVol, swapUSD)
	valuedRows, valuedErr := idx.store.ValueSwaps(swapUSD)

	var totalTVL, totalVol float64
	for _, vp := range vps {
		p := pools[vp.id]
		if p == nil {
			continue
		}
		v0, ok0 := usdLeg(vp.bal0, vp.t0, prices)
		v1, ok1 := usdLeg(vp.bal1, vp.t1, prices)
		tvl := 0.0
		switch {
		case ok0 && ok1:
			tvl = v0 + v1
		case ok0 && vp.bal1 > 0:
			// Constant-product: at the marginal price both legs are worth the
			// same, so a single priced leg values the whole pool. (This is the
			// same rule the Uniswap subgraphs use for a whitelisted-token pair.)
			tvl = 2 * v0
		case ok1 && vp.bal0 > 0:
			tvl = 2 * v1
		case ok0:
			tvl = v0 // one-sided pool: no mirror leg to double
		case ok1:
			tvl = v1
		}
		if ok0 {
			tokenTVL[vp.t0] += v0
		}
		if ok1 {
			tokenTVL[vp.t1] += v1
		}
		tokenBal[vp.t0] += vp.bal0
		tokenBal[vp.t1] += vp.bal1
		poolTVL[vp.id] = tvl
		if vp.bal1 > 0 {
			ratio[vp.id] = vp.bal0 / vp.bal1
		}

		p.TotalValueLockedUSD = fmtUSD(tvl)
		p.Token0Price = fmtRatio(vp.bal0, vp.bal1)
		p.Token1Price = fmtRatio(vp.bal1, vp.bal0)
		if v, ok := poolVol[vp.id]; ok {
			p.VolumeUSD = fmtUSD(v)
			totalVol += v
		} else if p.VolumeUSD == "" {
			p.VolumeUSD = fmtUSD(0)
		}
		idx.store.SeedPool(vp.id, p)
		totalTVL += tvl
	}

	// ── Tokens: TVL and volume across every pool holding them ──────────
	for addr, t := range tokens {
		low := strings.ToLower(addr)
		tvl, hasTVL := tokenTVL[low]
		vol, hasVol := tokenVol[low]
		price, hasPrice := prices[low]
		if !hasTVL && !hasVol && !hasPrice {
			continue
		}
		t.TotalValueLockedUSD = fmtUSD(tvl)
		t.VolumeUSD = fmtUSD(vol)
		t.DerivedETH = derivedNative(price, nativeUSD, hasPrice)
		idx.store.SeedToken(addr, t)
	}

	// After the token rows, not before them.
	//
	// This pass reads every token into memory once at the top and writes those
	// copies back here. Run earlier, the supply landed in the store and was then
	// overwritten by a copy read before it — so the chain's own coin reported
	// whatever its wrapper's ERC20 says it has wrapped, and Zoo's two trillion
	// showed as the fifteen billion sitting in the contract. The write has to be
	// the last word on the row, because it is the only one that knows what was
	// minted.
	idx.publishNativeSupply(ctx, tokens)

	// ── Factory: the landing-page header aggregate ─────────────────────
	// poolCount is DERIVED here (the true number of pools in the store), not
	// accumulated by the create handlers — an accumulator drifts on every
	// restart, re-index and duplicate create, and there is no reason to guess a
	// number the store can be asked for.
	//
	// txCount counts every interaction — mints and burns as well as trades — and
	// is carried through from the handlers that see them. Volume is a different
	// question and gets a different answer: what the whole table has traded.
	//
	// A pass values a bounded slice of that table, newest first, because it grows
	// forever and rescanning it would cost more every day the chain lives. The
	// bound is right for the pass and wrong for a figure the wire calls all-time.
	// Summing the day series does not escape it either — those rows are folded
	// from the same window, so their sum IS the window. Summing a column costs
	// one scan and answers for every trade ever indexed.
	f, _ := idx.store.GetFactory(nil, "1")
	var txCount int64
	if m, ok := f.(map[string]interface{}); ok {
		txCount = asInt64(m["txCount"])
	}
	traded, volume, err := idx.store.Traded()
	if err != nil {
		idx.logf("[valuation] all-time volume unavailable, reporting this window: %v", err)
		traded, volume = int64(len(trades)), totalVol
	}
	idx.store.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount:           int64(len(pools)),
		TxCount:             txCount,
		TotalValueLockedUSD: fmtUSD(totalTVL),
		TotalVolumeUSD:      fmtUSD(volume),
	})

	// ── The day series: the same trades, grouped by the day they happened ──
	idx.writeSeries(snapshot{
		now:      time.Now().Unix(),
		trades:   trades,
		prices:   prices,
		ratio:    ratio,
		poolTVL:  poolTVL,
		tokenTVL: tokenTVL,
		tokenBal: tokenBal,
		pools:    pools,
		tokens:   tokens,
	})

	// Say what the swap write actually did. A pass that reports its pools and
	// then writes nothing to the trades underneath them is how a pool came to
	// claim thousands in volume over a list of swaps each worth zero.
	priced := fmt.Sprintf(", %d/%d swaps priced", valuedRows, len(swapUSD))
	if valuedErr != nil {
		priced = fmt.Sprintf(", SWAP PRICES NOT WRITTEN: %v", valuedErr)
	}
	// Both volumes, because they answer different questions and a reader who
	// sees one alone cannot tell which he has. The first is what this pass just
	// valued; the second is what the chain has traded and what is served.
	idx.logf("[valuation] %d/%d pools valued, %d tokens priced, %d swaps — TVL $%s, volume $%s of $%s over %d trades%s (%s)",
		len(vps), len(pools), len(prices), len(trades), fmtUSD(totalTVL), fmtUSD(totalVol),
		fmtUSD(volume), traded, priced, time.Since(started).Round(time.Millisecond))
}

// readBalances asks each pool's two tokens what the pool holds. One eth_call
// per leg, batched into a single JSON-RPC request so a 35-pool book costs one
// round trip rather than seventy.
func (idx *Indexer) readBalances(ctx context.Context, pools map[string]*storage.SeedPoolData, tokens map[string]*storage.SeedTokenData) []valuedPool {
	type leg struct {
		pool  int  // index into vps
		isT0  bool //
		token string
	}
	var vps []valuedPool
	var legs []leg
	var calls []rpcBatchReq

	for id, p := range pools {
		if !isAddress(id) || len(vps) >= maxValuedPools {
			continue
		}
		t0, t1 := strings.ToLower(p.Token0), strings.ToLower(p.Token1)
		vps = append(vps, valuedPool{id: id, t0: t0, t1: t1})
		n := len(vps) - 1
		for _, l := range []leg{{n, true, t0}, {n, false, t1}} {
			if !isAddress(l.token) {
				continue
			}
			legs = append(legs, l)
			calls = append(calls, rpcBatchReq{
				JSONRPC: "2.0", ID: len(calls) + 1, Method: "eth_call",
				Params: []interface{}{
					map[string]string{"to": l.token, "data": selBalanceOf + padAddress(id)},
					"latest",
				},
			})
		}
	}
	if len(calls) == 0 {
		return nil
	}

	results, err := idx.rpcBatch(ctx, calls)
	if err != nil {
		log.Printf("[valuation] balance batch: %v", err)
		return nil
	}
	for i, l := range legs {
		raw, ok := results[i+1]
		if !ok {
			continue
		}
		var word string
		if json.Unmarshal(raw, &word) != nil {
			continue
		}
		dec := decimalsOf(l.token, tokens)
		bal, ok := decodeBalance(word, dec)
		if !ok {
			continue
		}
		if l.isT0 {
			vps[l.pool].bal0, vps[l.pool].held0 = bal, true
		} else {
			vps[l.pool].bal1, vps[l.pool].held1 = bal, true
		}
	}
	return vps
}

// priceTokens relaxes USD prices outward from the stablecoin anchors through
// the pool graph. Returns lower-cased token address → USD price.
func priceTokens(vps []valuedPool, tokens map[string]*storage.SeedTokenData) map[string]float64 {
	prices := map[string]float64{}
	for addr, t := range tokens {
		if stableSymbols[strings.ToUpper(strings.TrimSpace(t.Symbol))] {
			prices[strings.ToLower(addr)] = 1
		}
	}
	if len(prices) == 0 {
		return prices
	}

	type quote struct{ price, depth float64 }
	for round := 0; round < priceRelaxRounds; round++ {
		best := map[string]quote{}
		for _, vp := range vps {
			// A price is a RATIO of two live balances; a pool with an empty leg
			// quotes nothing.
			if vp.bal0 <= 0 || vp.bal1 <= 0 {
				continue
			}
			p0, ok0 := prices[vp.t0]
			p1, ok1 := prices[vp.t1]
			switch {
			case ok0 && !ok1:
				d := vp.bal0 * p0
				if q, seen := best[vp.t1]; !seen || d > q.depth {
					best[vp.t1] = quote{d / vp.bal1, d}
				}
			case ok1 && !ok0:
				d := vp.bal1 * p1
				if q, seen := best[vp.t0]; !seen || d > q.depth {
					best[vp.t0] = quote{d / vp.bal0, d}
				}
			}
		}
		if len(best) == 0 {
			break // fixpoint: nothing new is reachable
		}
		for addr, q := range best {
			if q.price > 0 && !math.IsInf(q.price, 0) && !math.IsNaN(q.price) {
				prices[addr] = q.price
			}
		}
	}
	return prices
}

// valueSwaps totals the valued trades into per-pool volume, accumulating
// per-token volume into tokenVol and each trade's own worth into swapUSD.
//
// It folds a stream valuedTrades already built, which the day series folds a
// second way. Two questions about the same trades, one answer about what each
// trade was worth — so the trades a pool's volume is made of carry the very
// figures that add up to it, and a swap list can never disagree with the total
// printed above it.
//
// The per-trade figures are collected here and written in one batch by the
// caller. An earlier cut sent them back one INSERT OR REPLACE at a time; every
// row committed on its own, the interval was spent waiting on the disk, and on
// a chain with a real trade history the pass never finished — so that chain
// silently produced no aggregates at all while a quiet chain beside it looked
// fine. The cost was the trip, not the writing.
func valueSwaps(trades []trade, vps []valuedPool, tokenVol map[string]float64, swapUSD map[string]string) map[string]float64 {
	out := map[string]float64{}
	for _, vp := range vps {
		out[vp.id] = 0
	}
	for i := range trades {
		t := &trades[i]
		usd, priced := t.usd()
		if !priced {
			continue
		}
		out[t.pool] += usd
		swapUSD[t.id] = fmtUSD(usd)
		if t.ok0 {
			tokenVol[t.t0] += t.usd0
		}
		if t.ok1 {
			tokenVol[t.t1] += t.usd1
		}
	}
	return out
}

// twoPow255 is the sign boundary of a 256-bit two's-complement word.
var twoPow255 = new(big.Int).Lsh(big.NewInt(1), 255)

// twoPow256 is 2^256, subtracted to recover the negative value of a word whose
// high bit is set.
var twoPow256 = new(big.Int).Lsh(big.NewInt(1), 256)

// swapLeg decodes one raw swap leg into the absolute amount of the token that
// changed hands and, when that token has a price, what it was worth. The amount
// is a fact of the trade and is always returned; priced says whether the USD
// figure means anything.
//
// A stored leg at or above 2^255 is the two's-complement encoding of a NEGATIVE
// int256 that was persisted through an unsigned decode (the V3 handler's old
// bug — a V3 Swap emits int256 amounts, and one leg of every swap is negative).
// Reinterpreting it here is exact, not a heuristic: the V2 handler stores a
// difference of two uint112 reserves, which can never reach that magnitude, so
// the boundary is unambiguous and no re-index is needed to heal rows the old
// build wrote.
func swapLeg(raw, token string, tokens map[string]*storage.SeedTokenData, prices map[string]float64) (amt, usd float64, priced bool) {
	if raw == "" {
		return 0, 0, false
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return 0, 0, false
	}
	if n.Cmp(twoPow255) >= 0 {
		n.Sub(n, twoPow256)
	}
	amt = normalize(n.Abs(n), decimalsOf(token, tokens))
	price, ok := prices[token]
	if !ok {
		return amt, 0, false
	}
	return amt, amt * price, true
}

// usdLeg values one pool leg, reporting whether the token has a price at all.
func usdLeg(bal float64, token string, prices map[string]float64) (float64, bool) {
	p, ok := prices[token]
	if !ok {
		return 0, false
	}
	return bal * p, true
}

// --- decoding helpers -------------------------------------------------

// rpcBatchReq is one entry of a JSON-RPC 2.0 batch.
type rpcBatchReq struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// rpcBatch posts a JSON-RPC batch and returns id → raw result. Entries that
// errored are absent from the map; the caller treats a missing id as "not
// read" rather than as zero. The result stays raw because the two callers ask
// for different shapes — a balance is a hex word, a header is an object.
func (idx *Indexer) rpcBatch(ctx context.Context, reqs []rpcBatchReq) (map[int]json.RawMessage, error) {
	body, err := json.Marshal(reqs)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", idx.rpc, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := idx.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out []struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("rpc batch decode: %w", err)
	}
	res := make(map[int]json.RawMessage, len(out))
	for _, r := range out {
		if r.Error == nil && len(r.Result) > 0 && string(r.Result) != "null" {
			res[r.ID] = r.Result
		}
	}
	return res, nil
}

// decodeBalance turns a 32-byte uint256 hex word into a token-denominated
// float. Reports false for a short/absent word so an unread balance is never
// mistaken for a zero balance.
func decodeBalance(raw string, decimals int64) (float64, bool) {
	h := strings.TrimPrefix(raw, "0x")
	if len(h) < 64 {
		return 0, false
	}
	n, ok := new(big.Int).SetString(h[len(h)-64:], 16)
	if !ok {
		return 0, false
	}
	return normalize(n, decimals), true
}

// normalize scales a raw integer amount by the token's decimals.
func normalize(n *big.Int, decimals int64) float64 {
	if decimals < 0 || decimals > 36 {
		decimals = defaultDecimals
	}
	f := new(big.Float).SetInt(n)
	f.Quo(f, new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(decimals), nil)))
	v, _ := f.Float64()
	return v
}

// decimalsOf returns a token's ERC20 decimals from the store, defaulting to 18.
func decimalsOf(token string, tokens map[string]*storage.SeedTokenData) int64 {
	if t, ok := tokens[token]; ok && t.Decimals > 0 {
		return t.Decimals
	}
	if t, ok := tokens[strings.ToLower(token)]; ok && t.Decimals > 0 {
		return t.Decimals
	}
	return defaultDecimals
}

// isAddress reports whether s is a 0x-prefixed 20-byte hex address.
func isAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	for _, c := range s[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// padAddress left-pads an address to a 32-byte ABI word.
func padAddress(addr string) string {
	return strings.Repeat("0", 24) + strings.ToLower(strings.TrimPrefix(addr, "0x"))
}

// fmtUSD renders a USD aggregate with two decimals — a money value, always
// definite. A pool the pass could not price reports "0.00", never "".
func fmtUSD(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0.00"
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// publishAnchor writes the native coin's USD price — the anchor every quoted
// price is read against — and returns it. Zero when no pool connects the native
// coin to a stable: then nothing is quotable and nothing is published, rather
// than a bundle of zero that would render every token free.
func (idx *Indexer) publishAnchor(prices map[string]float64) float64 {
	usd := prices[idx.native]
	if usd <= 0 {
		return 0
	}
	idx.store.SeedBundle("1", &storage.SeedBundleData{EthPriceUSD: fmtPrice(usd)})
	return usd
}

// derivedNative converts a USD price into the wire's native-denominated one, so
// that derivedETH * bundle.ethPriceUSD is exactly the USD price again. Empty
// when either side is unknown — an absent quote, not a zero one.
func derivedNative(priceUSD, nativeUSD float64, priced bool) string {
	if !priced || nativeUSD <= 0 {
		return ""
	}
	return fmtPrice(priceUSD / nativeUSD)
}

// fmtPrice renders a price at full significance. fmtUSD's two decimals are a
// CURRENCY format and wrong for a price: LUX at $0.00041334 renders as "0.00"
// there, which reads as free rather than as small.
func fmtPrice(v float64) string {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	return strconv.FormatFloat(v, 'g', 12, 64)
}

// fmtRatio renders a spot price as num/den, matching the Uniswap convention
// that Pair.token0Price is reserve0/reserve1. An undefined ratio renders "0".
func fmtRatio(num, den float64) string {
	if den <= 0 || math.IsNaN(num) || math.IsNaN(den) {
		return "0"
	}
	return strconv.FormatFloat(num/den, 'g', 12, 64)
}
