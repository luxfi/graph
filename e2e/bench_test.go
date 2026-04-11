//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/storage"
)

// ---------------------------------------------------------------------------
// Seed helpers — realistic data volumes.
// 100 tokens, 500 pools, 10 000 swaps, 1 000 mints, 1 000 burns.
// ---------------------------------------------------------------------------

// seedBenchStore populates a store with production-scale data.
// Addresses are deterministic hex strings so benchmarks are reproducible.
func seedBenchStore(b *testing.B) *storage.Store {
	b.Helper()

	store, err := storage.New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	if err := store.Init(context.Background()); err != nil {
		b.Fatal(err)
	}

	// Factory + bundle
	store.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount:           500,
		TxCount:             12000,
		TotalVolumeUSD:      "87654321.12",
		TotalValueLockedUSD: "23456789.01",
	})
	store.SeedBundle("1", &storage.SeedBundleData{
		EthPriceUSD: "2.53",
	})

	// 100 tokens
	for i := 0; i < 100; i++ {
		addr := fmt.Sprintf("0x%040x", i+1)
		store.SeedToken(addr, &storage.SeedTokenData{
			Symbol:              fmt.Sprintf("TK%d", i),
			Name:                fmt.Sprintf("Token %d", i),
			Decimals:            18,
			VolumeUSD:           fmt.Sprintf("%d.00", (i+1)*50000),
			TotalValueLockedUSD: fmt.Sprintf("%d.00", (i+1)*30000),
			DerivedETH:          fmt.Sprintf("0.%04d", i+1),
			TxCount:             int64((i + 1) * 10),
		})
	}

	// 500 pools — each references two of the 100 tokens
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("0xpool%06d", i)
		t0 := fmt.Sprintf("0x%040x", (i%100)+1)
		t1 := fmt.Sprintf("0x%040x", ((i+1)%100)+1)
		fee := []int64{500, 3000, 10000}[i%3]
		store.SeedPool(id, &storage.SeedPoolData{
			Token0:              t0,
			Token1:              t1,
			FeeTier:             fee,
			TotalValueLockedUSD: fmt.Sprintf("%d.00", (i+1)*4000),
			VolumeUSD:           fmt.Sprintf("%d.00", (i+1)*1000),
			Token0Price:         "2.53",
			Token1Price:         "0.395",
			TxCount:             int64((i + 1) * 2),
		})
	}

	// 10 000 swaps
	baseTS := int64(1711929600)
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("0xtx%08d#%d", i/4, i%4)
		pool := fmt.Sprintf("0xpool%06d", i%500)
		store.SeedSwap(id, &storage.SeedSwapData{
			Timestamp: baseTS - int64(i),
			Pool:      pool,
			Amount0:   fmt.Sprintf("%d.%02d", i%10000, i%100),
			Amount1:   fmt.Sprintf("-%d.%02d", (i*2)%10000, (i*3)%100),
			AmountUSD: fmt.Sprintf("%d.%02d", (i+1)*25, (i*7)%100),
			Sender:    fmt.Sprintf("0x%040x", i%200+1000),
		})
	}

	// 1 000 mints — stored as generic entities (mint storage returns empty slices)
	for i := 0; i < 1000; i++ {
		store.SetEntity("Mint", fmt.Sprintf("mint-%d", i), map[string]interface{}{
			"id":          fmt.Sprintf("mint-%d", i),
			"transaction": fmt.Sprintf("0x%064x", i),
			"timestamp":   baseTS - int64(i*3),
			"pool":        fmt.Sprintf("0xpool%06d", i%500),
			"amount0":     fmt.Sprintf("%d.00", i*100),
			"amount1":     fmt.Sprintf("%d.00", i*50),
			"amountUSD":   fmt.Sprintf("%d.00", i*75),
		})
	}

	// 1 000 burns
	for i := 0; i < 1000; i++ {
		store.SetEntity("Burn", fmt.Sprintf("burn-%d", i), map[string]interface{}{
			"id":          fmt.Sprintf("burn-%d", i),
			"transaction": fmt.Sprintf("0x%064x", i+10000),
			"timestamp":   baseTS - int64(i*5),
			"pool":        fmt.Sprintf("0xpool%06d", i%500),
			"amount0":     fmt.Sprintf("%d.00", i*80),
			"amount1":     fmt.Sprintf("%d.00", i*40),
			"amountUSD":   fmt.Sprintf("%d.00", i*60),
		})
	}

	return store
}

// newBenchEngine builds an engine with AMM + all chain schemas loaded.
func newBenchEngine(b *testing.B, store *storage.Store) *engine.Engine {
	b.Helper()
	eng := engine.New(store, nil)
	if err := eng.LoadBuiltin("all"); err != nil {
		b.Fatal(err)
	}
	return eng
}

