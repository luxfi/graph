package engine

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luxfi/graph/storage"
)

// The addresses the fake book is built from. Distinguishable on sight, so a
// failure reads as "the route went through B" rather than as forty hex digits.
const (
	tokenA = "0x000000000000000000000000000000000000aaaa"
	tokenB = "0x000000000000000000000000000000000000bbbb"
	tokenC = "0x000000000000000000000000000000000000cccc"

	poolAC2 = "0x0000000000000000000000000000000000002200" // A/C, constant product
	poolAC3 = "0x0000000000000000000000000000000000003300" // A/C, concentrated
	poolAB3 = "0x0000000000000000000000000000000000003ab0"
	poolBC3 = "0x0000000000000000000000000000000000003bc0"

	fakeQuoter = "0x0000000000000000000000000000000000009999"
	fakeHead   = 1158221
)

var e18 = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

func tokens(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), e18) }

// bigOf reads a decimal too wide for an int64 — a sqrt price is a 160-bit value.
func bigOf(s string) *big.Int {
	n, _ := new(big.Int).SetString(s, 10)
	return n
}

// ── the constant product ──────────────────────────────────────────────────
//
// Every expected value below was worked out by hand from x·y=k with the fee
// withheld from the input, not read back from this package.

func TestAmountOut(t *testing.T) {
	cases := []struct {
		name          string
		in, rIn, rOut *big.Int
		fee           int64
		want          string
	}{
		{"symmetric pool, one token in", tokens(1), tokens(100), tokens(100), 3000, "987158034397061298"},
		{"ten tokens moves the price", tokens(10), tokens(100), tokens(100), 3000, "9066108938801491315"},
		{"asymmetric reserves", tokens(5), tokens(1000), tokens(4000), 3000, "19841092155604312502"},
		{"a thousand base units", big.NewInt(1000), tokens(100), tokens(100), 3000, "996"},
		// A constant product can be pushed towards a reserve but never through
		// it: a million tokens in draws out not quite the whole hundred.
		{"more input than the pool holds", tokens(1000000), tokens(100), tokens(100), 3000, "99989970915655400661"},
		{"five basis points", tokens(1), tokens(100), tokens(100), 500, "989608859449799256"},
		{"one percent", tokens(1), tokens(100), tokens(100), 10000, "980295078720665412"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := amountOut(c.in, c.rIn, c.rOut, c.fee)
			if got == nil {
				t.Fatalf("amountOut = nil, want %s", c.want)
			}
			if got.String() != c.want {
				t.Errorf("amountOut = %s, want %s", got, c.want)
			}
		})
	}
}

func TestAmountOutRefuses(t *testing.T) {
	cases := []struct {
		name          string
		in, rIn, rOut *big.Int
		fee           int64
	}{
		{"nothing in", big.NewInt(0), tokens(100), tokens(100), 3000},
		{"negative in", big.NewInt(-1), tokens(100), tokens(100), 3000},
		{"empty inbound reserve", tokens(1), big.NewInt(0), tokens(100), 3000},
		{"empty outbound reserve", tokens(1), tokens(100), big.NewInt(0), 3000},
		{"the whole trade is fee", tokens(1), tokens(100), tokens(100), 1_000_000},
		{"negative fee", tokens(1), tokens(100), tokens(100), -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := amountOut(c.in, c.rIn, c.rOut, c.fee); got != nil {
				t.Errorf("amountOut = %s, want no answer", got)
			}
		})
	}
}

func TestAmountIn(t *testing.T) {
	cases := []struct {
		name           string
		out, rIn, rOut *big.Int
		fee            int64
		want           string
	}{
		{"symmetric pool, one token out", tokens(1), tokens(100), tokens(100), 3000, "1013140431395195689"},
		{"half the outbound reserve", tokens(50), tokens(100), tokens(100), 3000, "100300902708124373120"},
		{"asymmetric reserves", tokens(20), tokens(1000), tokens(4000), 3000, "5040246367242430811"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := amountIn(c.out, c.rIn, c.rOut, c.fee)
			if got == nil {
				t.Fatalf("amountIn = nil, want %s", c.want)
			}
			if got.String() != c.want {
				t.Errorf("amountIn = %s, want %s", got, c.want)
			}
			// The input the pool demands must actually buy what was asked for.
			// This is the property the trailing +1 exists to hold: without it the
			// truncated quotient buys one base unit short and the pool reverts.
			back := amountOut(got, c.rIn, c.rOut, c.fee)
			if back == nil || back.Cmp(c.out) < 0 {
				t.Errorf("paying %s buys %v, short of %s", got, back, c.out)
			}
		})
	}
}

