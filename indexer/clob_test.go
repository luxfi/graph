package indexer

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// clobStub is an in-process D-Chain read-RPC double serving the exact JSON shapes
// of luxfi/dex pkg/dchain/read.go (captured live from devnet). It lets the CLOB
// source be driven deterministically with no node. Trades are served filtered by
// the `since` cursor so the incremental drain is exercised honestly.
type clobStub struct {
	mu          sync.Mutex
	marketsBody string
	trades      []string // each is a ready trade JSON object, height-ordered
	ordersBody  map[string]string
	hits        map[string]int
}

func newCLOBStub() *clobStub {
	return &clobStub{ordersBody: map[string]string{}, hits: map[string]int{}}
}

func (s *clobStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		method := strings.TrimPrefix(r.URL.Path, "/")
		s.hits[method]++
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "dex_get_markets":
			fmt.Fprint(w, s.marketsBody)
		case "dex_get_trades":
			var since uint64
			fmt.Sscanf(r.URL.Query().Get("since"), "%d", &since)
			rows := make([]string, 0, len(s.trades))
			for _, tr := range s.trades {
				var h uint64
				// cheap height extract from the canned row
				fmt.Sscanf(tr, `{"height":%d`, &h)
				if h >= since {
					rows = append(rows, tr)
				}
			}
			fmt.Fprintf(w, `{"height":209,"lastAccepted":"abc","root":"deadbeef","count":%d,"trades":[%s]}`,
				len(rows), strings.Join(rows, ","))
		case "dex_get_orders":
			m := r.URL.Query().Get("market")
			if body, ok := s.ordersBody[m]; ok {
				fmt.Fprint(w, body)
				return
			}
			fmt.Fprintf(w, `{"height":209,"lastAccepted":"abc","root":"deadbeef","market":%q,"count":0,"orders":[]}`, m)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// liveMarketsBody is the dex_get_markets body captured from lux-devnet luxd-0:
// one unbound pool, two bound markets (LUX/LETH, LUX/LUSD) whose hex symbols
// decode to human pair names.
const liveMarketsBody = `{"height":209,"lastAccepted":"YtNQ","root":"dba7","count":3,"markets":[` +
	`{"poolId":"32bb228d59d9770be8c4ec1153267135f294bfcc0a96bab8f54c408e3735cebd","symbol":"32bb228d59d9770be8c4ec1153267135f294bfcc0a96bab8f54c408e3735cebd","assetsBound":false,"orders":0,"remaining":0,"bestBid":0,"bestAsk":0},` +
	`{"poolId":"4c55582f4c4554480000000000000000000000000000000000000000000000d0","symbol":"4c55582f4c4554480000000000000000000000000000000000000000000000d0","base":"00","quote":"01","assetsBound":true,"orders":10,"remaining":26.375,"bestBid":0.003996,"bestAsk":0.004004},` +
	`{"poolId":"4c55582f4c5553440000000000000000000000000000000000000000000000d0","symbol":"4c55582f4c5553440000000000000000000000000000000000000000000000d0","base":"00","quote":"01","assetsBound":true,"orders":21,"remaining":133.375,"bestBid":12.4875,"bestAsk":5}` +
	`]}`

// TestCLOB_IndexesNativeMarketsTradesOrders is the end-to-end proof: a CLOB source
// pointed at the read-RPC double populates Market/Fill/Order entities the dex
// resolvers serve, with the bound-market symbol decoded to its human pair name.
func TestCLOB_IndexesNativeMarketsTradesOrders(t *testing.T) {
	s := newMemSQLiteStore(t)
	stub := newCLOBStub()
	stub.marketsBody = liveMarketsBody
	stub.trades = []string{
		`{"height":172,"seq":0,"tradeId":1,"price":5,"size":10,"takerSide":"buy","makerOrderId":519691042817,"takerOrderId":738734374913,"timestamp":1782163644410312965}`,
		`{"height":179,"seq":0,"tradeId":1,"price":5,"size":10,"takerSide":"buy","makerOrderId":549755813889,"takerOrderId":768799145985,"timestamp":1782163752630157863}`,
	}
	bound := "4c55582f4c5553440000000000000000000000000000000000000000000000d0"
	stub.ordersBody[bound] = `{"height":209,"lastAccepted":"abc","root":"deadbeef","market":"` + bound + `","count":2,"orders":[` +
		`{"orderId":12,"side":"buy","price":12.4875,"size":100,"remaining":100,"user":"aabb"},` +
		`{"orderId":13,"side":"sell","price":5,"size":33.375,"remaining":33.375,"user":"ccdd"}]}`
	srv := stub.server(t)

	c := NewCLOBSource(srv.URL, s)
	if err := c.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	// Markets: 3 present, keyed by poolID.
	mkts, _ := s.ListByType("Market", 100)
	if mkts == nil || len(mkts.([]interface{})) != 3 {
		t.Fatalf("want 3 markets, got %v", mkts)
	}
	// The bound LUX/LUSD market carries a human symbol + live book summary.
	mk, _ := s.GetByType("Market", bound)
	if mk == nil {
		t.Fatal("LUX/LUSD market missing")
	}
	mm := mk.(map[string]interface{})
	if mm["symbol"] != "LUX/LUSD" {
		t.Errorf("symbol = %v, want LUX/LUSD (decoded from hex)", mm["symbol"])
	}
	if mm["assetsBound"] != true {
		t.Errorf("assetsBound = %v, want true", mm["assetsBound"])
	}
	if fmt.Sprint(mm["bestBid"]) != "12.4875" || fmt.Sprint(mm["bestAsk"]) != "5" {
		t.Errorf("book summary = bid %v ask %v, want 12.4875/5", mm["bestBid"], mm["bestAsk"])
	}
	// An unbound pool keeps its hex poolID as the symbol (no fabricated label).
	unbound := "32bb228d59d9770be8c4ec1153267135f294bfcc0a96bab8f54c408e3735cebd"
	um, _ := s.GetByType("Market", unbound)
	if um.(map[string]interface{})["symbol"] != unbound {
		t.Errorf("unbound market symbol = %v, want raw poolID", um.(map[string]interface{})["symbol"])
	}

	// Fills: 2 from the trade log, keyed height:seq, with full trade rows.
	fills, _ := s.ListByType("Fill", 100)
	if fills == nil || len(fills.([]interface{})) != 2 {
		t.Fatalf("want 2 fills, got %v", fills)
	}
	f, _ := s.GetByType("Fill", "172:0")
	if f == nil {
		t.Fatal("fill 172:0 missing")
	}
	fm := f.(map[string]interface{})
	if fm["price"] != "5" || fm["size"] != "10" || fm["side"] != "buy" {
		t.Errorf("fill 172:0 = price %v size %v side %v, want 5/10/buy", fm["price"], fm["size"], fm["side"])
	}
	if fm["takerOrderId"] != "738734374913" {
		t.Errorf("fill takerOrderId = %v, want 738734374913", fm["takerOrderId"])
	}

	// Orders: 2 resting on the bound market, namespaced poolID#orderId.
	ords, _ := s.ListByType("Order", 100)
	if ords == nil || len(ords.([]interface{})) != 2 {
		t.Fatalf("want 2 orders, got %v", ords)
	}
	o, _ := s.GetByType("Order", bound+"#12")
	if o == nil {
		t.Fatal("order #12 missing")
	}
	om := o.(map[string]interface{})
	if om["market"] != bound || om["side"] != "buy" || om["price"] != "12.4875" {
		t.Errorf("order #12 = market %v side %v price %v", om["market"], om["side"], om["price"])
	}
}

// TestCLOB_IncrementalAndIdempotent proves the trade cursor advances (a second
// poll re-requests only since>=lastHeight+1) and that re-ingesting a trade is an
// idempotent overwrite (the Fill set does not grow on a re-read of the same row).
func TestCLOB_IncrementalAndIdempotent(t *testing.T) {
	s := newMemSQLiteStore(t)
	stub := newCLOBStub()
	stub.marketsBody = `{"height":1,"lastAccepted":"a","root":"b","count":0,"markets":[]}`
	stub.trades = []string{
		`{"height":10,"seq":0,"tradeId":1,"price":5,"size":1,"takerSide":"buy","makerOrderId":1,"takerOrderId":2,"timestamp":1}`,
	}
	srv := stub.server(t)
	c := NewCLOBSource(srv.URL, s)

	if err := c.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.lastTradeHeight != 10 {
		t.Fatalf("cursor must advance to 10, got %d", c.lastTradeHeight)
	}
	fills, _ := s.ListByType("Fill", 100)
	if len(fills.([]interface{})) != 1 {
		t.Fatalf("want 1 fill after poll 1, got %d", len(fills.([]interface{})))
	}

	// A new fill at a higher height arrives; the old one is still served (since<=10
	// not yet advanced past it) — the Fill set must end at 2, not 3 (no double-count
	// of the height-10 row on its re-read).
	stub.mu.Lock()
	stub.trades = append(stub.trades,
		`{"height":12,"seq":0,"tradeId":1,"price":6,"size":2,"takerSide":"sell","makerOrderId":3,"takerOrderId":4,"timestamp":2}`)
	stub.mu.Unlock()

	if err := c.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.lastTradeHeight != 12 {
		t.Fatalf("cursor must advance to 12, got %d", c.lastTradeHeight)
	}
	fills, _ = s.ListByType("Fill", 100)
	if len(fills.([]interface{})) != 2 {
		t.Fatalf("idempotent re-read: want 2 fills, got %d", len(fills.([]interface{})))
	}
}

// TestCLOB_AdditiveToEVM is the no-regression guard: an indexer configured with
// DexRPC still constructs the EVM poller (rpc-based) unchanged, AND a CLOB source.
// The EVM-side AMM path (handleSwapV4 etc.) is untouched — proven by feeding an
// AMM swap log through processLog and seeing the swap recorded, with the CLOB
// source present but driving only Market/Fill/Order. AMM and DEX never collide.
func TestCLOB_AdditiveToEVM(t *testing.T) {
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused", DexRPC: "http://node/v1/chain/D/dex"}, s)
	if idx.clob == nil {
		t.Fatal("DexRPC set must construct a CLOB source")
	}

	// The EVM AMM path still works: a canonical 0x9999 V4 swap is recorded as an
	// AMM swap (the CLOB source touches none of this).
	poolID := topic32("abcd")
	taker := "0x00000000000000000000000000000000000000aa"
	idx.processLog(context.Background(), &logEntry{
		Address:         LXSettleAddress,
		Topics:          []string{SigSwapV4, poolID, addrTopic(taker)},
		Data:            "0x" + signedWord(big.NewInt(1000)) + signedWord(big.NewInt(-2500)) + word(big.NewInt(0)) + word(big.NewInt(0)) + word(big.NewInt(0)) + word(big.NewInt(3000)),
		BlockNumber:     "0x10",
		TransactionHash: "0xdead",
		LogIndex:        "0x0",
	})
	sw, _ := s.GetSwaps(nil, 10, "timestamp", "desc", nil)
	if sw == nil || len(sw.([]interface{})) != 1 {
		t.Fatalf("EVM AMM swap must still be recorded with a CLOB source present, got %v", sw)
	}
}

// TestDecodeMarketSymbol covers the bound→human and unbound→hex projection.
func TestDecodeMarketSymbol(t *testing.T) {
	cases := []struct{ in, want string }{
		{"4c55582f4c5553440000000000000000000000000000000000000000000000d0", "LUX/LUSD"},
		{"4c55582f4c4554480000000000000000000000000000000000000000000000d0", "LUX/LETH"},
		// Unbound pool: random 32-byte poolID hex has no "/" → keep the hex id.
		{"32bb228d59d9770be8c4ec1153267135f294bfcc0a96bab8f54c408e3735cebd",
			"32bb228d59d9770be8c4ec1153267135f294bfcc0a96bab8f54c408e3735cebd"},
		{"0xdeadbeef", "0xdeadbeef"}, // not a pair, odd content → unchanged
	}
	for _, tc := range cases {
		if got := decodeMarketSymbol(tc.in); got != tc.want {
			t.Errorf("decodeMarketSymbol(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
