package indexer

import (
	"context"
	"fmt"
	"testing"

	"github.com/luxfi/graph/storage"
)

// A swap must be COUNTED, not merely stored. The indexer recorded every swap via
// SeedSwap and nothing counted them, so the explorer served 0 for every pool,
// every token and all-time trades — a TXNS column of zeros next to real volume.
//
// It is counted by asking the store rather than by tallying as they arrive. A
// tally drifts on every restart and re-index, which is how the protocol came to
// claim 16,062 trades over pools holding 14,827 of them, and how a token could
// report $74,100 of volume beside a count of zero. The volume and the count are
// read together now, so the two columns cannot disagree.

const (
	testPool   = "0x1c000d5dbe1246fb84ad431e933e5563f212a62b" // LUX/LZOO
	testToken0 = "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e" // WLUX
	testToken1 = "0x5e5290f350352768bd2bfc59c2da15dd04a7cb88" // LZOO
)

func newTestIndexer(t *testing.T) *Indexer {
	t.Helper()
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	// Opening a store does not create its tables. Without this every Seed below
	// writes into nothing — the calls report no error, so the fixture looks
	// built and the assertions fail somewhere far from the cause.
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("storage.Init: %v", err)
	}
	idx := NewWithConfig(Config{RPC: "http://127.0.0.1:0"}, store)
	idx.store.SeedPool(testPool, &storage.SeedPoolData{
		Token0: testToken0, Token1: testToken1, FeeTier: 3000,
	})
	idx.store.SeedToken(testToken0, &storage.SeedTokenData{Symbol: "WLUX", Decimals: 18})
	idx.store.SeedToken(testToken1, &storage.SeedTokenData{Symbol: "LZOO", Decimals: 18})
	return idx
}

func poolTxCount(t *testing.T, idx *Indexer, id string) int64 {
	t.Helper()
	p, ok := idx.store.PoolsRaw()[id]
	if !ok {
		t.Fatalf("pool %s absent", id)
	}
	return p.TxCount
}

func tokenTxCount(t *testing.T, idx *Indexer, addr string) int64 {
	t.Helper()
	tk, ok := idx.store.TokensRaw()[addr]
	if !ok {
		t.Fatalf("token %s absent", addr)
	}
	return tk.TxCount
}

func TestSwapsAreCountedWhereTheirVolumeIs(t *testing.T) {
	idx := newTestIndexer(t)

	byPool, err := idx.store.TradedByPool()
	if err != nil {
		t.Fatalf("TradedByPool: %v", err)
	}
	if got := byPool[testPool].Trades; got != 0 {
		t.Fatalf("pool trades before = %d, want 0", got)
	}

	for i := 0; i < 5; i++ {
		idx.store.SeedSwap(fmt.Sprintf("0xfeed#%d", i), &storage.SeedSwapData{
			Timestamp: int64(1767225600 + i), Block: uint64(10 + i), Pool: testPool, AmountUSD: "10.00",
		})
	}

	byPool, err = idx.store.TradedByPool()
	if err != nil {
		t.Fatalf("TradedByPool: %v", err)
	}
	if got := byPool[testPool]; got.Trades != 5 || got.VolumeUSD != 50 {
		t.Errorf("pool = %+v, want 5 trades worth 50 — the count and the volume are one read", got)
	}
}

// A swap whose pool was never registered is dropped upstream, and counting must
// not invent a pool entity for it — that is the phantom-pool bypass the handlers
// gate. Nothing here writes a pool, so an unknown pool simply has no row to
// stand beside its trades.
func TestCountingInventsNoPool(t *testing.T) {
	idx := newTestIndexer(t)
	before := len(idx.store.PoolsRaw())

	idx.store.SeedSwap("0xghost#0", &storage.SeedSwapData{
		Timestamp: 1767225600, Block: 11, Pool: "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", AmountUSD: "1.00",
	})

	if after := len(idx.store.PoolsRaw()); after != before {
		t.Errorf("pool count = %d, want %d — an unknown pool must not be created", after, before)
	}
}