// A pool cannot be emptied: asking for its whole outbound reserve, or more, has
// no answer at any price.
func TestAmountInRefusesToDrainThePool(t *testing.T) {
	for _, out := range []*big.Int{tokens(100), tokens(101), tokens(1000)} {
		if got := amountIn(out, tokens(100), tokens(100), 3000); got != nil {
			t.Errorf("amountIn(out=%s) = %s, want no answer", out, got)
		}
	}
}

// Fees are parts per million here; Uniswap's pair writes the same rule as
// 997/1000. At 3000 ppm they must be the same integers, not merely close.
func TestFeePPMMatchesUniswapsNinetyNineSeven(t *testing.T) {
	classic := func(in, rIn, rOut *big.Int) *big.Int {
		afterFee := new(big.Int).Mul(in, big.NewInt(997))
		num := new(big.Int).Mul(afterFee, rOut)
		den := new(big.Int).Add(new(big.Int).Mul(rIn, big.NewInt(1000)), afterFee)
		return num.Quo(num, den)
	}
	cases := [][3]*big.Int{
		{tokens(1), tokens(100), tokens(100)},
		{tokens(5), tokens(1000), tokens(4000)},
		{big.NewInt(1000), tokens(100), tokens(100)},
		{tokens(1000000), tokens(100), tokens(100)},
		{big.NewInt(7), big.NewInt(13), big.NewInt(29)}, // tiny, where truncation bites
	}
	for _, c := range cases {
		want := classic(c[0], c[1], c[2])
		got := amountOut(c[0], c[1], c[2], 3000)
		if got == nil || got.Cmp(want) != 0 {
			t.Errorf("in=%s reserves=%s/%s: ppm gave %v, 997/1000 gives %s", c[0], c[1], c[2], got, want)
		}
	}
}

// ── price impact ──────────────────────────────────────────────────────────

func TestPriceImpact(t *testing.T) {
	// A leg that pays exactly its marginal rate has no impact; one that pays
	// half of it has 50%.
	crossed := func(in, out int64, marginal float64) *leg {
		return &leg{amtIn: big.NewInt(in), amtOut: big.NewInt(out), marginal: marginal}
	}
	cases := []struct {
		name string
		r    *route
		want float64
	}{
		{"trade at the marginal rate", &route{legs: []*leg{crossed(100, 100, 1.0)}}, 0},
		{"half the marginal rate", &route{legs: []*leg{crossed(100, 50, 1.0)}}, 50},
		{"compounded across two legs", &route{legs: []*leg{crossed(100, 90, 1.0), crossed(90, 81, 1.0)}}, 19},
		// Rounding can put a leg a hair above its own marginal rate. That is not
		// a negative impact, it is no impact.
		{"a shade better than marginal", &route{legs: []*leg{crossed(100, 101, 1.0)}}, 0},
		{"no marginal to compare against", &route{legs: []*leg{crossed(100, 50, 0)}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := priceImpact(c.r)
			if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("priceImpact = %v, want %v", got, c.want)
			}
		})
	}
}

// ── the ABI ───────────────────────────────────────────────────────────────

// The selectors are derived from their signatures rather than pasted, so this
// pins them to the four bytes the deployed contracts actually answer to. Each
// was confirmed against Lux mainnet before it was written down.
func TestSelectors(t *testing.T) {
	for _, c := range []struct{ got, want, name string }{
		{selQuoteExactIn, "0xc6a5026a", "quoteExactInputSingle"},
		{selQuoteExactOut, "0xbd21704a", "quoteExactOutputSingle"},
		{selGetReserves, "0x0902f1ac", "getReserves"},
		{selSlot0, "0x3850c7bd", "slot0"},
		{selLiquidity, "0x1a686502", "liquidity"},
	} {
		if c.got != c.want {
			t.Errorf("%s selector = %s, want %s", c.name, c.got, c.want)
		}
	}
}

func TestWordsAndSigned(t *testing.T) {
	// slot0's first two values as the WLUX/LUSD pool returned them.
	data := "0x000000000000000000000000000000000000000006567adc849cf073f1d839e8" +
		"fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffedf07"
	w := words(data)
	if len(w) != 2 {
		t.Fatalf("words = %d, want 2", len(w))
	}
	if got := w[0].String(); got != "1961457875965311724333578728" {
		t.Errorf("sqrtPriceX96 = %s", got)
	}
	if got := signed(w[1]); got != -73977 {
		t.Errorf("tick = %d, want -73977", got)
	}
	if got := signed(big.NewInt(12345)); got != 12345 {
		t.Errorf("a positive word must survive unchanged, got %d", got)
	}
}

