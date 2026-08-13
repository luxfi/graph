package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/graph/storage"
	"golang.org/x/crypto/sha3"
)

// What a swap would pay out, over the pools this process indexes.
//
// It was served by exchange-api, the last TypeScript service in front of this
// one. The shape below is that service's, because the swap form reads it today
// and a client is not something to break while moving a service.
//
// TWO QUESTIONS, TWO ANSWERERS
//
// A quote is really two questions, and they have different authorities:
//
//   - "Which pools could carry this trade?" — the index answers. That is the
//     whole reason the endpoint belongs in graphd: the book is already here,
//     with every pool, its pair and its fee, so a route can be searched over
//     the real graph instead of a hardcoded hub list. The service being retired
//     could only try WLUX and LUSD as intermediaries; this tries every token
//     the book says both sides connect to.
//
//   - "What does that pool actually pay?" — the pool answers. For a V2 pair
//     that is constant product over its two reserves, and it is computed here,
//     exactly, in integers. For a V3 pool it is a walk across whatever ticks
//     the trade crosses, and the pool's own quoter is the only thing holding
//     the tick map, so that is what is asked.
//
// WHY V3 IS NOT COMPUTED HERE
//
// The tempting shortcut is to treat a V3 pool as constant-liquidity around its
// current price: slot0 gives sqrtPriceX96, liquidity() gives L, and one step of
// the standard formula gives an amount out. Measured against the pool on Lux
// mainnet that shortcut is exact while a trade stays inside the current tick and
// badly wrong the moment it does not — 15% high on CYRUS/LUSD, 44% on Z/LUSD,
// 89% on TRUMP/LUSD, always in the direction of promising output the pool cannot
// pay. Those are the pools whose liquidity is parked away from spot, which is
// most of a thin book. Closing that gap means the tick map, and the tick map is
// not indexed here: the Mint/Burn arm of processLog is deliberately inert
// because reserves are state, not history (see valuation.go's header). So the
// choice is between a number that is wrong by a factor and the pool's own
// arithmetic. The pool's own arithmetic is right.
//
// STALENESS
//
// Every amount below is read at head, per request, not from the indexed
// aggregates — those are recomputed on a 30s pass, and a quote a person is
// about to sign should not be half a minute old. blockNumber says which block
// the answer came from.

const (
	// nativeSentinel is the zero address standing for the chain's own coin. It
	// routes as the wrapped token and echoes back to the caller unwrapped.
	nativeSentinel = "0x0000000000000000000000000000000000000000"

	// marginalRef is the reference trade the price impact is measured against:
	// small enough that it moves no price, so it reports the rate a trade would
	// get if it were free of its own weight. Impact is the gap between that and
	// what the real size gets.
	marginalRef = 1_000_000_000_000_000 // 1e15 base units

	// v2SwapGas is what a constant-product swap costs to execute. Unlike a V3
	// leg, whose quoter measures the walk it just did, a V2 pair has nothing to
	// ask — so this is a figure for the transfer-and-settle a pair does, not a
	// reading. It exists because the field is an estimate and a route that
	// reported nothing would read as free.
	v2SwapGas = 104_000

	// maxRoutes bounds the search. The book on Lux mainnet is 32 pools and every
	// candidate is priced with a live call, so this is a guard against a chain
	// with thousands of pools, not a view about how many routes are worth trying.
	maxRoutes = 24

	// quoteBudget bounds one request's conversation with the node.
	quoteBudget = 20 * time.Second
)

// selector is the first four bytes of a function signature's hash — how the EVM
// names a method. Deriving them from the signatures keeps the ABI readable
// rather than a column of magic hex.
func selector(sig string) string {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(sig))
	return "0x" + fmt.Sprintf("%x", h.Sum(nil)[:4])
}

var (
	selQuoteExactIn  = selector("quoteExactInputSingle((address,address,uint256,uint24,uint160))")
	selQuoteExactOut = selector("quoteExactOutputSingle((address,address,uint256,uint24,uint160))")
	selGetReserves   = selector("getReserves()")
	selSlot0         = selector("slot0()")
	selLiquidity     = selector("liquidity()")
)

// ── the wire ──────────────────────────────────────────────────────────────

type quoteRequest struct {
	TokenIn           string   `json:"tokenIn"`
	TokenOut          string   `json:"tokenOut"`
	Amount            string   `json:"amount"`
	Type              string   `json:"type"`
	Swapper           string   `json:"swapper"`
	SlippageTolerance *float64 `json:"slippageTolerance"`
	TokenInChainID    *int64   `json:"tokenInChainId"`
	TokenOutChainID   *int64   `json:"tokenOutChainId"`
}

