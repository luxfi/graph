package indexer

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
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

// fakeERC20RPC is an httptest server that answers eth_call for the four
// metadata selectors with canned ABI-encoded values, and counts the calls so a
// test can assert the per-token cache (one call set per token, never per-block).
type fakeERC20RPC struct {
	symbolHex      string
	nameHex        string
	decimalsHex    string
	totalSupplyHex string
	calls          int32 // total eth_call requests served
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
				case selTotalSupply:
					result = f.totalSupplyHex
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + result + `"}`))
	}
}

// The exact bytes Lux C-Chain mainnet returns for LZOO (0x5E5290f3…cB88):
// totalSupply() = 10863299479896148738270000000 base units at 18 decimals.
const (
	lzooSupplyHex   = "0x00000000000000000000000000000000000000002319e924d2bd6a3b927b7380"
	lzooSupplyWhole = "10863299479.89614873827"
)

// TestWholeUnits is the arithmetic a market cap rests on. A supply is a uint256
// and the answer must survive it digit for digit — float64 gives up around 2^53,
// four orders of magnitude below a two-trillion-token supply.
func TestWholeUnits(t *testing.T) {
	n := func(s string) *big.Int { v, _ := new(big.Int).SetString(s, 10); return v }
	cases := []struct {
		name     string
		raw      *big.Int
		decimals int64
		want     string
	}{
		{"LZOO, the real mainnet supply", n("10863299479896148738270000000"), 18, lzooSupplyWhole},
		{"WLUX, wrapped at head", n("150126795518059496352559779"), 18, "150126795.518059496352559779"},
		{"a whole number keeps no point", n("2000000000000000000000000000000"), 18, "2000000000000"},
		{"six decimals (USDC-style)", n("1234567890"), 6, "1234.56789"},
		{"zero decimals is the number itself", n("1000000000000000000000000000"), 0, "1000000000000000000000000000"},
		{"a fraction below one keeps its leading zeros", n("1"), 18, "0.000000000000000001"},
		{"zero supply", n("0"), 18, "0"},
		{"2^256-1 loses nothing", n("115792089237316195423570985008687907853269984665640564039457584007913129639935"), 18,
			"115792089237316195423570985008687907853269984665640564039457.584007913129639935"},
		{"no answer, no number", nil, 18, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wholeUnits(tc.raw, tc.decimals); got != tc.want {
				t.Errorf("wholeUnits = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeERC20Uint(t *testing.T) {
	got, ok := decodeERC20Uint(lzooSupplyHex)
	if !ok || got.String() != "10863299479896148738270000000" {
		t.Errorf("decodeERC20Uint = (%v,%v), want the LZOO supply", got, ok)
	}
	if _, ok := decodeERC20Uint("0x"); ok {
		t.Error("a reverting totalSupply() must not decode to a number")
	}
}

func lzooRPC() *fakeERC20RPC {
	return &fakeERC20RPC{
		symbolHex: "0x" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"0000000000000000000000000000000000000000000000000000000000000004" +
			"4c5a4f4f00000000000000000000000000000000000000000000000000000000", // LZOO
		nameHex: "0x" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"0000000000000000000000000000000000000000000000000000000000000007" +
			"4c7578205a4f4f0000000000000000000000000000000000000000000000000000", // Lux ZOO
		decimalsHex:    "0x" + strings.Repeat("0", 62) + "12", // 18
		totalSupplyHex: lzooSupplyHex,
	}
}

// TestToken_ReadsAndCaches: a token is read from the chain once, persisted whole
// — supply included, in whole tokens — and served from memory thereafter however
// many times the handlers re-reference it.
func TestToken_ReadsAndCaches(t *testing.T) {
	fake := lzooRPC()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: srv.URL}, s)

	const addr = "0x5e5290f350352768bd2bfc59c2da15dd04a7cb88"
	idx.token(context.Background(), addr)

	tok, err := s.GetToken(nil, addr)
	if err != nil || tok == nil {
		t.Fatalf("token not persisted: %v", err)
	}
	m := tok.(map[string]interface{})
	if m["symbol"] != "LZOO" {
		t.Errorf("symbol = %v, want LZOO", m["symbol"])
	}
	if asInt64(m["decimals"]) != 18 {
		t.Errorf("decimals = %v, want 18", m["decimals"])
	}
	if m["totalSupply"] != lzooSupplyWhole {
		t.Errorf("totalSupply = %v, want %v (whole tokens, not base units)", m["totalSupply"], lzooSupplyWhole)
	}

	// Four: symbol, name, decimals, totalSupply. The number matters less than the
	// bound — this is per TOKEN, once, and the loop below is what proves it never
	// becomes per LOG.
	first := atomic.LoadInt32(&fake.calls)
	if first != 4 {
		t.Fatalf("first sight must issue exactly 4 eth_calls, got %d", first)
	}
	for i := 0; i < 50; i++ {
		idx.token(context.Background(), addr)
	}
	if got := atomic.LoadInt32(&fake.calls); got != first {
		t.Fatalf("cache breach: %d eth_calls after 50 re-references, want %d", got, first)
	}
}

// TestToken_FallbackOnRevert: a non-compliant or reverting contract still yields
// a sane Token entity (address placeholder, 18 decimals, no supply) rather than
// dropping the token or failing the poll. No supply is invented.
func TestToken_FallbackOnRevert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"execution reverted"}}`))
	}))
	defer srv.Close()

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: srv.URL}, s)

	const addr = "0x00000000000000000000000000000000000000ff"
	idx.token(context.Background(), addr)

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
	if m["totalSupply"] != "" {
		t.Errorf("totalSupply = %v; a contract that says nothing must leave it unsaid", m["totalSupply"])
	}
}

// TestBackfill_FillsSupplyOnANamedRow is the bug this pass exists for. Every
// already-indexed token carried a real symbol and no supply, and the guard read
// the symbol as proof there was nothing left to ask — so the supply stayed empty
// however many times the token traded, and the page kept printing a dash.
func TestBackfill_FillsSupplyOnANamedRow(t *testing.T) {
	fake := lzooRPC()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: srv.URL}, s)

	const addr = "0x5e5290f350352768bd2bfc59c2da15dd04a7cb88"
	// The row exactly as production held it: named, priced, traded — no supply.
	s.SeedToken(addr, &storage.SeedTokenData{
		Symbol: "LZOO", Name: "Lux ZOO", Decimals: 18,
		TotalValueLockedUSD: "58450.58", VolumeUSD: "3472.81",
		DerivedETH: "0.045979230476", TxCount: 13278,
	})

	n, err := idx.BackfillTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("backfill settled %d rows, want 1", n)
	}

	m, _ := mustToken(t, s, addr)
	if m["totalSupply"] != lzooSupplyWhole {
		t.Errorf("totalSupply = %v, want %v", m["totalSupply"], lzooSupplyWhole)
	}
	// The pass reads a contract; it must not cost the row anything the valuation
	// pass put there.
	if m["totalValueLockedUSD"] != "58450.58" || m["volumeUSD"] != "3472.81" ||
		m["derivedETH"] != "0.045979230476" || asInt64(m["txCount"]) != 13278 {
		t.Errorf("backfill erased the row's aggregates: %v", m)
	}

	// A settled row costs nothing on a second pass.
	before := atomic.LoadInt32(&fake.calls)
	if n, _ := idx.BackfillTokens(context.Background()); n != 0 {
		t.Errorf("second pass settled %d rows, want 0 (idempotent)", n)
	}
	if got := atomic.LoadInt32(&fake.calls); got != before {
		t.Errorf("second pass issued %d extra eth_calls, want 0", got-before)
	}
}

// TestToken_BlipKeepsWhatIsKnown: a restart with the RPC unreachable must not
// downgrade a stored symbol to the address placeholder, nor erase the supply and
// aggregates already on the row. SeedToken is INSERT OR REPLACE, so a write
// built from the failed read alone would take all of it out.
func TestToken_BlipKeepsWhatIsKnown(t *testing.T) {
	s := newMemSQLiteStore(t)
	const addr = "0xf07b65def8cbe9f2645157bf69e3e5212d3ced9d"
	s.SeedToken(addr, &storage.SeedTokenData{
		Symbol: "UNI-V2", Name: "Uniswap V2", Decimals: 18,
		TotalValueLockedUSD: "1234.00", VolumeUSD: "99.00", TxCount: 7,
	})

	// New process, empty cache, RPC unreachable.
	idx := NewWithConfig(Config{RPC: "http://127.0.0.1:0"}, s)
	idx.token(context.Background(), addr)

	m, _ := mustToken(t, s, addr)
	if m["symbol"] != "UNI-V2" || m["name"] != "Uniswap V2" {
		t.Errorf("blip downgraded identity to %v/%v", m["symbol"], m["name"])
	}
	if m["totalValueLockedUSD"] != "1234.00" || m["volumeUSD"] != "99.00" || asInt64(m["txCount"]) != 7 {
		t.Errorf("blip erased aggregates: %v", m)
	}
}

// TestToken_StoredSupplySkipsTheChain: a settled row is served from disk, so the
// steady state costs no RPC at all.
func TestToken_StoredSupplySkipsTheChain(t *testing.T) {
	fake := lzooRPC()
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	s := newMemSQLiteStore(t)
	const addr = "0x1111111111111111111111111111111111111111"
	s.SeedToken(addr, &storage.SeedTokenData{
		Symbol: "WLUX", Name: "Wrapped LUX", Decimals: 18, TotalSupply: "2000000000000",
	})

	idx := NewWithConfig(Config{RPC: srv.URL}, s)
	got := idx.token(context.Background(), addr)
	if got.TotalSupply != "2000000000000" || got.Symbol != "WLUX" {
		t.Errorf("stored row not adopted: %+v", got)
	}
	if n := atomic.LoadInt32(&fake.calls); n != 0 {
		t.Errorf("settled row cost %d eth_calls, want 0", n)
	}
}

func mustToken(t *testing.T, s *storage.Store, addr string) (map[string]interface{}, bool) {
	t.Helper()
	tok, err := s.GetToken(nil, addr)
	if err != nil || tok == nil {
		t.Fatalf("token %s not persisted: %v", addr, err)
	}
	m, ok := tok.(map[string]interface{})
	if !ok {
		t.Fatalf("token %s has an unexpected shape: %T", addr, tok)
	}
	return m, true
}