// runQuery is a helper that executes a query and fails the benchmark on error.
func runQuery(b *testing.B, eng *engine.Engine, query string) {
	b.Helper()
	resp := eng.Execute(context.Background(), &engine.Request{Query: query})
	if len(resp.Errors) > 0 {
		b.Fatalf("query error: %s", resp.Errors[0].Message)
	}
}

// ---------------------------------------------------------------------------
// A) Query latency benchmarks
// ---------------------------------------------------------------------------

func BenchmarkQuery_Factory(b *testing.B) {
	store := seedBenchStore(b)
	eng := newBenchEngine(b, store)
	q := `{ factory(id: "1") { id poolCount txCount totalVolumeUSD totalValueLockedUSD } }`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runQuery(b, eng, q)
	}
}

func BenchmarkQuery_Tokens50(b *testing.B) {
	store := seedBenchStore(b)
	eng := newBenchEngine(b, store)
	q := `{ tokens(first: 50) { id symbol name decimals volumeUSD totalValueLockedUSD derivedETH txCount } }`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runQuery(b, eng, q)
	}
}

func BenchmarkQuery_Pools50(b *testing.B) {
	store := seedBenchStore(b)
	eng := newBenchEngine(b, store)
	q := `{ pools(first: 50) { id token0 { id symbol name decimals } token1 { id symbol name decimals } feeTier totalValueLockedUSD volumeUSD token0Price token1Price txCount } }`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runQuery(b, eng, q)
	}
}

func BenchmarkQuery_Swaps200(b *testing.B) {
	store := seedBenchStore(b)
	eng := newBenchEngine(b, store)
	q := `{ swaps(first: 200, orderBy: timestamp, orderDirection: desc) { id timestamp pool amount0 amount1 amountUSD sender } }`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runQuery(b, eng, q)
	}
}