type quoteResponse struct {
	RequestID  string       `json:"requestId"`
	Routing    string       `json:"routing"`
	PermitData any          `json:"permitData"`
	Quote      classicQuote `json:"quote"`
}

type classicQuote struct {
	ChainID        int64       `json:"chainId"`
	Swapper        string      `json:"swapper"`
	Input          quoteSide   `json:"input"`
	Output         quoteSide   `json:"output"`
	TradeType      string      `json:"tradeType"`
	Slippage       float64     `json:"slippage"`
	GasUseEstimate string      `json:"gasUseEstimate"`
	GasFee         string      `json:"gasFee"`
	Route          [][]hopJSON `json:"route"`
	RouteString    string      `json:"routeString"`
	QuoteID        string      `json:"quoteId"`
	BlockNumber    string      `json:"blockNumber"`
	PriceImpact    float64     `json:"priceImpact"`
}

type quoteSide struct {
	Token     string `json:"token"`
	Amount    string `json:"amount"`
	Recipient string `json:"recipient,omitempty"`
}

// hopJSON is one pool in a route. A V3 hop carries the pool's price state, a V2
// hop its two reserves; the swap form reads both shapes and keys on `type`.
type hopJSON struct {
	Type         string      `json:"type"`
	Address      string      `json:"address"`
	TokenIn      tokenJSON   `json:"tokenIn"`
	TokenOut     tokenJSON   `json:"tokenOut"`
	SqrtRatioX96 string      `json:"sqrtRatioX96,omitempty"`
	Liquidity    string      `json:"liquidity,omitempty"`
	TickCurrent  string      `json:"tickCurrent,omitempty"`
	Fee          string      `json:"fee,omitempty"`
	Reserve0     *reserveRef `json:"reserve0,omitempty"`
	Reserve1     *reserveRef `json:"reserve1,omitempty"`
	AmountIn     string      `json:"amountIn"`
	AmountOut    string      `json:"amountOut"`
}

// tokenJSON renders decimals as text. The token list renders the same field as a
// number; both are exchange-api's, and the swap form parses each where it finds it.
type tokenJSON struct {
	Address  string `json:"address"`
	ChainID  int64  `json:"chainId"`
	Symbol   string `json:"symbol"`
	Decimals string `json:"decimals"`
}

type reserveRef struct {
	Token    tokenJSON `json:"token"`
	Quotient string    `json:"quotient"`
}

// ── the book ──────────────────────────────────────────────────────────────

// pool is one indexed venue: its address, its pair, and the fee it charges.
type pool struct {
	addr   string // lower-cased
	t0, t1 string // lower-cased
	fee    int64
	venue  string // "v2" or "v3", learned from the chain; empty until then
}

// other returns the far side of a pool from the token given, and whether the
// token is on it at all.
func (p *pool) other(token string) (string, bool) {
	switch token {
	case p.t0:
		return p.t1, true
	case p.t1:
		return p.t0, true
	}
	return "", false
}

// leg is one pool crossed by a route, with whichever amount is known so far.
type leg struct {
	p       *pool
	in, out string
	amtIn   *big.Int
	amtOut  *big.Int
	gas     int64

	// what the pool looked like when it answered, echoed to the client
	sqrtP, liq *big.Int
	tick       int64
	r0, r1     *big.Int

	// marginal is the rate a negligible trade would get through this pool,
	// which is the baseline price impact is measured from. refIn is the size
	// that rate was measured at, kept because the rate is a quotient and the
	// divisor has to be the size actually asked about — for an exact output the
	// leg's own input is not known until after the question has been sent.
	marginal float64
	refIn    *big.Int
}

// route is an ordered chain of legs from the caller's token to the one wanted.
type route struct {
	legs []*leg
}

func (r *route) amountIn() *big.Int  { return r.legs[0].amtIn }
func (r *route) amountOut() *big.Int { return r.legs[len(r.legs)-1].amtOut }

func (r *route) gas() int64 {
	var g int64
	for _, l := range r.legs {
		g += l.gas
	}
	return g
}

// priced reports whether every leg answered. A route with a silent leg is not a
// worse route, it is not a route.
func (r *route) priced() bool {
	for _, l := range r.legs {
		if l.amtIn == nil || l.amtOut == nil || l.amtIn.Sign() <= 0 || l.amtOut.Sign() <= 0 {
			return false
		}
	}
	return true
}

// ── the quoter ────────────────────────────────────────────────────────────

// Quoter prices a swap over the indexed book.
type Quoter struct {
	store    *storage.Store
	chainID  int64
	node     *node
	quoterV2 string // the V3 periphery quoter; empty when the chain has no V3 venue
	wrapped  string // wrapped native, lower-cased

	mu     sync.Mutex
	venues map[string]string // pool address → "v2"/"v3"; a pool's venue never changes
}

