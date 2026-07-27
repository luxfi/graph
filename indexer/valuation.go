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

// selBalanceOf is `balanceOf(address)`.
const selBalanceOf = "0x70a08231"

// stableSymbols are the USD-pegged symbols that anchor the price graph at $1.
// Keyed by upper-cased ERC20 symbol: a peg is a property of the ASSET, and the
// same asset is deployed at many addresses across bridges and chains.
var stableSymbols = map[string]bool{
	"USDC": true, "USDT": true, "DAI": true, "LUSD": true, "USDS": true,
	"BUSD": true, "FRAX": true, "TUSD": true, "USDP": true, "PYUSD": true,
	"USDE": true, "GUSD": true,
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

// revalue recomputes every derived USD aggregate from on-chain balances and
// writes them back. Read-modify-write throughout: this pass owns the USD and
// price fields and must preserve everything the log handlers own.
func (idx *Indexer) revalue(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[valuation] recovered: %v", r)
		}
	}()

	pools := idx.store.PoolsRaw()
	if len(pools) == 0 {
		return
	}
	tokens := idx.store.TokensRaw()

	vps := idx.readBalances(ctx, pools, tokens)
	if len(vps) == 0 {
		return
	}
	prices := priceTokens(vps, tokens)

	// ── Pools: TVL, spot prices, volume ────────────────────────────────
	tokenTVL := map[string]float64{}
	tokenVol := map[string]float64{}
	poolVol := idx.valueSwaps(vps, tokens, prices, tokenVol)

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
		tvl, hasTVL := tokenTVL[strings.ToLower(addr)]
		vol, hasVol := tokenVol[strings.ToLower(addr)]
		if !hasTVL && !hasVol {
			continue
		}
		t.TotalValueLockedUSD = fmtUSD(tvl)
		t.VolumeUSD = fmtUSD(vol)
		idx.store.SeedToken(addr, t)
	}

	// ── Factory: the landing-page header aggregate ─────────────────────
	// poolCount is DERIVED here (the true number of pools in the store), not
	// accumulated by the create handlers — an accumulator drifts on every
	// restart, re-index and duplicate create, and there is no reason to guess a
	// number the store can be asked for.
	f, _ := idx.store.GetFactory(nil, "1")
	var txCount int64
	if m, ok := f.(map[string]interface{}); ok {
		txCount = asInt64(m["txCount"])
	}
	idx.store.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount:           int64(len(pools)),
		TxCount:             txCount,
		TotalValueLockedUSD: fmtUSD(totalTVL),
		TotalVolumeUSD:      fmtUSD(totalVol),
	})

	log.Printf("[valuation] %d pools, %d tokens priced — TVL $%s, volume $%s",
		len(vps), len(prices), fmtUSD(totalTVL), fmtUSD(totalVol))
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
		dec := decimalsOf(l.token, tokens)
		bal, ok := decodeBalance(raw, dec)
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

// valueSwaps prices the stored swap history and returns per-pool volume,
// accumulating per-token volume into tokenVol. A swap whose USD value changed
// is written back so the trade history carries the same number the aggregates
// were built from — one valuation, used everywhere.
func (idx *Indexer) valueSwaps(vps []valuedPool, tokens map[string]*storage.SeedTokenData, prices map[string]float64, tokenVol map[string]float64) map[string]float64 {
	byPool := map[string]*valuedPool{}
	for i := range vps {
		byPool[vps[i].id] = &vps[i]
	}
	out := map[string]float64{}
	for _, vp := range vps {
		out[vp.id] = 0
	}
	for id, sw := range idx.store.SwapsRaw() {
		vp := byPool[strings.ToLower(sw.Pool)]
		if vp == nil {
			continue
		}
		v0, ok0 := swapLegUSD(sw.Amount0, vp.t0, tokens, prices)
		v1, ok1 := swapLegUSD(sw.Amount1, vp.t1, tokens, prices)
		var usd float64
		switch {
		case ok0 && ok1:
			usd = (v0 + v1) / 2 // both legs priced: the mid of the two quotes
		case ok0:
			usd = v0
		case ok1:
			usd = v1
		default:
			continue
		}
		out[vp.id] += usd
		if ok0 {
			tokenVol[vp.t0] += v0
		}
		if ok1 {
			tokenVol[vp.t1] += v1
		}
		if s := fmtUSD(usd); s != sw.AmountUSD {
			sw.AmountUSD = s
			idx.store.SeedSwap(id, sw)
		}
	}
	return out
}

// swapLegUSD converts one signed raw swap leg to its absolute USD value.
func swapLegUSD(raw, token string, tokens map[string]*storage.SeedTokenData, prices map[string]float64) (float64, bool) {
	price, ok := prices[token]
	if !ok || raw == "" {
		return 0, false
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return 0, false
	}
	amt := normalize(new(big.Int).Abs(n), decimalsOf(token, tokens))
	return amt * price, true
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

// rpcBatch posts a JSON-RPC batch and returns id → result. Entries that
// errored are absent from the map; the caller treats a missing id as "not
// read" rather than as zero.
func (idx *Indexer) rpcBatch(ctx context.Context, reqs []rpcBatchReq) (map[int]string, error) {
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
		ID     int    `json:"id"`
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("rpc batch decode: %w", err)
	}
	res := make(map[int]string, len(out))
	for _, r := range out {
		if r.Error == nil && r.Result != "" {
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

// fmtRatio renders a spot price as num/den, matching the Uniswap convention
// that Pair.token0Price is reserve0/reserve1. An undefined ratio renders "0".
func fmtRatio(num, den float64) string {
	if den <= 0 || math.IsNaN(num) || math.IsNaN(den) {
		return "0"
	}
	return strconv.FormatFloat(num/den, 'g', 12, 64)
}
