package indexer

import (
	"testing"

	"github.com/luxfi/graph/storage"
)

// A swap must be COUNTED, not merely stored. The indexer recorded every swap via
// SeedSwap but nothing incremented txCount, so the explorer served 0 for every
// pool, every token and all-time trades — the table rendered a TXNS column of
// zeros next to real volume.

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

func TestBumpSwapCounts_CountsPoolTokensAndFactory(t *testing.T) {
	idx := newTestIndexer(t)

	if got := poolTxCount(t, idx, testPool); got != 0 {
		t.Fatalf("pool txCount before = %d, want 0", got)
	}

	idx.bumpSwapCounts(testPool)

	if got := poolTxCount(t, idx, testPool); got != 1 {
		t.Errorf("pool txCount = %d, want 1", got)
	}
	if got := tokenTxCount(t, idx, testToken0); got != 1 {
		t.Errorf("token0 txCount = %d, want 1", got)
	}
	if got := tokenTxCount(t, idx, testToken1); got != 1 {
		t.Errorf("token1 txCount = %d, want 1", got)
	}

	f, _ := idx.store.GetFactory(nil, "1")
	m, ok := f.(map[string]interface{})
	if !ok {
		t.Fatalf("factory missing after a counted swap")
	}
	if got := asInt64(m["txCount"]); got != 1 {
		t.Errorf("factory txCount = %d, want 1", got)
	}
}

func TestBumpSwapCounts_Accumulates(t *testing.T) {
	idx := newTestIndexer(t)
	for i := 0; i < 5; i++ {
		idx.bumpSwapCounts(testPool)
	}
	if got := poolTxCount(t, idx, testPool); got != 5 {
		t.Errorf("pool txCount = %d, want 5", got)
	}
	if got := tokenTxCount(t, idx, testToken0); got != 5 {
		t.Errorf("token0 txCount = %d, want 5", got)
	}
}

// A swap whose pool was never registered is dropped upstream; bumping on it must
// not invent a pool entity (that is the phantom-pool bypass the handlers gate).
func TestBumpSwapCounts_UnknownPoolIsNoop(t *testing.T) {
	idx := newTestIndexer(t)
	before := len(idx.store.PoolsRaw())

	idx.bumpSwapCounts("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	if after := len(idx.store.PoolsRaw()); after != before {
		t.Errorf("pool count = %d, want %d — an unknown pool must not be created", after, before)
	}
	if got := poolTxCount(t, idx, testPool); got != 0 {
		t.Errorf("known pool txCount = %d, want 0", got)
	}
}