// NewQuoter builds the quote handler over the same store the GraphQL engine
// reads. Exported because the process that serves production is the explorer,
// not graphd, and a handler in package main is reachable by neither.
func NewQuoter(store *storage.Store, chainID int64, rpc, quoterV2, wrapped string) *Quoter {
	return &Quoter{
		store:    store,
		chainID:  chainID,
		node:     dial(rpc),
		quoterV2: strings.ToLower(strings.TrimSpace(quoterV2)),
		wrapped:  strings.ToLower(strings.TrimSpace(wrapped)),
		venues:   map[string]string{},
	}
}

// HandleQuote is the swap form's price endpoint, over the same book.
func HandleQuote(q *Quoter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req quoteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badQuote(w, "body must be JSON")
			return
		}
		q.serve(r.Context(), w, &req)
	}
}

func badQuote(w http.ResponseWriter, detail string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"errorCode": "VALIDATION_ERROR",
		"detail":    detail,
	})
}

func (q *Quoter) serve(ctx context.Context, w http.ResponseWriter, req *quoteRequest) {
	// EXACT_INPUT is the default because it is the question the swap form asks
	// almost always: here is what I have, what do I get.
	tradeType := "EXACT_INPUT"
	if req.Type == "EXACT_OUTPUT" {
		tradeType = "EXACT_OUTPUT"
	}

	if !isHexAddress(req.TokenIn) {
		badQuote(w, "tokenIn must be an address")
		return
	}
	if !isHexAddress(req.TokenOut) {
		badQuote(w, "tokenOut must be an address")
		return
	}
	amount, ok := parseWei(req.Amount)
	if !ok {
		badQuote(w, "amount must be a decimal wei string")
		return
	}
	if amount.Sign() <= 0 {
		badQuote(w, "amount must be > 0")
		return
	}

	// The caller may say which chain it believes it is trading on. This process
	// indexes one chain, so a request naming another is a question it cannot
	// answer rather than one it should answer wrongly.
	for _, id := range []*int64{req.TokenInChainID, req.TokenOutChainID} {
		if id != nil && *id != q.chainID {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"errorCode": "QUOTE_ERROR",
				"detail":    "No quotes available",
			})
			return
		}
	}

	swapper := nativeSentinel
	if isHexAddress(req.Swapper) {
		swapper = checksumAddress(req.Swapper)
	}
	slippage := 0.5
	if req.SlippageTolerance != nil {
		slippage = *req.SlippageTolerance
	}

	// Native routes as wrapped: the pools hold the wrapped token, and a swap
	// from the coin wraps on the way in.
	in, out := q.unwrapNative(req.TokenIn), q.unwrapNative(req.TokenOut)
	if in == out {
		badQuote(w, "tokenIn and tokenOut resolve to the same token")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, quoteBudget)
	defer cancel()

	quote := noRoute(q.chainID, req, tradeType, swapper, slippage)
	if best := q.best(ctx, in, out, amount, tradeType); best != nil {
		head := q.decorate(ctx, best)
		quote = q.render(req, best, tradeType, swapper, slippage, head)
	}
	writeJSON(w, http.StatusOK, quoteResponse{
		RequestID: requestID(),
		Routing:   "CLASSIC",
		Quote:     quote,
	})
}

// noRoute is the answer when nothing in the book can carry the trade.
//
// It is a 200 with an empty route rather than an error because that is what the
// swap form is built to read: it treats `route: []` as a quote that found
// nothing and shows no price, whereas a 404 makes it retry. The amounts are
// zero — there is no route, so there is no number, and zero here is the absence
// the empty route already states rather than a figure anyone should trade on.
func noRoute(chainID int64, req *quoteRequest, tradeType, swapper string, slippage float64) classicQuote {
	return classicQuote{
		ChainID:        chainID,
		Swapper:        swapper,
		Input:          quoteSide{Token: echoToken(req.TokenIn), Amount: "0"},
		Output:         quoteSide{Token: echoToken(req.TokenOut), Amount: "0", Recipient: swapper},
		TradeType:      tradeType,
		Slippage:       slippage,
		GasUseEstimate: "0",
		GasFee:         "0",
		Route:          [][]hopJSON{},
		RouteString:    "",
		QuoteID:        requestID(),
		BlockNumber:    "0",
		PriceImpact:    0,
	}
}

// ── routing ───────────────────────────────────────────────────────────────

