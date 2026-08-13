package indexer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luxfi/graph/storage"
)

// TestDecodeERC20String covers both on-wire shapes plus the malformed/empty
// fallbacks. The "real" cases are the exact bytes Lux C-Chain mainnet returns
// for 0xf07b65… (symbol "UNI-V2", name "Uniswap V2") — the token the bug
// reported as symbol="0xf07b65".
func TestDecodeERC20String(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want string
	}{
		{
			name: "abi string UNI-V2 (real on-chain symbol)",
			hex: "0x" +
				"0000000000000000000000000000000000000000000000000000000000000020" + // offset
				"0000000000000000000000000000000000000000000000000000000000000006" + // len 6
				"554e492d56320000000000000000000000000000000000000000000000000000", // "UNI-V2"
			want: "UNI-V2",
		},
		{
			name: "abi string Uniswap V2 (real on-chain name)",
			hex: "0x" +
				"0000000000000000000000000000000000000000000000000000000000000020" +
				"000000000000000000000000000000000000000000000000000000000000000a" + // len 10
				"556e697377617020563200000000000000000000000000000000000000000000", // "Uniswap V2"
			want: "Uniswap V2",
		},
		{
			name: "bytes32 symbol (MKR-style, no offset/length header)",
			// "MKR" right-padded in a single 32-byte word.
			hex:  "0x4d4b520000000000000000000000000000000000000000000000000000000000",
			want: "MKR",
		},
		{
			name: "empty abi string (length 0)",
			hex: "0x" +
				"0000000000000000000000000000000000000000000000000000000000000020" +
				"0000000000000000000000000000000000000000000000000000000000000000",
			want: "",
		},
		{name: "empty result", hex: "0x", want: ""},
		{name: "0x prefix only with no data", hex: "", want: ""},
		{name: "all-zero bytes32", hex: "0x" + strings.Repeat("0", 64), want: ""},
		{
			name: "abi string with declared length past payload (malformed) -> empty",
			hex: "0x" +
				"0000000000000000000000000000000000000000000000000000000000000020" +
				"00000000000000000000000000000000000000000000000000000000000000ff", // claims 255 bytes, none present
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeERC20String(tc.hex); got != tc.want {
				t.Errorf("decodeERC20String(%s) = %q, want %q", tc.hex, got, tc.want)
			}
		})
	}
}

func TestDecodeERC20Decimals(t *testing.T) {
	cases := []struct {
		name   string
		hex    string
		want   int64
		wantOK bool
	}{
		{"18 decimals (real on-chain)", "0x" + "00000000000000000000000000000000000000000000000000000000000000" + "12", 18, true},
		{"6 decimals (USDC-style)", "0x" + strings.Repeat("0", 62) + "06", 6, true},
		{"0 decimals", "0x" + strings.Repeat("0", 64), 0, true},
		{"empty -> not ok", "0x", 0, false},
		{"short word -> not ok", "0x12", 0, false},
		{
			name:   "garbage high bytes (non-uint8) -> not ok",
			hex:    "0x" + "ff" + strings.Repeat("0", 60) + "12",
			want:   0,
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := decodeERC20Decimals(tc.hex)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Errorf("decodeERC20Decimals(%s) = (%d,%v), want (%d,%v)", tc.hex, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// fakeERC20RPC is an httptest server that answers eth_call for the three
// metadata selectors with canned ABI-encoded values, and counts the calls so a
// test can assert the per-token cache (one call set per token, never per-block).
type fakeERC20RPC struct {
	symbolHex   string
	nameHex     string
	decimalsHex string
	calls       int32 // total eth_call requests served
}

func (f *fakeERC20RPC) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string        `json:"method"`
			Params []interface{} `json:"params"`
		}
		_ = json.Unmarshal(body, &req)

		result := "0x"
		if req.Method == "eth_call" {
			atomic.AddInt32(&f.calls, 1)
			if call, ok := req.Params[0].(map[string]interface{}); ok {
				switch call["data"] {
				case selSymbol:
					result = f.symbolHex
				case selName:
					result = f.nameHex
				case selDecimals:
					result = f.decimalsHex
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + result + `"}`))
	}
}

// TestSeedToken_EnrichesAndCaches proves the fix end to end: a token is enriched
// with the real ERC20 symbol/name/decimals from the chain, persisted, and read
// at most once regardless of how many times the handlers re-reference it.
func TestSeedToken_EnrichesAndCaches(t *testing.T) {
	fake := &fakeERC20RPC{
		symbolHex: "0x" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"0000000000000000000000000000000000000000000000000000000000000006" +
			"554e492d56320000000000000000000000000000000000000000000000000000", // UNI-V2
		nameHex: "0x" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"000000000000000000000000000000000000000000000000000000000000000a" +
			"556e697377617020563200000000000000000000000000000000000000000000", // Uniswap V2
		decimalsHex: "0x" + strings.Repeat("0", 62) + "12", // 18
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: srv.URL}, s)

	const addr = "0xf07b65def8cbe9f2645157bf69e3e5212d3ced9d"

	// First sight: reads the chain and persists the real metadata.
	idx.seedToken(context.Background(), addr)

	tok, err := s.GetToken(nil, addr)
	if err != nil || tok == nil {
		t.Fatalf("token not persisted: %v", err)
	}
	m := tok.(map[string]interface{})
	if m["symbol"] != "UNI-V2" {
		t.Errorf("symbol = %v, want UNI-V2", m["symbol"])
	}
	if m["name"] != "Uniswap V2" {
		t.Errorf("name = %v, want Uniswap V2", m["name"])
	}
	if asInt64(m["decimals"]) != 18 {
		t.Errorf("decimals = %v, want 18", m["decimals"])
	}

	// Four: symbol, name, decimals, totalSupply. The number matters less than
	// the bound — this is per TOKEN, once, and the loop below is what proves it
	// never becomes per LOG.
	callsAfterFirst := atomic.LoadInt32(&fake.calls)
	if callsAfterFirst != 4 {
		t.Fatalf("first sight must issue exactly 4 eth_calls (symbol/name/decimals/totalSupply), got %d", callsAfterFirst)
	}

	// Re-reference the same token many times (as Transfer/Swap handlers do every
	// block): the cache must serve it with ZERO additional RPC.
	for i := 0; i < 50; i++ {
		idx.seedToken(context.Background(), addr)
	}
	if got := atomic.LoadInt32(&fake.calls); got != callsAfterFirst {
		t.Fatalf("cache breach: %d eth_calls after 50 re-references, want %d (one read per token, never per-block)", got, callsAfterFirst)
	}
}

// TestSeedToken_FallbackOnRevert proves a non-compliant/reverting token still
// yields a sane Token entity (address placeholder, 18 decimals) rather than
// dropping it or failing the poll.
func TestSeedToken_FallbackOnRevert(t *testing.T) {
	// Server that errors every eth_call (simulates a revert / non-ERC20 contract).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"execution reverted"}}`))
	}))
	defer srv.Close()

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: srv.URL}, s)

	const addr = "0x00000000000000000000000000000000000000ff"
	idx.seedToken(context.Background(), addr)

	tok, _ := s.GetToken(nil, addr)
	if tok == nil {
		t.Fatal("token must still be seeded on revert (with placeholder)")
	}
	m := tok.(map[string]interface{})
	if m["symbol"] != shortAddr(addr) {
		t.Errorf("symbol = %v, want placeholder %v", m["symbol"], shortAddr(addr))
	}
	if asInt64(m["decimals"]) != defaultDecimals {
		t.Errorf("decimals = %v, want default %d", m["decimals"], defaultDecimals)
	}
}