func TestChecksumAddress(t *testing.T) {
	cases := [][2]string{
		// The live service echoed this exact capitalisation back for a
		// lower-cased request, which is what makes it the reference here.
		{"0x00000000000000000000000000000000deadbeef", "0x00000000000000000000000000000000DeaDBeef"},
		{"0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e", "0x4888E4a2Ee0F03051c72D2BD3ACf755eD3498B3E"},
		{"0x848cff46eb323f323b6bbe1df274e40793d7f2c2", "0x848Cff46eb323f323b6Bbe1Df274E40793d7f2c2"},
		// Already checksummed in, unchanged out.
		{"0x4888E4a2Ee0F03051c72D2BD3ACf755eD3498B3E", "0x4888E4a2Ee0F03051c72D2BD3ACf755eD3498B3E"},
	}
	for _, c := range cases {
		if got := checksumAddress(c[0]); got != c[1] {
			t.Errorf("checksumAddress(%s) = %s, want %s", c[0], got, c[1])
		}
	}
}

func TestParseWei(t *testing.T) {
	good := map[string]string{
		"0":                   "0",
		"1000000000000000000": "1000000000000000000",
		"115792089237316195423570985008687907853269984665640564039457584007913129639935": "115792089237316195423570985008687907853269984665640564039457584007913129639935",
	}
	for in, want := range good {
		got, ok := parseWei(in)
		if !ok || got.String() != want {
			t.Errorf("parseWei(%q) = %v, %v", in, got, ok)
		}
	}
	// A wei amount is digits. Anything else is a different question, and reading
	// it as a number would round somebody's balance.
	for _, bad := range []string{"", "-5", "1.5", "0x10", " 1", "1e18", "abc"} {
		if _, ok := parseWei(bad); ok {
			t.Errorf("parseWei(%q) accepted", bad)
		}
	}
}

func TestIsHexAddress(t *testing.T) {
	if !isHexAddress(tokenA) || !isHexAddress("0x4888E4a2Ee0F03051c72D2BD3ACf755eD3498B3E") {
		t.Error("a 20-byte hex address must be accepted in either case")
	}
	for _, bad := range []string{"", "0x", "nothex", "4888e4a2ee0f03051c72d2bd3acf755ed3498b3e",
		"0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3", "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3ee",
		"0xzzzz4a2ee0f03051c72d2bd3acf755ed3498b3e"} {
		if isHexAddress(bad) {
			t.Errorf("isHexAddress(%q) accepted", bad)
		}
	}
}

// ── routing over the indexed book ─────────────────────────────────────────

// The middle of a two-hop route is found in the book, not configured. This is
// the whole reason the endpoint sits on the indexer.
func TestRoutesDiscoversTheMiddleToken(t *testing.T) {
	book := []*pool{
		{addr: poolAB3, t0: tokenA, t1: tokenB, fee: 3000},
		{addr: poolBC3, t0: tokenB, t1: tokenC, fee: 3000},
	}
	got := routes(book, tokenA, tokenC)
	if len(got) != 1 {
		t.Fatalf("routes = %d, want 1", len(got))
	}
	if n := len(got[0].legs); n != 2 {
		t.Fatalf("legs = %d, want 2", n)
	}
	if got[0].legs[0].out != tokenB || got[0].legs[1].in != tokenB {
		t.Errorf("route did not pass through B: %+v", got[0].legs)
	}
}

func TestRoutesFindsDirectAndIndirect(t *testing.T) {
	book := []*pool{
		{addr: poolAC3, t0: tokenA, t1: tokenC, fee: 3000},
		{addr: poolAB3, t0: tokenA, t1: tokenB, fee: 3000},
		{addr: poolBC3, t0: tokenB, t1: tokenC, fee: 3000},
	}
	got := routes(book, tokenA, tokenC)
	if len(got) != 2 {
		t.Fatalf("routes = %d, want 2 (one direct, one through B)", len(got))
	}
	if len(got[0].legs) != 1 {
		t.Errorf("the direct route should come first, got %d legs", len(got[0].legs))
	}
}

