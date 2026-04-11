// Package e2e tests the graph engine against the exact GraphQL queries
// made by ~/work/lux/exchange (the DEX frontend).
//
// These queries are extracted from:
//   - exchange/apps/web/src/state/explore/luxSubgraph.ts
//   - exchange/apps/web/src/state/explore/useExchangeStats.ts
//
// Run: go test -v ./e2e/ -tags=e2e
//
//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/storage"
)

// setupTestEngine creates an engine with seeded test data.
func setupTestEngine(t *testing.T) (*engine.Engine, *httptest.Server) {
	t.Helper()

	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Init(nil); err != nil {
		t.Fatal(err)
	}

	// Seed test data matching Lux mainnet DEX state
	store.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount:           42,
		TxCount:             1337,
		TotalVolumeUSD:      "12345678.90",
		TotalValueLockedUSD: "9876543.21",
	})
	store.SeedBundle("1", &storage.SeedBundleData{
		EthPriceUSD: "2.50",
	})
	store.SeedToken("0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e", &storage.SeedTokenData{
		Symbol:              "WLUX",
		Name:                "Wrapped LUX",
		Decimals:            18,
		VolumeUSD:           "5000000.00",
		TotalValueLockedUSD: "3000000.00",
		DerivedETH:          "1.0",
		TxCount:             500,
	})
	store.SeedToken("0x848Cff46eb323f323b6Bbe1Df274E40793d7f2c2", &storage.SeedTokenData{
		Symbol:              "LUSD",
		Name:                "Lux USD",
		Decimals:            6,
		VolumeUSD:           "8000000.00",
		TotalValueLockedUSD: "4000000.00",
		DerivedETH:          "0.40",
		TxCount:             800,
	})
	store.SeedPool("0xpool1", &storage.SeedPoolData{
		Token0:              "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e",
		Token1:              "0x848Cff46eb323f323b6Bbe1Df274E40793d7f2c2",
		FeeTier:             3000,
		TotalValueLockedUSD: "2000000.00",
		VolumeUSD:           "500000.00",
		Token0Price:         "2.50",
		Token1Price:         "0.40",
		TxCount:             200,
	})
	store.SeedSwap("0xtx1#0", &storage.SeedSwapData{
		Timestamp: 1711929600,
		Pool:      "0xpool1",
		Amount0:   "100.5",
		Amount1:   "-251.25",
		AmountUSD: "251.25",
		Sender:    "0xuser1",
	})

	eng := engine.New(store, nil)
	eng.LoadBuiltin("amm")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", eng.HandleGraphQL)
	mux.HandleFunc("POST /subgraph/v3", eng.HandleGraphQL) // exchange endpoint compat
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return eng, srv
}