// best searches the indexed book for the route that pays most (or, for an exact
// output, costs least) and returns it fully priced, or nil when none is.
func (q *Quoter) best(ctx context.Context, in, out string, amount *big.Int, tradeType string) *route {
	pools := q.book()
	if len(pools) == 0 {
		return nil
	}
	if err := q.classify(ctx, pools); err != nil {
		return nil
	}

	candidates := routes(pools, in, out)
	if len(candidates) == 0 {
		return nil
	}

	// Seed each candidate with the amount the caller fixed — the first leg's
	// input for an exact input, the last leg's output for an exact output — and
	// price outward from there. Two passes cover a two-hop route: one for the
	// leg whose amount the caller gave, one for the leg the first pass fed.
	exactIn := tradeType == "EXACT_INPUT"
	for _, c := range candidates {
		if exactIn {
			c.legs[0].amtIn = amount
		} else {
			c.legs[len(c.legs)-1].amtOut = amount
		}
	}
	for pass := 0; pass < 2; pass++ {
		if err := q.priceReady(ctx, candidates, exactIn); err != nil {
			return nil
		}
	}

	var winner *route
	for _, c := range candidates {
		if !c.priced() {
			continue
		}
		switch {
		case winner == nil:
			winner = c
		case exactIn && c.amountOut().Cmp(winner.amountOut()) > 0:
			winner = c
		case !exactIn && c.amountIn().Cmp(winner.amountIn()) < 0:
			winner = c
		}
	}
	return winner
}

// book reads every indexed pool into the routing graph. A pool with a malformed
// address or a missing side cannot be crossed and is left out.
func (q *Quoter) book() []*pool {
	raw := q.store.PoolsRaw()
	out := make([]*pool, 0, len(raw))
	for addr, p := range raw {
		if p == nil || !isHexAddress(addr) || !isHexAddress(p.Token0) || !isHexAddress(p.Token1) {
			continue
		}
		t0, t1 := strings.ToLower(p.Token0), strings.ToLower(p.Token1)
		if t0 == t1 {
			continue
		}
		out = append(out, &pool{addr: strings.ToLower(addr), t0: t0, t1: t1, fee: p.FeeTier})
	}
	// A stable order so the same book searched twice tries routes in the same
	// order, and a tie between two equal quotes resolves the same way each time.
	sort.Slice(out, func(i, j int) bool { return out[i].addr < out[j].addr })
	return out
}

// routes enumerates the ways the book connects in to out: every pool holding
// both, then every pair of pools meeting at a token in the middle.
//
// The middle token is discovered rather than configured. exchange-api tried two
// hardcoded hubs, which is exactly as much of the graph as someone remembered to
// list; the index knows the whole graph, so a pair that only meets through some
// third token is reachable here without anyone adding it to a list.
func routes(pools []*pool, in, out string) []*route {
	var direct, hops []*route
	byToken := map[string][]*pool{}
	for _, p := range pools {
		if (p.t0 == in && p.t1 == out) || (p.t0 == out && p.t1 == in) {
			direct = append(direct, &route{legs: []*leg{{p: p, in: in, out: out}}})
			continue
		}
		byToken[p.t0] = append(byToken[p.t0], p)
		byToken[p.t1] = append(byToken[p.t1], p)
	}

	// Rank middles by how much of the book meets there. Depth is not knowable
	// without asking every pool, but a token many pools hold is the one likeliest
	// to carry a trade, and the cap has to fall somewhere.
	type mid struct {
		token string
		first *pool
		last  *pool
		deg   int
	}
	var mids []mid
	for _, first := range byToken[in] {
		m, ok := first.other(in)
		if !ok || m == out || m == in {
			continue
		}
		for _, last := range byToken[m] {
			if last == first {
				continue
			}
			if far, ok := last.other(m); !ok || far != out {
				continue
			}
			mids = append(mids, mid{token: m, first: first, last: last, deg: len(byToken[m])})
		}
	}
	sort.SliceStable(mids, func(i, j int) bool {
		if mids[i].deg != mids[j].deg {
			return mids[i].deg > mids[j].deg
		}
		return mids[i].first.addr < mids[j].first.addr
	})
	for _, m := range mids {
		hops = append(hops, &route{legs: []*leg{
			{p: m.first, in: in, out: m.token},
			{p: m.last, in: m.token, out: out},
		}})
	}

	all := append(direct, hops...)
	if len(all) > maxRoutes {
		all = all[:maxRoutes]
	}
	return all
}

// ── pricing ───────────────────────────────────────────────────────────────

