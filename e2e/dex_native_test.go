// Native-DEX (CLOB) + uniswap-alias e2e.
//
// Proves the explorer's `dex` graph schema serves the Fill/Market entities the
// 0x9999 indexer writes, and that the uniswap-v2 `uniswapFactories` spelling
// resolves against the same factory rows as `factories`.
//
// Run: go test -v ./e2e/ -tags=e2e
//
//go:build e2e

package e2e

import (
	"testing"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/storage"
)

// TestUniswapFactoriesAlias proves the uniswap-v2 spelling resolves to the same
// data as `factories` (the schema delta the exchange-api used to hit as
// "unknown field: uniswapFactories").
func TestUniswapFactoriesAlias(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(nil); err != nil {
		t.Fatal(err)
	}
	store.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount: 7, TxCount: 70, TotalVolumeUSD: "100.0", TotalValueLockedUSD: "200.0",
	})

	eng := engine.New(store, nil)
	if err := eng.LoadBuiltin("amm"); err != nil {
		t.Fatal(err)
	}

	resp := eng.Execute(nil, &engine.Request{Query: `{
		uniswapFactories(first: 1) {
			poolCount
			totalVolumeUSD
			totalValueLockedUSD
		}
	}`})
	if len(resp.Errors) > 0 {
		t.Fatalf("uniswapFactories errors: %v", resp.Errors)
	}
	data := resp.Data.(map[string]interface{})
	arr, ok := data["uniswapFactories"].([]interface{})
	if !ok || len(arr) == 0 {
		t.Fatalf("expected uniswapFactories array, got %#v", data["uniswapFactories"])
	}
	f := arr[0].(map[string]interface{})
	if f["poolCount"] == nil {
		t.Error("missing poolCount via uniswapFactories alias")
	}

	// Singular alias too.
	resp = eng.Execute(nil, &engine.Request{Query: `{ uniswapFactory(id: "1") { poolCount } }`})
	if len(resp.Errors) > 0 {
		t.Fatalf("uniswapFactory errors: %v", resp.Errors)
	}
}

// TestNativeDexSchema proves the `dex` (CLOB) schema serves Order/Market/Fill
// entities — the shapes the 0x9999 indexer writes via SetEntity.
func TestNativeDexSchema(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(nil); err != nil {
		t.Fatal(err)
	}

	// Seed the entity shapes the indexer's writeFill / handleInitializeV4 emit.
	store.SetEntity("Market", "0xpool", map[string]interface{}{
		"id": "0xpool", "symbol": "LUX/LUSD", "volume24h": "2500", "tradeCount": int64(1),
	})
	store.SetEntity("Fill", "0xtx#0", map[string]interface{}{
		"id": "0xtx#0", "market": "0xpool", "taker": "0xaa", "amountOut": "2500", "timestamp": int64(16),
	})

	eng := engine.New(store, nil)
	if err := eng.LoadBuiltin("dex"); err != nil {
		t.Fatal(err)
	}

	resp := eng.Execute(nil, &engine.Request{Query: `{
		markets(first: 10) { id symbol volume24h tradeCount }
		fills(first: 10) { id market taker amountOut }
	}`})
	if len(resp.Errors) > 0 {
		t.Fatalf("dex schema errors: %v", resp.Errors)
	}
	data := resp.Data.(map[string]interface{})

	markets, ok := data["markets"].([]interface{})
	if !ok || len(markets) != 1 {
		t.Fatalf("expected 1 market, got %#v", data["markets"])
	}
	if markets[0].(map[string]interface{})["symbol"] != "LUX/LUSD" {
		t.Errorf("market symbol = %v", markets[0].(map[string]interface{})["symbol"])
	}

	fills, ok := data["fills"].([]interface{})
	if !ok || len(fills) != 1 {
		t.Fatalf("expected 1 fill, got %#v", data["fills"])
	}
	if fills[0].(map[string]interface{})["amountOut"] != "2500" {
		t.Errorf("fill amountOut = %v", fills[0].(map[string]interface{})["amountOut"])
	}
}