func TestRoutesReportsNothingWhenTheBookDoesNotConnect(t *testing.T) {
	book := []*pool{{addr: poolAB3, t0: tokenA, t1: tokenB, fee: 3000}}
	if got := routes(book, tokenA, tokenC); len(got) != 0 {
		t.Errorf("routes = %d, want none — nothing in the book reaches C", len(got))
	}
}

// ── a fake chain ──────────────────────────────────────────────────────────

// fakePool is one venue the fake node serves. A v2 pair answers getReserves and
// the quoter knows nothing of it; a v3 pool answers slot0 and liquidity and is
// priced through the quoter. Both are modelled as constant product, which is
// what lets the expected amounts below be worked out by hand.
type fakePool struct {
	addr       string
	t0, t1     string
	fee        int64
	v2         bool
	r0, r1     *big.Int
	sqrtP, liq *big.Int
	tick       int64
}

type fakeChain struct {
	pools []*fakePool
	calls int
}

func (f *fakeChain) find(addr string) *fakePool {
	for _, p := range f.pools {
		if strings.EqualFold(p.addr, addr) {
			return p
		}
	}
	return nil
}

// route resolves the quoter's (tokenIn, tokenOut, fee) to the v3 pool holding
// them, and reports which side is inbound.
func (f *fakeChain) route(in, out string, fee int64) (*fakePool, bool) {
	for _, p := range f.pools {
		if p.v2 || p.fee != fee {
			continue
		}
		if strings.EqualFold(p.t0, in) && strings.EqualFold(p.t1, out) {
			return p, true
		}
		if strings.EqualFold(p.t1, in) && strings.EqualFold(p.t0, out) {
			return p, false
		}
	}
	return nil, false
}

func hexWord(n *big.Int) string { return fmt.Sprintf("%064x", n) }

func (f *fakeChain) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqs []struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
			http.Error(w, "the quoter must send a JSON-RPC batch", http.StatusBadRequest)
			return
		}
		f.calls++
		out := make([]map[string]any, 0, len(reqs))
		for _, q := range reqs {
			entry := map[string]any{"jsonrpc": "2.0", "id": q.ID}
			if res, ok := f.answer(q.Method, q.Params); ok {
				entry["result"] = res
			} else {
				entry["error"] = map[string]any{"code": -32000, "message": "execution reverted"}
			}
			out = append(out, entry)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func (f *fakeChain) answer(method string, params []any) (string, bool) {
	if method == "eth_blockNumber" {
		return fmt.Sprintf("0x%x", fakeHead), true
	}
	if method != "eth_call" || len(params) == 0 {
		return "", false
	}
	call, ok := params[0].(map[string]any)
	if !ok {
		return "", false
	}
	to, _ := call["to"].(string)
	data, _ := call["data"].(string)
	if len(data) < 10 {
		return "", false
	}
	sel, args := data[:10], data[10:]

	if strings.EqualFold(to, fakeQuoter) {
		return f.quote(sel, args)
	}

	p := f.find(to)
	if p == nil {
		return "", false
	}
	switch sel {
	case selGetReserves:
		if !p.v2 {
			return "", false // a concentrated pool has no such function
		}
		return "0x" + hexWord(p.r0) + hexWord(p.r1) + hexWord(big.NewInt(0)), true
	case selSlot0:
		if p.v2 {
			return "", false
		}
		tick := new(big.Int).SetInt64(p.tick)
		if p.tick < 0 {
			tick.Add(tick, new(big.Int).Lsh(big.NewInt(1), 256))
		}
		return "0x" + hexWord(p.sqrtP) + hexWord(tick) + strings.Repeat(hexWord(big.NewInt(1)), 5), true
	case selLiquidity:
		if p.v2 {
			return "", false
		}
		return "0x" + hexWord(p.liq), true
	}
	return "", false
}

// quote is the fake QuoterV2: it prices through the pool the arguments name.
func (f *fakeChain) quote(sel, args string) (string, bool) {
	if len(args) < 5*wordHex {
		return "", false
	}
	word := func(i int) *big.Int {
		n, _ := new(big.Int).SetString(args[i*wordHex:(i+1)*wordHex], 16)
		return n
	}
	in := "0x" + args[wordHex-40:wordHex]
	out := "0x" + args[2*wordHex-40:2*wordHex]
	amount, fee := word(2), word(3).Int64()

	p, forward := f.route(in, out, fee)
	if p == nil {
		return "", false
	}
	rIn, rOut := p.r0, p.r1
	if !forward {
		rIn, rOut = p.r1, p.r0
	}

	var answer *big.Int
	switch sel {
	case selQuoteExactIn:
		answer = amountOut(amount, rIn, rOut, fee)
	case selQuoteExactOut:
		answer = amountIn(amount, rIn, rOut, fee)
	}
	if answer == nil {
		return "", false
	}
	// amount, price after, ticks crossed, gas — the quoter's four returns.
	return "0x" + hexWord(answer) + hexWord(p.sqrtP) + hexWord(big.NewInt(0)) + hexWord(big.NewInt(79990)), true
}

// ── the endpoint ──────────────────────────────────────────────────────────

func newTestQuoter(t *testing.T, chain *fakeChain) (*Quoter, func()) {
	t.Helper()
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(nil); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []struct {
		addr, symbol string
	}{{tokenA, "AAA"}, {tokenB, "BBB"}, {tokenC, "CCC"}} {
		store.SeedToken(tok.addr, &storage.SeedTokenData{Symbol: tok.symbol, Decimals: 18})
	}
	for _, p := range chain.pools {
		store.SeedPool(p.addr, &storage.SeedPoolData{Token0: p.t0, Token1: p.t1, FeeTier: p.fee})
	}
	srv := chain.serve()
	q := NewQuoter(store, 96369, srv.URL, fakeQuoter, tokenA)
	return q, func() { srv.Close(); store.Close() }
}

func ask(t *testing.T, q *Quoter, body string) (int, *quoteResponse, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/quote", strings.NewReader(body))
	HandleQuote(q)(rec, req)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
	}
	var parsed quoteResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	return rec.Code, &parsed, raw
}