func BenchmarkQuery_Dashboard(b *testing.B) {
	store := seedBenchStore(b)
	eng := newBenchEngine(b, store)
	q := `{
		bundle(id: "1") { ethPriceUSD }
		factories(first: 1) { id poolCount txCount totalVolumeUSD totalValueLockedUSD }
		tokens(first: 50, orderBy: totalValueLockedUSD, orderDirection: desc) {
			id symbol name decimals totalValueLockedUSD derivedETH
		}
		pools(first: 50, orderBy: totalValueLockedUSD, orderDirection: desc) {
			id token0 { id symbol } token1 { id symbol }
			feeTier totalValueLockedUSD token0Price token1Price
		}
	}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runQuery(b, eng, q)
	}
}

func BenchmarkQuery_AllSchemas(b *testing.B) {
	store := seedBenchStore(b)
	// Seed chain-specific entities
	store.SetEntity("Validator", "NodeID-bench", map[string]interface{}{
		"id": "NodeID-bench", "stake": "1000000", "uptime": "99.9",
	})
	store.SetEntity("Order", "order-bench", map[string]interface{}{
		"id": "order-bench", "market": "LUX/USD", "side": "buy", "price": "2.50", "amount": "100",
	})
	store.SetEntity("DKGCeremony", "dkg-bench", map[string]interface{}{
		"id": "dkg-bench", "participants": 5, "threshold": 3, "status": "complete",
	})
	store.SetEntity("Asset", "LUX-bench", map[string]interface{}{
		"id": "LUX-bench", "symbol": "LUX", "name": "Lux", "denomination": 6,
	})
	store.SetEntity("BridgeTransfer", "bt-bench", map[string]interface{}{
		"id": "bt-bench", "sourceChain": "C", "destChain": "ethereum", "amount": "1000",
	})
	store.SetEntity("ShieldedTransfer", "st-bench", map[string]interface{}{
		"id": "st-bench", "nullifier": "0xabc", "commitment": "0xdef",
	})

	eng := newBenchEngine(b, store)

	// Query every chain type in sequence per iteration.
	queries := []string{
		`{ factories(first: 1) { id } }`,
		`{ tokens(first: 10) { id symbol } }`,
		`{ pools(first: 10) { id } }`,
		`{ swaps(first: 10) { id } }`,
		`{ orders(first: 10) { id } }`,
		`{ validators(first: 10) { id } }`,
		`{ dkgCeremonies(first: 10) { id } }`,
		`{ assets(first: 10) { id } }`,
		`{ bridgeTransfers(first: 10) { id } }`,
		`{ shieldedTransfers(first: 10) { id } }`,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, q := range queries {
			runQuery(b, eng, q)
		}
	}
}

// ---------------------------------------------------------------------------
// B) Throughput benchmarks — concurrent clients
// ---------------------------------------------------------------------------

// benchThroughput fires N goroutines each running queries in a loop.
func benchThroughput(b *testing.B, clients int) {
	b.Helper()

	store := seedBenchStore(b)
	eng := newBenchEngine(b, store)

	// Mix of realistic queries weighted by exchange usage patterns.
	queries := []string{
		`{ bundle(id: "1") { ethPriceUSD } }`,
		`{ factories(first: 1) { poolCount txCount totalVolumeUSD totalValueLockedUSD } }`,
		`{ tokens(first: 50) { id symbol volumeUSD totalValueLockedUSD derivedETH } }`,
		`{ pools(first: 50) { id token0 { id symbol } token1 { id symbol } feeTier totalValueLockedUSD } }`,
		`{ swaps(first: 200, orderBy: timestamp, orderDirection: desc) { id timestamp amountUSD sender } }`,
	}

	var ops atomic.Int64

	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	perClient := b.N / clients
	if perClient < 1 {
		perClient = 1
	}

	for c := 0; c < clients; c++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			for i := 0; i < perClient; i++ {
				q := queries[(clientID+i)%len(queries)]
				resp := eng.Execute(context.Background(), &engine.Request{Query: q})
				if len(resp.Errors) > 0 {
					b.Errorf("client %d query error: %s", clientID, resp.Errors[0].Message)
					return
				}
				ops.Add(1)
			}
		}(c)
	}
	wg.Wait()

	b.ReportMetric(float64(ops.Load())/b.Elapsed().Seconds(), "queries/sec")
}

func BenchmarkThroughput_1Client(b *testing.B) {
	benchThroughput(b, 1)
}

func BenchmarkThroughput_10Clients(b *testing.B) {
	benchThroughput(b, 10)
}

func BenchmarkThroughput_100Clients(b *testing.B) {
	benchThroughput(b, 100)
}

// ---------------------------------------------------------------------------
// C) Indexing simulation — entity writes
// ---------------------------------------------------------------------------

func BenchmarkIndex_SwapEvent(b *testing.B) {
	store, err := storage.New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	store.Init(context.Background())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("0xtx%08d#0", i)
		store.SeedSwap(id, &storage.SeedSwapData{
			Timestamp: 1711929600 + int64(i),
			Pool:      "0xpool000001",
			Amount0:   "123.456",
			Amount1:   "-308.640",
			AmountUSD: "308.64",
			Sender:    "0x000000000000000000000000000000000000abcd",
		})
	}
}

func BenchmarkIndex_BatchSwaps1000(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		store, _ := storage.New(b.TempDir())
		store.Init(context.Background())
		b.StartTimer()

		for j := 0; j < 1000; j++ {
			id := fmt.Sprintf("0xtx%08d#%d", i*1000+j/4, j%4)
			store.SeedSwap(id, &storage.SeedSwapData{
				Timestamp: 1711929600 + int64(j),
				Pool:      fmt.Sprintf("0xpool%06d", j%50),
				Amount0:   fmt.Sprintf("%d.%02d", j%10000, j%100),
				Amount1:   fmt.Sprintf("-%d.%02d", (j*2)%10000, (j*3)%100),
				AmountUSD: fmt.Sprintf("%d.%02d", (j+1)*25, (j*7)%100),
				Sender:    fmt.Sprintf("0x%040x", j%200+1000),
			})
		}
	}
}

// BenchmarkIndex_GenericEntity measures SetEntity for chain-specific resolvers.
func BenchmarkIndex_GenericEntity(b *testing.B) {
	store, err := storage.New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	store.Init(context.Background())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.SetEntity("Swap", fmt.Sprintf("swap-%d", i), map[string]interface{}{
			"id":        fmt.Sprintf("swap-%d", i),
			"timestamp": 1711929600 + i,
			"pool":      "0xpool000001",
			"amount0":   "123.456",
			"amount1":   "-308.640",
			"amountUSD": "308.64",
			"sender":    "0x000000000000000000000000000000000000abcd",
		})
	}
}

// BenchmarkIndex_ConcurrentWrites measures write contention under load.
func BenchmarkIndex_ConcurrentWrites(b *testing.B) {
	store, err := storage.New(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	store.Init(context.Background())

	writers := 8
	perWriter := b.N / writers
	if perWriter < 1 {
		perWriter = 1
	}

	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(wID int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				id := fmt.Sprintf("0xtx-w%d-%08d#0", wID, i)
				store.SeedSwap(id, &storage.SeedSwapData{
					Timestamp: 1711929600 + int64(i),
					Pool:      fmt.Sprintf("0xpool%06d", i%50),
					Amount0:   "100.00",
					Amount1:   "-250.00",
					AmountUSD: "250.00",
					Sender:    fmt.Sprintf("0x%040x", wID*10000+i),
				})
			}
		}(w)
	}
	wg.Wait()
}
