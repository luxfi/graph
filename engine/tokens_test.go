package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luxfi/graph/storage"
)

// A picker offers what a pool holds, and the chain's own coin. Everything else
// the indexer met is a contract, not an asset: a v2 PAIR is an ERC-20, so are
// position receipts, vault shares and an NFT collection, and all four were once
// offered as things to trade.
func TestSwappableTokens_OffersOnlyWhatAPoolHolds(t *testing.T) {
	const (
		wlux = "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e"
		usdc = "0x8031e9b0d02a792cfefaa2bdca6e1289d385426f"
		pair = "0xf07b65def8cbe9f2645157bf69e3e5212d3ced9d" // a v2 pair token
		nft  = "0x10310fed33f22e75a935ace74564a411d5b5196b" // the Genesis collection
	)
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	if err := store.Init(t.Context()); err != nil {
		t.Fatalf("init: %v", err)
	}
	store.SeedToken(wlux, &storage.SeedTokenData{Symbol: "WLUX", Name: "Wrapped LUX", Decimals: 18})
	store.SeedToken(usdc, &storage.SeedTokenData{Symbol: "USDC", Name: "USD Coin", Decimals: 18})
	// Both of these are real ERC-20s the indexer has seen, and neither is held
	// by any pool.
	store.SeedToken(pair, &storage.SeedTokenData{Symbol: "UNI-V2", Name: "Uniswap V2", Decimals: 18})
	store.SeedToken(nft, &storage.SeedTokenData{Symbol: "GENESIS", Name: "Lux Genesis", Decimals: 0})
	store.SeedPool("0x5914d1fb5ec9aa5ac9610afdb8e9a2f209d2b345",
		&storage.SeedPoolData{Token0: wlux, Token1: usdc, FeeTier: 10})

	rec := httptest.NewRecorder()
	HandleSwappableTokens(store, 96369, "LUX", wlux)(rec, httptest.NewRequest(http.MethodGet, "/v1/swappable_tokens", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Tokens []swapToken `json:"tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := map[string]swapToken{}
	for _, tok := range body.Tokens {
		got[tok.Address] = tok
	}

	for _, addr := range []string{wlux, usdc} {
		if _, ok := got[addr]; !ok {
			t.Errorf("%s is held by a pool and must be offered", addr)
		}
	}
	for _, addr := range []string{pair, nft} {
		if tok, ok := got[addr]; ok {
			t.Errorf("%s (%s) is held by no pool and must not be offered", addr, tok.Symbol)
		}
	}

	// Selling the chain's own coin is the commonest trade there is, and the form
	// can only express it through the zero address.
	native, ok := got[nativeSentinelAddr]
	if !ok {
		t.Fatal("the native sentinel is missing — native LUX cannot be sold without it")
	}
	if native.Symbol != "LUX" {
		t.Errorf("sentinel symbol = %q, want LUX", native.Symbol)
	}
	// Its scale is the wrapped token's scale. A sentinel carrying the wrong
	// decimals misprices every trade that starts in native.
	if native.Decimals != 18 {
		t.Errorf("sentinel decimals = %d, want 18 (WLUX's)", native.Decimals)
	}
	if len(body.Tokens) != 3 {
		t.Errorf("offered %d tokens, want 3 (WLUX, USDC, the coin)", len(body.Tokens))
	}
}

// A chain whose wrapped token no pool holds has nothing to price the coin
// against, so the coin is not offered rather than offered unbacked.
func TestSwappableTokens_NoSentinelWithoutAPoolForTheWrapped(t *testing.T) {
	const wlux = "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e"
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	if err := store.Init(t.Context()); err != nil {
		t.Fatalf("init: %v", err)
	}
	store.SeedToken(wlux, &storage.SeedTokenData{Symbol: "WLUX", Decimals: 18})

	rec := httptest.NewRecorder()
	HandleSwappableTokens(store, 96369, "LUX", wlux)(rec, httptest.NewRequest(http.MethodGet, "/v1/swappable_tokens", nil))

	var body struct {
		Tokens []swapToken `json:"tokens"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Tokens) != 0 {
		t.Errorf("offered %d tokens on a chain with no pools, want 0", len(body.Tokens))
	}
}