// classify learns which venue each pool is, by asking it. The store cannot say:
// a V2 pair and a V3 pool are the same row there, and the pair's fee is recorded
// as 3000 whether or not that is what it charges. So the pool is asked directly
// — getReserves answers on a pair, slot0 on a pool, and the one that answers is
// the one it is. A venue never changes, so this is asked once per pool.
func (q *Quoter) classify(ctx context.Context, pools []*pool) error {
	var unknown []*pool
	q.mu.Lock()
	for _, p := range pools {
		if v, ok := q.venues[p.addr]; ok {
			p.venue = v
			continue
		}
		unknown = append(unknown, p)
	}
	q.mu.Unlock()
	if len(unknown) == 0 {
		return nil
	}

	calls := make([]ethCall, 0, len(unknown)*2)
	for _, p := range unknown {
		calls = append(calls,
			ethCall{To: p.addr, Data: selGetReserves},
			ethCall{To: p.addr, Data: selSlot0})
	}
	res, err := q.node.call(ctx, calls)
	if err != nil {
		return err // the node did not answer; nothing is learned, nothing recorded
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	for i, p := range unknown {
		reserves, slot0 := res[i*2], res[i*2+1]
		switch {
		case len(reserves) >= 3*wordHex:
			p.venue = "v2"
		case len(slot0) >= 2*wordHex:
			p.venue = "v3"
		default:
			continue // answered neither; leave it unknown rather than guess
		}
		q.venues[p.addr] = p.venue
	}
	return nil
}

// priceReady prices every leg whose input amount is now known, in one batch, and
// carries each answer to the leg that follows it.
func (q *Quoter) priceReady(ctx context.Context, candidates []*route, exactIn bool) error {
	var ready []*leg
	for _, c := range candidates {
		for _, l := range c.legs {
			if l.done() {
				continue
			}
			if exactIn && l.amtIn != nil {
				ready = append(ready, l)
			} else if !exactIn && l.amtOut != nil {
				ready = append(ready, l)
			}
		}
	}
	if len(ready) == 0 {
		return nil
	}

	// Each leg asks for two things: what it pays, and what a negligible trade
	// would pay. The second is the baseline for price impact, and asking for it
	// here rides the batch that is going out anyway.
	var calls []ethCall
	index := make([][2]int, len(ready))
	for i, l := range ready {
		c := q.legCalls(l, exactIn)
		index[i] = [2]int{len(calls), len(c)}
		calls = append(calls, c...)
	}
	res, err := q.node.call(ctx, calls)
	if err != nil {
		return err
	}
	for i, l := range ready {
		at, n := index[i][0], index[i][1]
		l.absorb(res[at:at+n], exactIn)
	}

	// Feed each answer to the next leg along.
	for _, c := range candidates {
		for i := 0; i+1 < len(c.legs); i++ {
			a, b := c.legs[i], c.legs[i+1]
			if exactIn && b.amtIn == nil && a.amtOut != nil {
				b.amtIn = a.amtOut
			}
			if !exactIn && a.amtOut == nil && b.amtIn != nil {
				a.amtOut = b.amtIn
			}
		}
	}
	return nil
}

// done reports whether a leg has both its amounts and needs no further asking.
func (l *leg) done() bool { return l.amtIn != nil && l.amtOut != nil }

// legCalls builds the calls one leg needs: the quote itself, then the marginal
// reference. A V2 pair needs only its reserves — both numbers fall out of the
// same two integers, computed here.
func (q *Quoter) legCalls(l *leg, exactIn bool) []ethCall {
	if l.p.venue == "v2" {
		return []ethCall{{To: l.p.addr, Data: selGetReserves}}
	}
	if q.quoterV2 == "" {
		return nil
	}
	sel := selQuoteExactIn
	amount := l.amtIn
	if !exactIn {
		sel, amount = selQuoteExactOut, l.amtOut
	}
	if amount == nil {
		return nil
	}
	quote := ethCall{To: q.quoterV2, Data: sel +
		addrWord(l.in) + addrWord(l.out) + numWord(amount) + numWord(big.NewInt(l.p.fee)) + numWord(zero)}

	// The marginal reference is always an exact-input quote, whichever direction
	// the trade was asked in: it is a rate, and a rate reads the same either way.
	l.refIn = marginalSize(l.amtIn)
	refCall := ethCall{To: q.quoterV2, Data: selQuoteExactIn +
		addrWord(l.in) + addrWord(l.out) + numWord(l.refIn) + numWord(big.NewInt(l.p.fee)) + numWord(zero)}
	return []ethCall{quote, refCall}
}

// marginalSize is the trade the marginal rate is measured at: small enough not
// to move the price, and never larger than the trade being quoted, since a
// reference bigger than the trade would report the trade as an improvement.
func marginalSize(amtIn *big.Int) *big.Int {
	ref := big.NewInt(marginalRef)
	if amtIn != nil && amtIn.Sign() > 0 && amtIn.Cmp(ref) < 0 {
		return amtIn
	}
	return ref
}

// absorb reads the answers legCalls asked for onto the leg.
func (l *leg) absorb(res []string, exactIn bool) {
	if len(res) == 0 {
		return
	}
	if l.p.venue == "v2" {
		w := words(res[0])
		if len(w) < 2 {
			return
		}
		l.r0, l.r1 = w[0], w[1]
		rIn, rOut := l.r0, l.r1
		if l.in == l.p.t1 {
			rIn, rOut = l.r1, l.r0
		}
		if exactIn {
			l.amtOut = amountOut(l.amtIn, rIn, rOut, l.p.fee)
		} else {
			l.amtIn = amountIn(l.amtOut, rIn, rOut, l.p.fee)
		}
		if l.amtIn == nil || l.amtOut == nil {
			return
		}
		l.gas = v2SwapGas
		// A negligible trade through a constant product pays the pool ratio less
		// the fee, and both numbers are already here — no second question.
		l.refIn = marginalSize(l.amtIn)
		if ref := amountOut(l.refIn, rIn, rOut, l.p.fee); ref != nil {
			l.marginal = ratio(ref, l.refIn)
		}
		return
	}

	// A V3 quote answers with the amount, the price it left the pool at, how many
	// initialised ticks it crossed getting there, and what that walk cost.
	w := words(res[0])
	if len(w) < 4 {
		return
	}
	if exactIn {
		l.amtOut = w[0]
	} else {
		l.amtIn = w[0]
	}
	l.gas = w[3].Int64()
	if len(res) > 1 && l.refIn != nil {
		if rw := words(res[1]); len(rw) >= 1 {
			l.marginal = ratio(rw[0], l.refIn)
		}
	}
}

// ── constant product ──────────────────────────────────────────────────────
//
// The V2 pair's whole rule: the product of the two reserves may not fall. The
// fee is withheld from the input before it is swapped, which is why it appears
// on the numerator and again scaled on the denominator.
//
// Fees are parts per million here because that is how a pool records one, and at
// 3000 the arithmetic is identical to Uniswap's 997/1000 — the same integers,
// scaled by a thousand top and bottom.

var zero = new(big.Int)

// amountOut is what a pool pays for amountIn, given the reserve it receives into
// and the reserve it pays out of. Nil when the trade cannot happen at all.
func amountOut(amtIn, reserveIn, reserveOut *big.Int, feePPM int64) *big.Int {
	if amtIn == nil || amtIn.Sign() <= 0 || reserveIn.Sign() <= 0 || reserveOut.Sign() <= 0 {
		return nil
	}
	if feePPM < 0 || feePPM >= 1_000_000 {
		return nil
	}
	million := big.NewInt(1_000_000)
	afterFee := new(big.Int).Mul(amtIn, big.NewInt(1_000_000-feePPM))
	num := new(big.Int).Mul(afterFee, reserveOut)
	den := new(big.Int).Add(new(big.Int).Mul(reserveIn, million), afterFee)
	if den.Sign() <= 0 {
		return nil
	}
	return num.Quo(num, den)
}

// amountIn is what a pool must receive to pay amountOut. Nil when the pool does
// not hold that much: a constant product can be drained towards its reserve but
// never to it, so asking for the whole side has no answer.
func amountIn(amtOut, reserveIn, reserveOut *big.Int, feePPM int64) *big.Int {
	if amtOut == nil || amtOut.Sign() <= 0 || reserveIn.Sign() <= 0 || reserveOut.Sign() <= 0 {
		return nil
	}
	if feePPM < 0 || feePPM >= 1_000_000 || amtOut.Cmp(reserveOut) >= 0 {
		return nil
	}
	million := big.NewInt(1_000_000)
	num := new(big.Int).Mul(new(big.Int).Mul(reserveIn, amtOut), million)
	den := new(big.Int).Mul(new(big.Int).Sub(reserveOut, amtOut), big.NewInt(1_000_000-feePPM))
	// One more base unit than the division gives, because the pool checks its
	// invariant after truncation and the exact quotient rounds the wrong way.
	return num.Add(num.Quo(num, den), big.NewInt(1))
}

// ── price impact ──────────────────────────────────────────────────────────

// priceImpact is how much worse a trade does than a negligible one would, as a
// percentage, composed across the route: each leg contributes the ratio of what
// it paid to what it would have paid at the marginal rate.
//
// Zero when any leg has no marginal to compare against — an unknown gap is
// reported as none rather than guessed at, and a negative one is rounding.
func priceImpact(r *route) float64 {
	exec, marginal := 1.0, 1.0
	for _, l := range r.legs {
		if l.marginal <= 0 || l.amtIn == nil || l.amtOut == nil || l.amtIn.Sign() <= 0 {
			return 0
		}
		exec *= ratio(l.amtOut, l.amtIn)
		marginal *= l.marginal
	}
	if marginal <= 0 || exec <= 0 || math.IsInf(exec, 0) || math.IsInf(marginal, 0) {
		return 0
	}
	impact := (1 - exec/marginal) * 100
	if impact < 0 {
		return 0
	}
	if impact > 100 {
		return 100
	}
	return impact
}

// ratio divides two integers as a rate. Both sides are raw base units, and the
// decimals cancel wherever a ratio is compared to another ratio.
func ratio(num, den *big.Int) float64 {
	if num == nil || den == nil || den.Sign() == 0 {
		return 0
	}
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(num), new(big.Float).SetInt(den)).Float64()
	return f
}

