package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The cost of valuing has to stay bounded. The incident this guards against was
// a pass that never finished, and the reason was that every row went out as its
// own committed statement. Batched behind one transaction the same rows cost a
// different order of magnitude — this measures it rather than assuming it.
func TestValueSwapsCostIsBounded(t *testing.T) {
	const n = 50000
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	usd := make(map[string]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("s%d", i)
		s.SeedSwap(id, &SeedSwapData{Timestamp: int64(i), Pool: "0xpool", AmountUSD: "0"})
		usd[id] = "1.23"
	}
	start := time.Now()
	s.ValueSwaps(usd)
	took := time.Since(start)
	t.Logf("valued %d swaps in %v (%.1f µs/swap)", n, took, float64(took.Microseconds())/float64(n))
	if took > 10*time.Second {
		t.Errorf("valuing %d swaps took %v — an interval cannot absorb that", n, took)
	}
}
