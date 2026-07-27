package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/luxfi/graph/storage"
)

// balanceRPC stands in for the chain: it answers `balanceOf(pool)` from a
// table keyed "token@pool" and rejects anything else, so a test can only pass
// by asking the questions the valuation pass is supposed to ask.
func balanceRPC(t *testing.T, balances map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var reqs []struct {
			ID     int           `json:"id"`
			Method string        `json:"method"`
			Params []interface{} `json:"params"`
		}
		if err := json.Unmarshal(body, &reqs); err != nil {
			t.Errorf("valuation must send a JSON-RPC BATCH, got: %s", body)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		out := make([]map[string]interface{}, 0, len(reqs))
		for _, q := range reqs {
			if q.Method != "eth_call" {
				t.Errorf("unexpected method %q", q.Method)
				continue
			}
			call := q.Params[0].(map[string]interface{})
			to := strings.ToLower(call["to"].(string))
			data := call["data"].(string)
			if !strings.HasPrefix(data, selBalanceOf) {
				t.Errorf("expected balanceOf selector, got %q", data)
			}
			arg := strings.TrimPrefix(data, selBalanceOf)
			pool := "0x" + arg[len(arg)-40:]
			res, ok := balances[to+"@"+pool]
			if !ok {
				res = "0x" + strings.Repeat("0", 64)
			}
			out = append(out, map[string]interface{}{"jsonrpc": "2.0", "id": q.ID, "result": res})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRevalue_DerivesTVLPricesAndPoolCount is the end-to-end proof of the
// valuation pass on a book shaped like the live Lux mainnet one: a stablecoin
// pair to anchor the price graph, and a WLUX pair that must take its price from
// that anchor rather than from thin air.
func TestRevalue_DerivesTVLPricesAndPoolCount(t *testing.T) {
	const (
		usdc     = "0x0000000000000000000000000000000000000001"
		usdt     = "0x0000000000000000000000000000000000000002"
		wlux     = "0x0000000000000000000000000000000000000003"
		mystery  = "0x0000000000000000000000000000000000000004"
		stablePr = "0x00000000000000000000000000000000000000a1" // USDC/USDT
		luxPair  = "0x00000000000000000000000000000000000000a2" // WLUX/USDT
		orphan   = "0x00000000000000000000000000000000000000a3" // MYST/MYST2, unreachable
	)
	s := newMemSQLiteStore(t)

	for addr, sym := range map[string]string{usdc: "USDC", usdt: "USDT", wlux: "WLUX", mystery: "MYST"} {
		s.SeedToken(addr, &storage.SeedTokenData{Symbol: sym, Name: sym, Decimals: 18})
	}
	s.SeedPool(stablePr, &storage.SeedPoolData{Token0: usdc, Token1: usdt, FeeTier: 3000, TxCount: 7})
	s.SeedPool(luxPair, &storage.SeedPoolData{Token0: wlux, Token1: usdt, FeeTier: 500})
	s.SeedPool(orphan, &storage.SeedPoolData{Token0: mystery, Token1: mystery, FeeTier: 3000})

	// 100 USDC + 100 USDT; 4000 WLUX + 100 USDT  =>  WLUX = $0.025.
	srv := balanceRPC(t, map[string]string{
		usdc + "@" + stablePr: hexWord(100),
		usdt + "@" + stablePr: hexWord(100),
		wlux + "@" + luxPair:  hexWord(4000),
		usdt + "@" + luxPair:  hexWord(100),
	})

	idx := NewWithConfig(Config{RPC: srv.URL}, s)
	idx.revalue(context.Background())

	// ── The stable pair is worth the sum of its legs. ──────────────────
	p, _ := s.GetPool(nil, stablePr)
	if p == nil {
		t.Fatal("stable pair vanished")
	}
	pm := p.(map[string]interface{})
	if got := fmt.Sprint(pm["totalValueLockedUSD"]); got != "200.00" {
		t.Errorf("stable pair TVL = %q, want 200.00", got)
	}
	// A field the pass does not own must survive its read-modify-write.
	if asInt64(pm["txCount"]) != 7 {
		t.Errorf("valuation clobbered txCount: got %v want 7", pm["txCount"])
	}

	// ── WLUX is priced THROUGH the anchor, and the pair valued from it. ─
	p, _ = s.GetPool(nil, luxPair)
	pm = p.(map[string]interface{})
	if got := fmt.Sprint(pm["totalValueLockedUSD"]); got != "200.00" {
		t.Errorf("WLUX pair TVL = %q, want 200.00 (4000 WLUX @ $0.025 + 100 USDT)", got)
	}
	if got := mustFloat(t, pm["token0Price"]); math.Abs(got-40) > 1e-9 {
		t.Errorf("token0Price = %v, want 40 (reserve0/reserve1)", got)
	}
	if got := mustFloat(t, pm["token1Price"]); math.Abs(got-0.025) > 1e-12 {
		t.Errorf("token1Price = %v, want 0.025", got)
	}

	// ── A pool no path connects to a stable is reported as $0, not blank. ─
	p, _ = s.GetPool(nil, orphan)
	if got := fmt.Sprint(p.(map[string]interface{})["totalValueLockedUSD"]); got != "0.00" {
		t.Errorf("unpriceable pool TVL = %q, want 0.00", got)
	}

	// ── Token TVL sums every pool holding the token. ───────────────────
	tok, _ := s.GetToken(nil, usdt)
	if got := fmt.Sprint(tok.(map[string]interface{})["totalValueLockedUSD"]); got != "200.00" {
		t.Errorf("USDT TVL = %q, want 200.00 (100 in each pair)", got)
	}
	tok, _ = s.GetToken(nil, wlux)
	if got := fmt.Sprint(tok.(map[string]interface{})["totalValueLockedUSD"]); got != "100.00" {
		t.Errorf("WLUX TVL = %q, want 100.00", got)
	}

	// ── Factory: poolCount is the real count, TVL the real sum. ────────
	f, _ := s.GetFactory(nil, "1")
	fm := f.(map[string]interface{})
	if asInt64(fm["poolCount"]) != 3 {
		t.Errorf("factory poolCount = %v, want 3 (derived from the store)", fm["poolCount"])
	}
	if got := fmt.Sprint(fm["totalValueLockedUSD"]); got != "400.00" {
		t.Errorf("factory TVL = %q, want 400.00", got)
	}
}

// TestRevalue_UnreachableRPCKeepsLastGoodValues proves the pass fails closed:
// an RPC blip must never overwrite good aggregates with zeros.
func TestRevalue_UnreachableRPCKeepsLastGoodValues(t *testing.T) {
	const pool = "0x00000000000000000000000000000000000000b1"
	const tokenA = "0x0000000000000000000000000000000000000011"
	const tokenB = "0x0000000000000000000000000000000000000012"

	s := newMemSQLiteStore(t)
	s.SeedToken(tokenA, &storage.SeedTokenData{Symbol: "USDC", Decimals: 18})
	s.SeedToken(tokenB, &storage.SeedTokenData{Symbol: "WLUX", Decimals: 18})
	s.SeedPool(pool, &storage.SeedPoolData{
		Token0: tokenA, Token1: tokenB, FeeTier: 3000, TotalValueLockedUSD: "999.00",
	})
	s.SeedFactory("1", &storage.SeedFactoryData{PoolCount: 1, TotalValueLockedUSD: "999.00"})

	idx := NewWithConfig(Config{RPC: "http://127.0.0.1:1"}, s)
	idx.revalue(context.Background())

	p, _ := s.GetPool(nil, pool)
	if got := fmt.Sprint(p.(map[string]interface{})["totalValueLockedUSD"]); got != "999.00" {
		t.Errorf("an unreachable RPC must not clobber TVL: got %q want 999.00", got)
	}
	f, _ := s.GetFactory(nil, "1")
	if got := fmt.Sprint(f.(map[string]interface{})["totalValueLockedUSD"]); got != "999.00" {
		t.Errorf("an unreachable RPC must not clobber factory TVL: got %q want 999.00", got)
	}
}

// TestPriceTokens_PrefersTheDeeperQuote proves the depth ordering: when two
// pools disagree on a token's price, the one with more value behind it wins.
func TestPriceTokens_PrefersTheDeeperQuote(t *testing.T) {
	const (
		usdc = "0x0000000000000000000000000000000000000001"
		usdt = "0x0000000000000000000000000000000000000002"
		wlux = "0x0000000000000000000000000000000000000003"
	)
	tokens := map[string]*storage.SeedTokenData{
		usdc: {Symbol: "USDC", Decimals: 18},
		usdt: {Symbol: "USDT", Decimals: 18},
		wlux: {Symbol: "WLUX", Decimals: 18},
	}
	vps := []valuedPool{
		// thin: 100 WLUX vs 1 USDC  => $0.01
		{id: "0xa", t0: wlux, t1: usdc, bal0: 100, bal1: 1},
		// deep: 100 WLUX vs 50 USDT => $0.50
		{id: "0xb", t0: wlux, t1: usdt, bal0: 100, bal1: 50},
	}
	prices := priceTokens(vps, tokens)
	if got := prices[wlux]; math.Abs(got-0.5) > 1e-12 {
		t.Errorf("WLUX price = %v, want 0.5 from the deeper pool (not 0.01 from the thin one)", got)
	}
}

// hexWord encodes a whole-token amount at 18 decimals as a 32-byte hex word.
func hexWord(whole int64) string {
	n := new(big.Int).SetInt64(whole)
	n.Mul(n, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	return "0x" + fmt.Sprintf("%064s", n.Text(16))
}

func mustFloat(t *testing.T, v interface{}) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	if err != nil {
		t.Fatalf("not a number: %v", v)
	}
	return f
}

// TestSwapLegUSD_HealsUnsignedNegativeLeg pins the exact defect that made the
// volume rollup report ~$4e72: a V3 Swap emits int256 amounts and one leg of
// every swap is negative, but the handler decoded them unsigned, persisting
// ~2^256 instead of a small negative number.
func TestSwapLegUSD_HealsUnsignedNegativeLeg(t *testing.T) {
	const token = "0x0000000000000000000000000000000000000001"
	tokens := map[string]*storage.SeedTokenData{token: {Symbol: "USDC", Decimals: 18}}
	prices := map[string]float64{token: 1}

	// -5e18 stored through an unsigned decode: 2^256 - 5e18.
	neg := new(big.Int).Mul(big.NewInt(-5), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	wrapped := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 256), neg)

	got, ok := swapLegUSD(wrapped.String(), token, tokens, prices)
	if !ok {
		t.Fatal("leg must be valued")
	}
	if math.Abs(got-5) > 1e-9 {
		t.Errorf("wrapped negative leg = $%v, want $5 (got the 2^256 reading?)", got)
	}

	// A plainly-signed leg (what the fixed V3 handler and the V2 handler write)
	// must value identically.
	got2, _ := swapLegUSD(neg.String(), token, tokens, prices)
	if math.Abs(got2-5) > 1e-9 {
		t.Errorf("signed leg = $%v, want $5", got2)
	}
}

// TestHandleSwapV3_DecodesSignedAmounts proves the handler now persists the
// negative leg as a negative number rather than its 2^256 complement.
func TestHandleSwapV3_DecodesSignedAmounts(t *testing.T) {
	const (
		token0 = "0x0000000000000000000000000000000000000011"
		token1 = "0x0000000000000000000000000000000000000022"
		pool   = "0x00000000000000000000000000000000000000c3"
	)
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused", FactoryV2: testFactoryV2, FactoryV3: testFactoryV3}, s)
	idx.processLog(context.Background(), poolCreatedLog(testFactoryV3, token0, token1, 3000, pool))
	// amount0 = +1000 in, amount1 = -2500 out.
	idx.processLog(context.Background(), swapV3Log(pool, token0, "0xreal", 1000, -2500))

	raw := s.SwapsRaw()
	if len(raw) != 1 {
		t.Fatalf("expected exactly one swap, got %d", len(raw))
	}
	for _, sw := range raw {
		if sw.Amount1 != "-2500" {
			t.Errorf("amount1 = %q, want \"-2500\" (an unsigned read gives a ~78-digit number)", sw.Amount1)
		}
		if sw.Amount0 != "1000" {
			t.Errorf("amount0 = %q, want \"1000\"", sw.Amount0)
		}
	}
}

// TestRevalue_DoesNotRewriteSwapRows pins the performance defect that wedged a
// whole chain: valuing must be O(pools) writes, never O(swaps) writes.
func TestRevalue_DoesNotRewriteSwapRows(t *testing.T) {
	const (
		usdc = "0x0000000000000000000000000000000000000001"
		wlux = "0x0000000000000000000000000000000000000003"
		pool = "0x00000000000000000000000000000000000000d1"
	)
	s := newMemSQLiteStore(t)
	s.SeedToken(usdc, &storage.SeedTokenData{Symbol: "USDC", Decimals: 18})
	s.SeedToken(wlux, &storage.SeedTokenData{Symbol: "WLUX", Decimals: 18})
	s.SeedPool(pool, &storage.SeedPoolData{Token0: wlux, Token1: usdc, FeeTier: 3000})
	s.SeedSwap("swap-1", &storage.SeedSwapData{
		Timestamp: 1, Pool: pool, Amount0: "1000000000000000000", Amount1: "-25000000000000000000",
		AmountUSD: "0", Sender: "0xabc",
	})

	srv := balanceRPC(t, map[string]string{
		wlux + "@" + pool: hexWord(4000),
		usdc + "@" + pool: hexWord(100),
	})
	idx := NewWithConfig(Config{RPC: srv.URL}, s)
	idx.revalue(context.Background())

	// 1 WLUX @ $0.025 = $0.025; 25 USDC = $25; mid = $12.5125.
	p, _ := s.GetPool(nil, pool)
	if got := fmt.Sprint(p.(map[string]interface{})["volumeUSD"]); got != "12.51" {
		t.Errorf("pool volumeUSD = %q, want 12.51", got)
	}
	// The swap row itself is untouched — valuing must not write O(swaps) rows.
	for _, sw := range s.SwapsRaw() {
		if sw.AmountUSD != "0" {
			t.Errorf("valuation must not rewrite swap rows; amountUSD became %q", sw.AmountUSD)
		}
	}
}