// ── rendering ─────────────────────────────────────────────────────────────

// decorate reads the price state of the pools the winning route crosses, and the
// block it all was read at, in one trip. Only the winner is asked: what a route
// that lost looked like is not in the answer.
func (q *Quoter) decorate(ctx context.Context, r *route) uint64 {
	var reqs []rpcRequest
	type at struct {
		l    *leg
		slot int
	}
	var v3 []at
	for _, l := range r.legs {
		if l.p.venue != "v3" {
			continue
		}
		v3 = append(v3, at{l, len(reqs)})
		reqs = append(reqs,
			ethCallReq(len(reqs), l.p.addr, selSlot0),
			ethCallReq(len(reqs)+1, l.p.addr, selLiquidity))
	}
	headAt := len(reqs)
	reqs = append(reqs, rpcRequest{JSONRPC: "2.0", ID: headAt, Method: "eth_blockNumber", Params: []any{}})

	res, err := q.node.batch(ctx, reqs)
	if err != nil {
		return 0
	}
	for _, a := range v3 {
		// slot0 opens with the price and the tick it sits in; the rest of the
		// struct is oracle bookkeeping this does not read.
		if w := words(res[a.slot]); len(w) >= 2 {
			a.l.sqrtP, a.l.tick = w[0], signed(w[1])
		}
		if w := words(res[a.slot+1]); len(w) >= 1 {
			a.l.liq = w[0]
		}
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(res[headAt], "0x"), 16)
	if !ok {
		return 0
	}
	return n.Uint64()
}