// The validation the swap form already depends on, message for message.
func TestQuoteValidation(t *testing.T) {
	chain := &fakeChain{}
	q, done := newTestQuoter(t, chain)
	defer done()

	cases := []struct {
		name, body, detail string
	}{
		{"nothing at all", `{}`, "tokenIn must be an address"},
		{"only an input token", `{"tokenIn":"` + tokenA + `"}`, "tokenOut must be an address"},
		{"input is not an address", `{"tokenIn":"nope","tokenOut":"` + tokenC + `","amount":"1"}`, "tokenIn must be an address"},
		{"no amount", `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `"}`, "amount must be a decimal wei string"},
		{"a negative amount", `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"-5"}`, "amount must be a decimal wei string"},
		{"a fractional amount", `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"1.5"}`, "amount must be a decimal wei string"},
		{"nothing to swap", `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"0"}`, "amount must be > 0"},
		{"both sides the same", `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenA + `","amount":"1"}`, "tokenIn and tokenOut resolve to the same token"},
		// The sentinel routes as the wrapped token, so asking to swap the coin
		// for its own wrapper is the same trade twice.
		{"the coin for its wrapper", `{"tokenIn":"` + nativeSentinel + `","tokenOut":"` + tokenA + `","amount":"1"}`, "tokenIn and tokenOut resolve to the same token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, raw := ask(t, q, c.body)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", code)
			}
			if raw["errorCode"] != "VALIDATION_ERROR" || raw["detail"] != c.detail {
				t.Errorf("got %v / %v, want VALIDATION_ERROR / %q", raw["errorCode"], raw["detail"], c.detail)
			}
		})
	}
}

// A request naming a chain this process does not index is a question it cannot
// answer, and it says so rather than answering about a different chain.
func TestQuoteRefusesAnotherChain(t *testing.T) {
	q, done := newTestQuoter(t, &fakeChain{})
	defer done()
	body := `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"1","tokenInChainId":1}`
	code, _, raw := ask(t, q, body)
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
	if raw["errorCode"] != "QUOTE_ERROR" || raw["detail"] != "No quotes available" {
		t.Errorf("got %v / %v", raw["errorCode"], raw["detail"])
	}
}

