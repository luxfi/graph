package indexer

import (
	"math"
	"strconv"
	"testing"

	"github.com/luxfi/graph/storage"
)

// A price this pass computes must reach the WIRE, not just the arithmetic.
//
// The valuation pass priced every token in USD and published none of it: the
// subgraph format carries a price only as the product
// `token.derivedETH * bundle.ethPriceUSD`, and both were left unwritten. Live
// mainnet served TVL and volume beside an empty derivedETH and a null bundle,
// so lux.exchange and the explorer rendered blank prices and no market cap.
//
// These assert the published product, not the internal float — the internal
// number was always right.

const (
	wlux = "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e" // the native unit
	lusd = "0x848cff46eb323f323b6bbe1df274e40793d7f2c2" // stable anchor, $1
	lzoo = "0x5e5290f350352768bd2bfc59c2da15dd04a7cb88"
)

// priced builds an indexer holding the three tokens the relaxation walks:
// a stable anchor, the native unit, and one token priced a hop out.
func priced(t *testing.T) *Indexer {
	t.Helper()
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	idx := NewWithConfig(Config{RPC: "http://127.0.0.1:0", Native: wlux}, store)
	idx.store.SeedToken(wlux, &storage.SeedTokenData{Symbol: "WLUX", Decimals: 18})
	idx.store.SeedToken(lusd, &storage.SeedTokenData{Symbol: "LUSD", Decimals: 18})
	idx.store.SeedToken(lzoo, &storage.SeedTokenData{Symbol: "LZOO", Decimals: 18})
	return idx
}

func wireDerivedETH(t *testing.T, idx *Indexer, addr string) string {
	t.Helper()
	tk, ok := idx.store.TokensRaw()[addr]
	if !ok {
		t.Fatalf("token %s absent", addr)
	}
	return tk.DerivedETH
}

// The whole point: derivedETH * ethPriceUSD is the USD price a client shows.
func TestPricesReachTheWire(t *testing.T) {
	const luxUSD, zooUSD = 0.00041334, 0.0000190053

	idx := priced(t)
	vps := []valuedPool{
		// 1 LUSD ($1) per 1/luxUSD WLUX fixes WLUX at luxUSD.
		{id: "p0", t0: lusd, t1: wlux, bal0: 1000, bal1: 1000 / luxUSD, held0: true, held1: true},
		// LZOO priced one hop out, through WLUX.
		{id: "p1", t0: wlux, t1: lzoo, bal0: 5000, bal1: 5000 * (luxUSD / zooUSD), held0: true, held1: true},
	}
	prices := priceTokens(vps, idx.store.TokensRaw())

	// The PRODUCTION writers, not a copy of them: a test that recomputes the
	// projection itself passes just as happily when the pass publishes nothing.
	nativeUSD := idx.publishAnchor(prices)
	if math.Abs(nativeUSD-luxUSD)/luxUSD > 1e-9 {
		t.Fatalf("native price = %g, want %g", nativeUSD, luxUSD)
	}
	for addr, tk := range idx.store.TokensRaw() {
		p, ok := prices[addr]
		tk.DerivedETH = derivedNative(p, nativeUSD, ok)
		idx.store.SeedToken(addr, tk)
	}

	b, err := idx.store.GetBundle(nil, "1")
	if err != nil {
		t.Fatalf("GetBundle: %v", err)
	}
	m, ok := b.(map[string]interface{})
	if !ok {
		t.Fatal("bundle absent — a client has no USD anchor and every price renders blank")
	}
	eth, err := strconv.ParseFloat(m["ethPriceUSD"].(string), 64)
	if err != nil {
		t.Fatalf("ethPriceUSD unparseable: %v", err)
	}

	// The wrapped native quotes exactly 1 of itself.
	if got := wireDerivedETH(t, idx, wlux); got != "1" {
		t.Errorf("WLUX derivedETH = %q, want \"1\"", got)
	}

	for _, c := range []struct {
		addr string
		want float64
	}{{wlux, luxUSD}, {lzoo, zooUSD}} {
		d, err := strconv.ParseFloat(wireDerivedETH(t, idx, c.addr), 64)
		if err != nil {
			t.Fatalf("%s derivedETH unparseable: %v", c.addr, err)
		}
		if got := d * eth; math.Abs(got-c.want)/c.want > 1e-6 {
			t.Errorf("%s: derivedETH*ethPriceUSD = %g, want %g", c.addr, got, c.want)
		}
	}
}

// fmtUSD is a currency format; using it for a price rounds every sub-cent token
// to "0.00", which reads as free rather than as small.
func TestSubCentPriceSurvivesFormatting(t *testing.T) {
	if got := fmtUSD(0.00041334); got != "0.00" {
		t.Fatalf("fmtUSD(0.00041334) = %q — this test's premise is stale", got)
	}
	v, err := strconv.ParseFloat(fmtPrice(0.00041334), 64)
	if err != nil {
		t.Fatalf("fmtPrice unparseable: %v", err)
	}
	if math.Abs(v-0.00041334)/0.00041334 > 1e-9 {
		t.Errorf("fmtPrice(0.00041334) = %g, want 0.00041334", v)
	}
}

// No anchor means no price. A zero here would be a fabricated quote.
func TestUnpricedStaysEmpty(t *testing.T) {
	if got := fmtPrice(0); got != "" {
		t.Errorf("fmtPrice(0) = %q, want empty", got)
	}
	if got := fmtPrice(math.NaN()); got != "" {
		t.Errorf("fmtPrice(NaN) = %q, want empty", got)
	}
}