func (q *Quoter) render(req *quoteRequest, r *route, tradeType, swapper string, slippage float64, head uint64) classicQuote {
	// One read of the token table for the whole answer: it is a scan, and the
	// route asks about the same handful of tokens several times over.
	rows := q.tokens()

	last := len(r.legs) - 1
	hops := make([]hopJSON, 0, len(r.legs))
	for i, l := range r.legs {
		// The caller's own tokens are echoed as the caller wrote them, so a swap
		// from the native coin reads as the coin at both ends of the route rather
		// than as the wrapped token it travelled through.
		inTok := rows.describe(l.in, q.chainID)
		outTok := rows.describe(l.out, q.chainID)
		if i == 0 {
			inTok.Address = echoToken(req.TokenIn)
		}
		if i == last {
			outTok.Address = echoToken(req.TokenOut)
		}
		hops = append(hops, l.render(inTok, outTok))
	}

	names := make([]string, 0, len(r.legs)+1)
	names = append(names, rows.symbol(r.legs[0].in))
	for _, l := range r.legs {
		names = append(names, rows.symbol(l.out))
	}

	return classicQuote{
		ChainID:        q.chainID,
		Swapper:        swapper,
		Input:          quoteSide{Token: echoToken(req.TokenIn), Amount: r.amountIn().String()},
		Output:         quoteSide{Token: echoToken(req.TokenOut), Amount: r.amountOut().String(), Recipient: swapper},
		TradeType:      tradeType,
		Slippage:       slippage,
		GasUseEstimate: strconv.FormatInt(r.gas(), 10),
		// What the gas costs in the chain's coin is a fee market question, and
		// nothing here reads the fee market.
		GasFee:      "0",
		Route:       [][]hopJSON{hops},
		RouteString: strings.Join(names, " -> "),
		QuoteID:     requestID(),
		BlockNumber: strconv.FormatUint(head, 10),
		PriceImpact: priceImpact(r),
	}
}

func (l *leg) render(in, out tokenJSON) hopJSON {
	h := hopJSON{
		Address:   checksumAddress(l.p.addr),
		TokenIn:   in,
		TokenOut:  out,
		AmountIn:  l.amtIn.String(),
		AmountOut: l.amtOut.String(),
	}
	if l.p.venue == "v2" {
		h.Type = "v2-pool"
		t0, t1 := in, out
		if l.in == l.p.t1 {
			t0, t1 = out, in
		}
		h.Reserve0 = &reserveRef{Token: t0, Quotient: bigString(l.r0)}
		h.Reserve1 = &reserveRef{Token: t1, Quotient: bigString(l.r1)}
		return h
	}
	h.Type = "v3-pool"
	h.SqrtRatioX96 = bigString(l.sqrtP)
	h.Liquidity = bigString(l.liq)
	h.TickCurrent = strconv.FormatInt(l.tick, 10)
	h.Fee = strconv.FormatInt(l.p.fee, 10)
	return h
}