// Nothing in the book reaches the token asked for. The answer is a well-formed
// quote with an empty route — no amount is offered, because there is none.
func TestQuoteWithNoRouteOffersNoAmount(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAB3, t0: tokenA, t1: tokenB, fee: 3000, r0: tokens(1000), r1: tokens(1000),
			sqrtP: big.NewInt(1), liq: tokens(1)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	code, res, raw := ask(t, q, `{"tokenIn":"`+tokenA+`","tokenOut":"`+tokenC+`","amount":"1000000000000000000"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a missing route is not an error", code)
	}
	if len(res.Quote.Route) != 0 {
		t.Errorf("route = %v, want empty", res.Quote.Route)
	}
	if res.Quote.Output.Amount != "0" || res.Quote.Input.Amount != "0" {
		t.Errorf("amounts = %s/%s, want 0/0", res.Quote.Input.Amount, res.Quote.Output.Amount)
	}
	if res.Quote.BlockNumber != "0" || res.Quote.PriceImpact != 0 {
		t.Errorf("a route that does not exist was read at no block: %+v", res.Quote)
	}
	if res.Routing != "CLASSIC" {
		t.Errorf("routing = %q", res.Routing)
	}
	// The key must be present and null, not absent: the client reads it.
	if v, ok := raw["permitData"]; !ok || v != nil {
		t.Errorf("permitData = %v, present=%v; want an explicit null", v, ok)
	}
}

// A constant-product pair, priced here rather than asked of any contract.
func TestQuoteThroughAConstantProductPair(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC2, t0: tokenA, t1: tokenC, fee: 3000, v2: true, r0: tokens(100), r1: tokens(100)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	code, res, _ := ask(t, q, `{"tokenIn":"`+tokenA+`","tokenOut":"`+tokenC+`","amount":"1000000000000000000"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got := res.Quote.Output.Amount; got != "987158034397061298" {
		t.Errorf("amountOut = %s, want 987158034397061298", got)
	}
	if got := res.Quote.Input.Amount; got != "1000000000000000000" {
		t.Errorf("amountIn = %s", got)
	}
	if len(res.Quote.Route) != 1 || len(res.Quote.Route[0]) != 1 {
		t.Fatalf("route = %+v, want one hop", res.Quote.Route)
	}
	hop := res.Quote.Route[0][0]
	if hop.Type != "v2-pool" {
		t.Errorf("hop type = %q, want v2-pool", hop.Type)
	}
	if hop.Reserve0 == nil || hop.Reserve0.Quotient != tokens(100).String() {
		t.Errorf("reserve0 = %+v", hop.Reserve0)
	}
	if hop.Reserve1 == nil || hop.Reserve1.Quotient != tokens(100).String() {
		t.Errorf("reserve1 = %+v", hop.Reserve1)
	}
	if hop.SqrtRatioX96 != "" || hop.Liquidity != "" {
		t.Errorf("a constant-product pair has no sqrt price or concentrated liquidity: %+v", hop)
	}
	if res.Quote.RouteString != "AAA -> CCC" {
		t.Errorf("routeString = %q", res.Quote.RouteString)
	}
	if res.Quote.BlockNumber != "1158221" {
		t.Errorf("blockNumber = %q, want the head it was read at", res.Quote.BlockNumber)
	}
	if got := res.Quote.PriceImpact; got < 0.98617 || got > 0.98618 {
		t.Errorf("priceImpact = %v, want ≈0.986171", got)
	}
	if res.Quote.GasUseEstimate != "104000" {
		t.Errorf("gasUseEstimate = %q", res.Quote.GasUseEstimate)
	}
}

func TestQuoteExactOutputThroughAPair(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC2, t0: tokenA, t1: tokenC, fee: 3000, v2: true, r0: tokens(100), r1: tokens(100)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	body := `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"1000000000000000000","type":"EXACT_OUTPUT"}`
	code, res, _ := ask(t, q, body)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if res.Quote.TradeType != "EXACT_OUTPUT" {
		t.Errorf("tradeType = %q", res.Quote.TradeType)
	}
	if got := res.Quote.Input.Amount; got != "1013140431395195689" {
		t.Errorf("amountIn = %s, want 1013140431395195689", got)
	}
	if got := res.Quote.Output.Amount; got != "1000000000000000000" {
		t.Errorf("amountOut = %s, want exactly what was asked for", got)
	}
}

// The two-hop route pays more than the direct one, and the search finds it
// without anyone naming B as a hub.
func TestQuoteTakesTheBetterTwoHopRoute(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC3, t0: tokenA, t1: tokenC, fee: 3000, r0: tokens(1000), r1: tokens(1000),
			sqrtP: bigOf("79228162514264337593543950336"), liq: tokens(1), tick: 0},
		{addr: poolAB3, t0: tokenA, t1: tokenB, fee: 3000, r0: tokens(1000), r1: tokens(4000),
			sqrtP: bigOf("158456325028528675187087900672"), liq: tokens(2), tick: 13863},
		{addr: poolBC3, t0: tokenB, t1: tokenC, fee: 3000, r0: tokens(4000), r1: tokens(2000),
			sqrtP: bigOf("56022770974786139918731938227"), liq: tokens(3), tick: -6932},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	code, res, _ := ask(t, q, `{"tokenIn":"`+tokenA+`","tokenOut":"`+tokenC+`","amount":"1000000000000000000"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// Direct pays 996006981039903216; through B it is 1984067703346028726.
	if got := res.Quote.Output.Amount; got != "1984067703346028726" {
		t.Errorf("amountOut = %s, want the two-hop 1984067703346028726", got)
	}
	if res.Quote.RouteString != "AAA -> BBB -> CCC" {
		t.Errorf("routeString = %q, want the route through B", res.Quote.RouteString)
	}
	if len(res.Quote.Route[0]) != 2 {
		t.Fatalf("hops = %d, want 2", len(res.Quote.Route[0]))
	}
	first, second := res.Quote.Route[0][0], res.Quote.Route[0][1]
	if first.AmountOut != second.AmountIn {
		t.Errorf("the first hop pays %s but the second is fed %s", first.AmountOut, second.AmountIn)
	}
	if first.Type != "v3-pool" || first.Fee != "3000" {
		t.Errorf("hop = %+v", first)
	}
	// The pool state echoed back is what the pool reported, not a placeholder.
	if first.Liquidity != tokens(2).String() || first.TickCurrent != "13863" {
		t.Errorf("hop state = liquidity %s tick %s", first.Liquidity, first.TickCurrent)
	}
	if second.TickCurrent != "-6932" {
		t.Errorf("a negative tick must survive the round trip, got %s", second.TickCurrent)
	}
	if res.Quote.GasUseEstimate != "159980" {
		t.Errorf("gasUseEstimate = %q, want both legs' cost", res.Quote.GasUseEstimate)
	}
	if got := res.Quote.PriceImpact; got < 0.19857 || got > 0.19859 {
		t.Errorf("priceImpact = %v, want ≈0.198581", got)
	}
}

func TestQuoteExactOutputTakesTheCheaperTwoHopRoute(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC3, t0: tokenA, t1: tokenC, fee: 3000, r0: tokens(1000), r1: tokens(1000),
			sqrtP: big.NewInt(1), liq: tokens(1)},
		{addr: poolAB3, t0: tokenA, t1: tokenB, fee: 3000, r0: tokens(1000), r1: tokens(4000),
			sqrtP: big.NewInt(1), liq: tokens(2)},
		{addr: poolBC3, t0: tokenB, t1: tokenC, fee: 3000, r0: tokens(4000), r1: tokens(2000),
			sqrtP: big.NewInt(1), liq: tokens(3)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	body := `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"1000000000000000000","type":"EXACT_OUTPUT"}`
	code, res, _ := ask(t, q, body)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// Direct costs 1004013040121365097; through B it is 503517829582206318.
	if got := res.Quote.Input.Amount; got != "503517829582206318" {
		t.Errorf("amountIn = %s, want the cheaper two-hop 503517829582206318", got)
	}
	if res.Quote.RouteString != "AAA -> BBB -> CCC" {
		t.Errorf("routeString = %q", res.Quote.RouteString)
	}
}

// The coin routes through its wrapper's pools, and comes back described as the
// coin — the wrapping is this service's business, not the caller's.
func TestQuoteRoutesTheNativeCoinThroughItsWrapper(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC2, t0: tokenA, t1: tokenC, fee: 3000, v2: true, r0: tokens(100), r1: tokens(100)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	code, res, _ := ask(t, q, `{"tokenIn":"`+nativeSentinel+`","tokenOut":"`+tokenC+`","amount":"1000000000000000000"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got := res.Quote.Output.Amount; got != "987158034397061298" {
		t.Errorf("amountOut = %s — the coin should price exactly as its wrapper does", got)
	}
	if res.Quote.Input.Token != nativeSentinel {
		t.Errorf("input token = %s, want the sentinel echoed back", res.Quote.Input.Token)
	}
	if res.Quote.Route[0][0].TokenIn.Address != nativeSentinel {
		t.Errorf("the route's first hop should open at the coin, got %s", res.Quote.Route[0][0].TokenIn.Address)
	}
}

// Addresses go out EIP-55 checksummed, and the swapper and slippage the caller
// sent come back on the quote.
func TestQuoteEchoesTheCaller(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC2, t0: tokenA, t1: tokenC, fee: 3000, v2: true, r0: tokens(100), r1: tokens(100)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	body := `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"1000000000000000000",` +
		`"swapper":"0x9011e888251ab053b7bd1cdb598db4f9ded94714","slippageTolerance":1.25}`
	code, res, _ := ask(t, q, body)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if want := "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"; res.Quote.Swapper != want {
		t.Errorf("swapper = %s, want %s", res.Quote.Swapper, want)
	}
	if res.Quote.Output.Recipient != res.Quote.Swapper {
		t.Errorf("recipient = %s, want the swapper", res.Quote.Output.Recipient)
	}
	if res.Quote.Slippage != 1.25 {
		t.Errorf("slippage = %v, want 1.25", res.Quote.Slippage)
	}
	if res.Quote.ChainID != 96369 {
		t.Errorf("chainId = %d", res.Quote.ChainID)
	}
	if res.Quote.Input.Token != checksumAddress(tokenA) {
		t.Errorf("input token = %s, want it checksummed", res.Quote.Input.Token)
	}
}

// A pool the node will not describe is left out of the search rather than
// guessed at. The book still holds it; nothing prices it.
func TestQuoteSkipsAPoolThatAnswersNeitherVenue(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		// v2:false means getReserves reverts, and no sqrtP/liq means slot0 is
		// answered — so make it answer neither by leaving it out of the fake's
		// pool list while the store still knows about it.
	}}
	q, done := newTestQuoter(t, chain)
	defer done()
	q.store.SeedPool(poolAC2, &storage.SeedPoolData{Token0: tokenA, Token1: tokenC, FeeTier: 3000})

	code, res, _ := ask(t, q, `{"tokenIn":"`+tokenA+`","tokenOut":"`+tokenC+`","amount":"1000000000000000000"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(res.Quote.Route) != 0 || res.Quote.Output.Amount != "0" {
		t.Errorf("a pool that named no venue was quoted anyway: %+v", res.Quote)
	}
}

// A venue never changes, so it is settled once and not re-asked on every quote.
func TestVenueIsAskedOnce(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC2, t0: tokenA, t1: tokenC, fee: 3000, v2: true, r0: tokens(100), r1: tokens(100)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	body := `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"1000000000000000000"}`
	ask(t, q, body)
	first := chain.calls
	ask(t, q, body)
	second := chain.calls - first
	if second >= first {
		t.Errorf("second quote made %d round trips against the first's %d — the venue was asked again", second, first)
	}
}

// Without a quoter there is no way to price a concentrated pool, so it is not
// priced. Constant-product pairs are unaffected: their arithmetic is here.
func TestConcentratedPoolsGoUnquotedWithoutAQuoter(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC3, t0: tokenA, t1: tokenC, fee: 3000, r0: tokens(1000), r1: tokens(1000),
			sqrtP: big.NewInt(1), liq: tokens(1)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()
	q.quoterV2 = "" // the chain has no V3 periphery deployed

	code, res, _ := ask(t, q, `{"tokenIn":"`+tokenA+`","tokenOut":"`+tokenC+`","amount":"1000000000000000000"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(res.Quote.Route) != 0 || res.Quote.Output.Amount != "0" {
		t.Errorf("a pool nothing could price reported an amount anyway: %+v", res.Quote)
	}
}

// An exact-output quote asks the pool a question whose answer — the input the
// trade needs — is not known when the marginal reference is sent. The rate that
// comes back must be divided by the size actually asked about, not by the input
// that was learned afterwards. A trade smaller than the reference is marginal by
// definition and moves no price.
func TestPriceImpactOfATinyExactOutputTrade(t *testing.T) {
	chain := &fakeChain{pools: []*fakePool{
		{addr: poolAC3, t0: tokenA, t1: tokenC, fee: 3000, r0: tokens(1000), r1: tokens(1000),
			sqrtP: bigOf("79228162514264337593543950336"), liq: tokens(1)},
	}}
	q, done := newTestQuoter(t, chain)
	defer done()

	// A millionth of a token out of a thousand-token pool: the input lands well
	// below the 1e15 reference, and well above the base unit where the exact-in
	// rounding-up would itself show as a fraction of a percent.
	body := `{"tokenIn":"` + tokenA + `","tokenOut":"` + tokenC + `","amount":"1000000000000","type":"EXACT_OUTPUT"}`
	code, res, _ := ask(t, q, body)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if res.Quote.Input.Amount == "0" || res.Quote.Input.Amount == "" {
		t.Fatalf("no input quoted: %+v", res.Quote)
	}
	if got := res.Quote.PriceImpact; got > 0.01 {
		t.Errorf("priceImpact = %v for a dust trade, want ~0 — the marginal rate was "+
			"divided by the wrong reference size", got)
	}
}