func graphqlQuery(t *testing.T, url, query string) map[string]interface{} {
	t.Helper()

	body, _ := json.Marshal(engine.Request{Query: query})
	resp, err := http.Post(url+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	if errs, ok := result["errors"]; ok {
		t.Fatalf("graphql errors: %v", errs)
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Fatal("no data in response")
	}
	return data
}

// Test: factories query (exchange/apps/web/src/state/explore/luxSubgraph.ts:137-147)
func TestExchangeQuery_Factories(t *testing.T) {
	_, srv := setupTestEngine(t)

	data := graphqlQuery(t, srv.URL, `{
		factories(first: 1) {
			poolCount
			txCount
			totalVolumeUSD
			totalValueLockedUSD
		}
	}`)

	factories, ok := data["factories"].([]interface{})
	if !ok || len(factories) == 0 {
		t.Fatal("expected factories array")
	}

	f := factories[0].(map[string]interface{})
	if f["poolCount"] == nil {
		t.Error("missing poolCount")
	}
	if f["totalVolumeUSD"] == nil {
		t.Error("missing totalVolumeUSD")
	}
	if f["totalValueLockedUSD"] == nil {
		t.Error("missing totalValueLockedUSD")
	}
}

// Test: bundle query (exchange/apps/web/src/state/explore/useExchangeStats.ts:115)
func TestExchangeQuery_Bundle(t *testing.T) {
	_, srv := setupTestEngine(t)

	data := graphqlQuery(t, srv.URL, `{
		bundle(id: "1") {
			ethPriceUSD
		}
	}`)

	bundle, ok := data["bundle"].(map[string]interface{})
	if !ok {
		t.Fatal("expected bundle object")
	}
	if bundle["ethPriceUSD"] == nil {
		t.Error("missing ethPriceUSD")
	}
}

// Test: tokens query (exchange/apps/web/src/state/explore/luxSubgraph.ts:92-104)
func TestExchangeQuery_Tokens(t *testing.T) {
	_, srv := setupTestEngine(t)

	data := graphqlQuery(t, srv.URL, `{
		tokens(first: 50, orderBy: volumeUSD, orderDirection: desc) {
			id
			symbol
			name
			decimals
			volumeUSD
			totalValueLockedUSD
			derivedETH
			txCount
		}
	}`)

	tokens, ok := data["tokens"].([]interface{})
	if !ok {
		t.Fatal("expected tokens array")
	}
	if len(tokens) == 0 {
		t.Fatal("expected at least 1 token")
	}

	tok := tokens[0].(map[string]interface{})
	for _, field := range []string{"id", "symbol", "name", "volumeUSD", "totalValueLockedUSD", "derivedETH"} {
		if tok[field] == nil {
			t.Errorf("missing field: %s", field)
		}
	}
}

// Test: pools query (exchange/apps/web/src/state/explore/luxSubgraph.ts:106-119)
func TestExchangeQuery_Pools(t *testing.T) {
	_, srv := setupTestEngine(t)

	data := graphqlQuery(t, srv.URL, `{
		pools(first: 50, orderBy: totalValueLockedUSD, orderDirection: desc) {
			id
			token0 { id symbol name decimals }
			token1 { id symbol name decimals }
			feeTier
			liquidity
			totalValueLockedUSD
			volumeUSD
			token0Price
			token1Price
			txCount
		}
	}`)

	pools, ok := data["pools"].([]interface{})
	if !ok {
		t.Fatal("expected pools array")
	}
	if len(pools) == 0 {
		t.Fatal("expected at least 1 pool")
	}

	pool := pools[0].(map[string]interface{})
	if pool["token0"] == nil {
		t.Error("missing token0")
	}
	if pool["token1"] == nil {
		t.Error("missing token1")
	}
	if pool["feeTier"] == nil {
		t.Error("missing feeTier")
	}
	if pool["totalValueLockedUSD"] == nil {
		t.Error("missing totalValueLockedUSD")
	}
}

// Test: swaps query (exchange/apps/web/src/state/explore/luxSubgraph.ts:121-135)
func TestExchangeQuery_Swaps(t *testing.T) {
	_, srv := setupTestEngine(t)

	data := graphqlQuery(t, srv.URL, `{
		swaps(first: 200, orderBy: timestamp, orderDirection: desc) {
			id
			timestamp
			amount0
			amount1
			amountUSD
			sender
		}
	}`)

	swaps, ok := data["swaps"].([]interface{})
	if !ok {
		t.Fatal("expected swaps array")
	}
	if len(swaps) == 0 {
		t.Fatal("expected at least 1 swap")
	}

	swap := swaps[0].(map[string]interface{})
	for _, field := range []string{"id", "timestamp", "amount0", "amount1", "amountUSD", "sender"} {
		if swap[field] == nil {
			t.Errorf("missing field: %s", field)
		}
	}
}

// Test: combined dashboard query (exchange/apps/web/src/state/explore/useExchangeStats.ts:115-133)
// This is the main query the exchange landing page makes on load.
func TestExchangeQuery_Dashboard(t *testing.T) {
	_, srv := setupTestEngine(t)

	data := graphqlQuery(t, srv.URL, `{
		bundle(id: "1") { ethPriceUSD }
		factories(first: 1) { id poolCount txCount totalVolumeUSD totalValueLockedUSD }
		tokens(first: 50, orderBy: totalValueLockedUSD, orderDirection: desc) {
			id symbol name decimals totalValueLockedUSD derivedETH
		}
		pools(first: 50, orderBy: totalValueLockedUSD, orderDirection: desc) {
			id token0 { id symbol } token1 { id symbol }
			feeTier totalValueLockedUSD token0Price token1Price
		}
	}`)

	if data["bundle"] == nil {
		t.Error("missing bundle")
	}
	if data["factories"] == nil {
		t.Error("missing factories")
	}
	if data["tokens"] == nil {
		t.Error("missing tokens")
	}
	if data["pools"] == nil {
		t.Error("missing pools")
	}
}

// Test: subgraph/v3 endpoint (exchange uses this path)
func TestExchangeQuery_SubgraphV3Endpoint(t *testing.T) {
	_, srv := setupTestEngine(t)

	body, _ := json.Marshal(engine.Request{
		Query: `{ factories(first: 1) { poolCount } }`,
	})

	resp, err := http.Post(srv.URL+"/subgraph/v3", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// Test: health endpoint
func TestHealthEndpoint(t *testing.T) {
	_, srv := setupTestEngine(t)

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatal("health check failed")
	}
}

// Test: mutations are rejected (read-only)
func TestMutationsRejected(t *testing.T) {
	_, srv := setupTestEngine(t)

	body, _ := json.Marshal(engine.Request{
		Query: `mutation { createToken(symbol: "HACK") { id } }`,
	})

	resp, err := http.Post(srv.URL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	errs, ok := result["errors"].([]interface{})
	if !ok || len(errs) == 0 {
		t.Fatal("expected mutation to be rejected")
	}
}

// Test: negative first does not panic (F3 red-team finding)
func TestNegativeFirstDoesNotPanic(t *testing.T) {
	_, srv := setupTestEngine(t)

	body, _ := json.Marshal(engine.Request{
		Query: `{ tokens(first: -1) { id symbol } }`,
	})

	resp, err := http.Post(srv.URL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}

	// Must not panic — any valid JSON response (data or errors) is acceptable
	if result["data"] == nil && result["errors"] == nil {
		t.Fatal("expected data or errors in response")
	}
}

// Test: where filter returns only matching records
func TestWhereFilter(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(nil); err != nil {
		t.Fatal(err)
	}

	// Seed 3 swaps for different pools (pool IDs not in pool store, so pool stays as string)
	store.SeedSwap("swap-a", &storage.SeedSwapData{Timestamp: 100, Pool: "0xpoolA", Amount0: "1", Amount1: "2", AmountUSD: "3", Sender: "0xsender1"})
	store.SeedSwap("swap-b", &storage.SeedSwapData{Timestamp: 200, Pool: "0xpoolA", Amount0: "4", Amount1: "5", AmountUSD: "6", Sender: "0xsender2"})
	store.SeedSwap("swap-c", &storage.SeedSwapData{Timestamp: 300, Pool: "0xpoolB", Amount0: "7", Amount1: "8", AmountUSD: "9", Sender: "0xsender3"})

	eng := engine.New(store, nil)
	eng.LoadBuiltin("amm")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", eng.HandleGraphQL)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	data := graphqlQuery(t, srv.URL, `{
		swaps(where: { pool: "0xpoolA" }) {
			id
			pool
		}
	}`)

	swaps, ok := data["swaps"].([]interface{})
	if !ok {
		t.Fatal("expected swaps array")
	}
	if len(swaps) != 2 {
		t.Fatalf("expected 2 swaps for 0xpoolA, got %d", len(swaps))
	}
}

// Test: SQLite persistence across close/reopen
func TestSQLitePersistence(t *testing.T) {
	dir := t.TempDir()

	// Create, seed, close
	store1, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store1.Init(nil); err != nil {
		t.Fatal(err)
	}
	store1.SeedToken("0xabc", &storage.SeedTokenData{Symbol: "TST", Name: "Test Token", Decimals: 18})
	store1.SeedSwap("swap-persist", &storage.SeedSwapData{
		Timestamp: 999, Pool: "0xpool", Amount0: "10", Amount1: "20", AmountUSD: "30", Sender: "0xuser",
	})
	store1.SetLastBlock(42)
	if err := store1.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen same directory
	store2, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store2.Init(nil); err != nil {
		t.Fatal(err)
	}
	defer store2.Close()

	if lb := store2.GetLastBlock(); lb != 42 {
		t.Fatalf("expected lastBlock=42 after reopen, got %d", lb)
	}

	eng := engine.New(store2, nil)
	eng.LoadBuiltin("amm")

	resp := eng.Execute(nil, &engine.Request{Query: `{ tokens(first: 10) { id symbol } }`})
	if len(resp.Errors) > 0 {
		t.Fatalf("query errors: %v", resp.Errors)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data map")
	}
	tokens, ok := data["tokens"].([]interface{})
	if !ok || len(tokens) == 0 {
		t.Fatal("expected tokens to survive close/reopen")
	}
}

// Test: empty query rejected
func TestEmptyQueryRejected(t *testing.T) {
	_, srv := setupTestEngine(t)

	body, _ := json.Marshal(engine.Request{Query: ""})
	resp, err := http.Post(srv.URL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	errs, ok := result["errors"].([]interface{})
	if !ok || len(errs) == 0 {
		t.Fatal("expected empty query to be rejected")
	}
}