// tokenTable is the index's token rows, keyed the one way this file looks them
// up. The indexer writes a row under whatever casing the log carried, so the
// key is folded here rather than hoped about at each call site.
type tokenTable map[string]*storage.SeedTokenData

func (q *Quoter) tokens() tokenTable {
	rows := q.store.TokensRaw()
	out := make(tokenTable, len(rows))
	for addr, t := range rows {
		out[strings.ToLower(addr)] = t
	}
	return out
}

// describe says what a token is, as the index knows it. Eighteen decimals is the
// default the indexer itself falls back to, so a token it has not read yet reads
// the same way here as everywhere else.
func (t tokenTable) describe(addr string, chainID int64) tokenJSON {
	out := tokenJSON{Address: checksumAddress(addr), ChainID: chainID, Decimals: "18"}
	if row := t[addr]; row != nil {
		out.Symbol = row.Symbol
		if row.Decimals > 0 {
			out.Decimals = strconv.FormatInt(row.Decimals, 10)
		}
	}
	return out
}

// symbol names a token for the human-readable route. A token with no symbol
// indexed is named by its address, which is at least true.
func (t tokenTable) symbol(addr string) string {
	if row := t[addr]; row != nil && row.Symbol != "" {
		return row.Symbol
	}
	return checksumAddress(addr)
}

// unwrapNative maps the native sentinel onto the wrapped token that the pools
// actually hold.
func (q *Quoter) unwrapNative(addr string) string {
	a := strings.ToLower(addr)
	if a == nativeSentinel && q.wrapped != "" {
		return q.wrapped
	}
	return a
}

// echoToken gives the caller back the address it sent, checksummed. The native
// sentinel stays the sentinel: the caller asked about the coin, and the wrapping
// is this service's business, not theirs.
func echoToken(addr string) string {
	if strings.ToLower(addr) == nativeSentinel {
		return nativeSentinel
	}
	return checksumAddress(addr)
}

// wordHex is the length of one ABI word rendered as hex characters.
const wordHex = 64

// ── ABI ───────────────────────────────────────────────────────────────────

// numWord renders an integer as a 32-byte argument.
func numWord(n *big.Int) string {
	return fmt.Sprintf("%064x", n)
}

// addrWord renders an address as a 32-byte argument, left-padded.
func addrWord(addr string) string {
	return strings.Repeat("0", 24) + strings.ToLower(strings.TrimPrefix(addr, "0x"))
}

// words splits returned data into its 32-byte values. Everything read back here
// is a flat tuple of fixed-width values, so position is the whole decoding.
func words(data string) []*big.Int {
	h := strings.TrimPrefix(data, "0x")
	out := make([]*big.Int, 0, len(h)/wordHex)
	for i := 0; i+wordHex <= len(h); i += wordHex {
		n, ok := new(big.Int).SetString(h[i:i+wordHex], 16)
		if !ok {
			return out
		}
		out = append(out, n)
	}
	return out
}

// signed reinterprets a word as a two's-complement integer, which is how a tick
// — the only signed value read here — comes back.
func signed(w *big.Int) int64 {
	limit := new(big.Int).Lsh(big.NewInt(1), 255)
	if w.Cmp(limit) >= 0 {
		return new(big.Int).Sub(w, new(big.Int).Lsh(big.NewInt(1), 256)).Int64()
	}
	return w.Int64()
}

func bigString(n *big.Int) string {
	if n == nil {
		return "0"
	}
	return n.String()
}

// ── addresses ─────────────────────────────────────────────────────────────

func isHexAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	for _, c := range s[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// checksumAddress renders an address with EIP-55 capitalisation: the hash of the
// lower-cased digits decides which letters are upper. Wallets compare addresses
// in this form, so an address that goes out any other way looks like a different
// one to whatever reads it.
func checksumAddress(addr string) string {
	a := strings.ToLower(strings.TrimPrefix(addr, "0x"))
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(a))
	sum := h.Sum(nil)
	out := []byte("0x" + a)
	for i := 0; i < len(a); i++ {
		c := a[i]
		if c < 'a' || c > 'f' {
			continue
		}
		if sum[i/2]>>(4-4*(i%2))&0xf >= 8 {
			out[2+i] = c - ('a' - 'A')
		}
	}
	return string(out)
}

// parseWei reads an amount as the caller must write it: base units, decimal
// digits only. A float here would round someone's balance.
func parseWei(s string) (*big.Int, bool) {
	if s == "" {
		return nil, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, false
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	return n, ok
}
