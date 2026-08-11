package indexer

import (
	"math"
	"strconv"
	"testing"
)

// A pool's volume is the sum of what its trades were worth, so the trades a
// swap list shows must add up to the total printed above them. Reading them
// from different places is how a page ends up claiming $3,616 of volume over a
// list of trades that each say they were worth nothing.
func TestSwapValuesSumToPoolVolume(t *testing.T) {
	const pool = "0xpool"
	trades := []trade{
		{id: "s1", pool: pool, t0: "0xa", t1: "0xb", usd0: 100, usd1: 100, ok0: true, ok1: true},
		{id: "s2", pool: pool, t0: "0xa", t1: "0xb", usd0: 40, usd1: 60, ok0: true, ok1: true},
		{id: "s3", pool: pool, t0: "0xa", t1: "0xb", usd0: 25, ok0: true},
	}
	vps := []valuedPool{{id: pool, t0: "0xa", t1: "0xb"}}

	swapUSD := map[string]string{}
	poolVol := valueSwaps(trades, vps, map[string]float64{}, swapUSD)

	if len(swapUSD) != len(trades) {
		t.Fatalf("valued %d of %d trades; a trade with a price must carry it", len(swapUSD), len(trades))
	}
	var sum float64
	for id, v := range swapUSD {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatalf("swap %s value %q does not parse: %v", id, v, err)
		}
		if f <= 0 {
			t.Errorf("swap %s valued at %s — a priced trade is worth something", id, v)
		}
		sum += f
	}
	if math.Abs(sum-poolVol[pool]) > 0.01 {
		t.Errorf("trades sum to %.2f but the pool reports %.2f", sum, poolVol[pool])
	}
}

// A trade whose tokens have no price is worth an unknown amount, not zero.
// Writing a zero would let an unpriced pool masquerade as a quiet one.
func TestUnpricedTradeCarriesNoValue(t *testing.T) {
	trades := []trade{{id: "s1", pool: "0xpool", t0: "0xa", t1: "0xb"}}
	swapUSD := map[string]string{}
	valueSwaps(trades, []valuedPool{{id: "0xpool"}}, map[string]float64{}, swapUSD)

	if v, ok := swapUSD["s1"]; ok {
		t.Errorf("unpriced trade was valued %q; it should carry nothing", v)
	}
}