// TestBackfillTokens enriches placeholder rows (from an older build) without a
// re-sync, skips rows that already have a real symbol, and leaves a reverting
// token as a (cached) placeholder.
func TestBackfillTokens(t *testing.T) {
	fake := &fakeERC20RPC{
		symbolHex: "0x" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"0000000000000000000000000000000000000000000000000000000000000006" +
			"554e492d56320000000000000000000000000000000000000000000000000000", // UNI-V2
		nameHex: "0x" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"000000000000000000000000000000000000000000000000000000000000000a" +
			"556e697377617020563200000000000000000000000000000000000000000000", // Uniswap V2
		decimalsHex: "0x" + strings.Repeat("0", 62) + "12",
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: srv.URL}, s)

	const placeholder = "0xf07b65def8cbe9f2645157bf69e3e5212d3ced9d"
	const alreadyGood = "0x1111111111111111111111111111111111111111"

	// A placeholder row exactly as the buggy build wrote it.
	s.SeedToken(placeholder, &storage.SeedTokenData{Symbol: shortAddr(placeholder), Name: placeholder, Decimals: 18})
	// A row already enriched by the fixed build — must be left untouched (0 RPC).
	s.SeedToken(alreadyGood, &storage.SeedTokenData{Symbol: "WLUX", Name: "Wrapped LUX", Decimals: 18})

	n, err := idx.BackfillTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("backfill enriched %d, want 1 (only the placeholder)", n)
	}
	// The already-good row must not have been probed.
	if got := atomic.LoadInt32(&fake.calls); got != 4 {
		t.Fatalf("backfill issued %d eth_calls, want 4 (one placeholder token only)", got)
	}

	tok, _ := s.GetToken(nil, placeholder)
	if m := tok.(map[string]interface{}); m["symbol"] != "UNI-V2" || m["name"] != "Uniswap V2" {
		t.Errorf("placeholder not enriched: %v", m)
	}
	good, _ := s.GetToken(nil, alreadyGood)
	if m := good.(map[string]interface{}); m["symbol"] != "WLUX" {
		t.Errorf("already-good row was clobbered: %v", m)
	}
}

// TestSeedToken_NoDowngradeOnColdStartBlip proves the cold-start clobber guard:
// an already-enriched stored token is NOT overwritten with the address
// placeholder when a transient RPC failure occurs after a restart (empty cache).
func TestSeedToken_NoDowngradeOnColdStartBlip(t *testing.T) {
	s := newMemSQLiteStore(t)
	const addr = "0xf07b65def8cbe9f2645157bf69e3e5212d3ced9d"

	// Simulate a prior run having persisted real metadata.
	s.SeedToken(addr, &storage.SeedTokenData{Symbol: "UNI-V2", Name: "Uniswap V2", Decimals: 18})

	// New process, empty cache, RPC unreachable (blip).
	idx := NewWithConfig(Config{RPC: "http://127.0.0.1:0"}, s)
	idx.seedToken(context.Background(), addr)

	tok, _ := s.GetToken(nil, addr)
	m := tok.(map[string]interface{})
	if m["symbol"] != "UNI-V2" {
		t.Errorf("cold-start blip downgraded symbol to %v; must keep stored UNI-V2", m["symbol"])
	}
}
