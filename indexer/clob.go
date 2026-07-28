// clob.go is the native D-Chain (dexvm CLOB) source for the DEX subgraph.
//
// The EVM poller in indexer.go derives DEX entities from 0x9999 settlement LOGS
// on an EVM chain (C-Chain). That is the cross-chain SETTLEMENT view. The native
// trading engine, however, IS the D-Chain (dexvm): a trade is a consensus state
// transition matched at Block.Verify, not an EVM event. Its committed state is
// served by the read RPC at /v1/bc/D/dex/dex_get_{markets,trades,orders,book}
// (GET, self-describing JSON; see luxfi/dex pkg/dchain/read.go). Nothing on the
// EVM side carries those fills, so without this source the dex subgraph stays
// empty on a native-DEX deployment.
//
// CLOBSource polls that read surface and writes the SAME Market/Fill/Order
// entities (via storage.SetEntity) the EVM path writes, so the existing dex
// resolvers (resolvers/dex) and the FE serve them unchanged. It is orthogonal to
// the EVM poller: a distinct loop, a distinct protocol (GET/JSON, not the EVM
// JSON-RPC POST), a distinct endpoint. The two share only the store (the sink)
// and the entity shapes (the contract) — they never touch the same source, so
// running the CLOB source NEVER regresses AMM/EVM indexing.
//
// READ-ONLY: every call is a GET against committed chain state. No consensus
// impact, no mempool, no writes to the chain — only writes to the local subgraph
// store. Trades are paged incrementally by accepted height (since=lastHeight) so
// a steady chain costs one small trade query per tick.
package indexer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/luxfi/graph/storage"
)

// clobPollInterval is how often the CLOB source re-reads the D-Chain committed
// state. The read is cheap (committed state, no consensus) and the markets feed
// is not latency-critical, so a few seconds keeps the subgraph fresh without
// hammering the node. Matches the EVM poller's cadence.
const clobPollInterval = 3 * time.Second

// clobTradePage bounds one incremental dex_get_trades response. The trade log is
// paged by accepted height via the `since` cursor, so this only caps a single
// catch-up batch — a long history is drained across successive polls.
const clobTradePage = 1000

// CLOBSource indexes a native D-Chain (dexvm) CLOB into the DEX subgraph store by
// polling its committed-state read RPC. One source per dex subgraph; it owns the
// trade cursor and writes Market/Fill/Order entities the dex resolvers read.
type CLOBSource struct {
	// base is the D-Chain read-RPC root, e.g.
	// http://node:9650/v1/bc/D/dex  (dex_get_* are appended as /<method>).
	// Trailing "/" is trimmed on construction so joins are unambiguous.
	base   string
	store  *storage.Store
	client *http.Client

	// lastTradeHeight is the highest accepted block height whose trades we have
	// already ingested. The next poll requests since=lastTradeHeight+1. Fills are
	// keyed deterministically (height:seq), so a re-read of an already-seen height
	// is an idempotent overwrite — the cursor is an optimization, not correctness.
	lastTradeHeight uint64

	// status mirrors the indexer Status shape so graphd's /health can report CLOB
	// progress (committed head height + fills ingested) the same way it reports EVM.
	status Status
}

// NewCLOBSource builds a CLOB source for a D-Chain read-RPC base URL and a store.
// base is the chain's /v1/bc/<D>/dex root (with or without a trailing slash).
func NewCLOBSource(base string, store *storage.Store) *CLOBSource {
	return &CLOBSource{
		base:  strings.TrimRight(base, "/"),
		store: store,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// Status returns CLOB indexing progress (committed head height, fills ingested).
func (c *CLOBSource) Status() Status { return c.status }

// Run polls the D-Chain CLOB read RPC until ctx is cancelled. Each tick refreshes
// markets + their resting orders and drains any new fills. A poll error is logged
// and retried on the next tick — a transient node blip never tears down the loop.
func (c *CLOBSource) Run(ctx context.Context) error {
	log.Printf("[clob] starting — base=%s", c.base)
	ticker := time.NewTicker(clobPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.poll(ctx); err != nil {
				log.Printf("[clob] poll error: %v", err)
			}
		}
	}
}

// poll runs one read cycle: markets (+ resting orders per market) then the
// incremental trade drain. Markets/orders are a full refresh (the set is small and
// the book is authoritative); fills are append-only by height.
func (c *CLOBSource) poll(ctx context.Context) error {
	markets, head, err := c.fetchMarkets(ctx)
	if err != nil {
		return fmt.Errorf("markets: %w", err)
	}
	c.status.LatestBlock = head.Height
	for i := range markets {
		c.writeMarket(&markets[i])
		// Resting orders are per-market; refresh each market's open book so the FE's
		// depth/open-orders view reflects committed state. A market with no resting
		// orders simply clears to an empty set on the next overwrite.
		if err := c.refreshOrders(ctx, markets[i].PoolID); err != nil {
			log.Printf("[clob] orders %s: %v", markets[i].PoolID, err)
		}
	}
	if err := c.drainTrades(ctx); err != nil {
		return fmt.Errorf("trades: %w", err)
	}
	return nil
}

// ── wire types (mirror luxfi/dex pkg/dchain/read.go response bodies) ──────────

type clobHead struct {
	Height       uint64 `json:"height"`
	LastAccepted string `json:"lastAccepted"`
	Root         string `json:"root"`
}

type clobMarket struct {
	PoolID      string  `json:"poolId"`
	Symbol      string  `json:"symbol"`
	Base        string  `json:"base"`
	Quote       string  `json:"quote"`
	AssetsBound bool    `json:"assetsBound"`
	Orders      int     `json:"orders"`
	Remaining   float64 `json:"remaining"`
	BestBid     float64 `json:"bestBid"`
	BestAsk     float64 `json:"bestAsk"`
}

type clobMarketsResp struct {
	clobHead
	Count   int          `json:"count"`
	Markets []clobMarket `json:"markets"`
}

type clobTrade struct {
	Height       uint64  `json:"height"`
	Seq          uint64  `json:"seq"`
	TradeID      uint64  `json:"tradeId"`
	Price        float64 `json:"price"`
	Size         float64 `json:"size"`
	TakerSide    string  `json:"takerSide"`
	MakerOrderID uint64  `json:"makerOrderId"`
	TakerOrderID uint64  `json:"takerOrderId"`
	Timestamp    int64   `json:"timestamp"`
}

type clobTradesResp struct {
	clobHead
	Count  int         `json:"count"`
	Trades []clobTrade `json:"trades"`
}

type clobOrder struct {
	OrderID   uint64  `json:"orderId"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Remaining float64 `json:"remaining"`
	User      string  `json:"user"`
}

type clobOrdersResp struct {
	clobHead
	Market string      `json:"market"`
	Count  int         `json:"count"`
	Orders []clobOrder `json:"orders"`
}

// ── reads ─────────────────────────────────────────────────────────────────────

// fetchMarkets reads dex_get_markets (every market + its book summary + head).
func (c *CLOBSource) fetchMarkets(ctx context.Context) ([]clobMarket, clobHead, error) {
	var resp clobMarketsResp
	if err := c.get(ctx, "dex_get_markets", nil, &resp); err != nil {
		return nil, clobHead{}, err
	}
	return resp.Markets, resp.clobHead, nil
}

// drainTrades pages new fills from the global trade log (since the cursor) and
// writes each as a Fill, rolling per-market volume/trade-count/last-price. It
// loops until a page returns fewer than the requested limit (caught up), so a
// long backlog is drained within one poll without an unbounded single request.
func (c *CLOBSource) drainTrades(ctx context.Context) error {
	for {
		since := c.lastTradeHeight + 1
		if c.lastTradeHeight == 0 {
			since = 0 // first run: from genesis
		}
		var resp clobTradesResp
		params := map[string]string{
			"since": strconv.FormatUint(since, 10),
			"limit": strconv.Itoa(clobTradePage),
		}
		if err := c.get(ctx, "dex_get_trades", params, &resp); err != nil {
			return err
		}
		if len(resp.Trades) == 0 {
			return nil
		}
		maxH := c.lastTradeHeight
		for i := range resp.Trades {
			t := &resp.Trades[i]
			c.writeTrade(t)
			c.status.IndexedEvents++
			if t.Height > maxH {
				maxH = t.Height
			}
		}
		// Advance past the highest height we just ingested. Heights are
		// monotonically non-decreasing in the log, so this never skips a fill: a
		// later block's fills (same or greater height) are picked up next page.
		if maxH < c.lastTradeHeight+1 {
			// Defensive: no forward progress (all rows at/below cursor) — stop to
			// avoid a tight loop. Should not happen given since=cursor+1.
			return nil
		}
		c.lastTradeHeight = maxH
		if len(resp.Trades) < clobTradePage {
			return nil // caught up
		}
	}
}

// refreshOrders reads a market's resting book (dex_get_orders) and overwrites the
// market's Order entities. The order id is namespaced by poolID so two markets'
// order ids (which restart per market) never collide in the shared entity table.
func (c *CLOBSource) refreshOrders(ctx context.Context, poolID string) error {
	var resp clobOrdersResp
	if err := c.get(ctx, "dex_get_orders", map[string]string{"market": poolID}, &resp); err != nil {
		return err
	}
	for i := range resp.Orders {
		c.writeOrder(poolID, &resp.Orders[i])
	}
	return nil
}

// ── writes (Market/Fill/Order entities — the dex resolver contract) ───────────

// writeMarket upserts a Market entity from the authoritative book summary. The id
// IS the poolID (what Fills reference and the EVM 0x9999 path keys by), so both
// sources converge on one Market per pool. The human symbol is decoded from the
// bound-asset pair name when present, else the hex poolID. The live book fields
// (book summary) are refreshed every poll — the book moves, stale bid/ask/depth
// must not linger. The EVM-settlement path (handleInitializeV4/writeFill) owns the
// orthogonal accrued aggregates (volume24h/tradeCount/lastPrice/lastUpdate); when
// a chain runs BOTH sources we MERGE those through untouched so a CLOB book refresh
// never clobbers an EVM-accrued volume — mirroring handleInitializeV4's own merge.
// On a native-only chain those fields are simply absent.
func (c *CLOBSource) writeMarket(m *clobMarket) {
	mk := map[string]interface{}{
		"id":          m.PoolID,
		"symbol":      decodeMarketSymbol(m.Symbol),
		"baseToken":   m.Base,
		"quoteToken":  m.Quote,
		"assetsBound": m.AssetsBound,
		"openOrders":  m.Orders,
		"remaining":   formatFloat(m.Remaining),
		"bestBid":     formatFloat(m.BestBid),
		"bestAsk":     formatFloat(m.BestAsk),
	}
	if existing, _ := c.store.GetByType("Market", m.PoolID); existing != nil {
		if em, ok := existing.(map[string]interface{}); ok {
			for _, k := range []string{"volume24h", "tradeCount", "lastPrice", "lastUpdate", "feeTier"} {
				if v, present := em[k]; present {
					mk[k] = v
				}
			}
		}
	}
	c.store.SetEntity("Market", m.PoolID, mk)
}

// writeTrade upserts a Fill entity. The Fill id is the on-chain trade coordinate
// height:seq — deterministic and stable across re-reads, so a re-ingested trade
// is an idempotent overwrite (never a double-count). price/size/side and the
// maker/taker order ids give the FE a complete trade-history row.
//
// SCOPE — no per-market attribution here: the 80-byte on-chain trade row carries
// NO poolID (it is the frozen GPU ABI), so dex_get_trades is a GLOBAL,
// height-ordered log. Attributing a historical fill to a market by its order id is
// not reliable (a fully-filled order leaves the book, so the current
// dex_get_orders set cannot map every past fill back). Rather than FABRICATE a
// per-market volume by mis-attributing the global log, the truthful split is:
// Fill entities are the global trade feed (the trade-history view), and every
// PER-MARKET figure the FE shows (open orders, depth, best bid/ask, remaining) is
// taken from dex_get_markets' authoritative per-market book summary in
// writeMarket. This keeps both views correct without inventing data the chain
// does not expose.
func (c *CLOBSource) writeTrade(t *clobTrade) {
	id := fmt.Sprintf("%d:%d", t.Height, t.Seq)
	c.store.SetEntity("Fill", id, map[string]interface{}{
		"id":           id,
		"tradeId":      t.TradeID,
		"price":        formatFloat(t.Price),
		"size":         formatFloat(t.Size),
		"side":         t.TakerSide,
		"makerOrderId": strconv.FormatUint(t.MakerOrderID, 10),
		"takerOrderId": strconv.FormatUint(t.TakerOrderID, 10),
		"height":       t.Height,
		"seq":          t.Seq,
		"timestamp":    t.Timestamp,
	})
}

// writeOrder upserts a resting Order entity. The id is poolID#orderID so the
// per-market order id (which restarts per book) is globally unique in the shared
// entity table. market lets the FE filter open orders by market.
func (c *CLOBSource) writeOrder(poolID string, o *clobOrder) {
	id := poolID + "#" + strconv.FormatUint(o.OrderID, 10)
	c.store.SetEntity("Order", id, map[string]interface{}{
		"id":        id,
		"orderId":   o.OrderID,
		"market":    poolID,
		"side":      o.Side,
		"price":     formatFloat(o.Price),
		"size":      formatFloat(o.Size),
		"remaining": formatFloat(o.Remaining),
		"user":      o.User,
	})
}

// ── small pure helpers ────────────────────────────────────────────────────────

// get issues a GET to <base>/<method> with optional query params and decodes the
// JSON body into out. Body size is capped (defensive) like the EVM path.
func (c *CLOBSource) get(ctx context.Context, method string, params map[string]string, out interface{}) error {
	url := c.base + "/" + method
	if len(params) > 0 {
		var b strings.Builder
		b.WriteString(url)
		b.WriteByte('?')
		first := true
		for k, v := range params {
			if !first {
				b.WriteByte('&')
			}
			first = false
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(v)
		}
		url = b.String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s -> %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", method, err)
	}
	return nil
}

// decodeMarketSymbol turns a bound market's hex symbol into its human pair name.
// dexvm encodes a bound market's symbol as the ASCII pair (e.g. "LUX/LUSD")
// right-padded into 32 bytes then hex-encoded; the trailing 0x00 padding and any
// non-printable bytes are stripped. An unbound market's symbol is the raw 32-byte
// poolID hex (not ASCII) — for those the cleaned string is empty, so we fall back
// to the hex id. The result is a stable, display-ready label; ambiguity is
// resolved toward the readable pair when one exists.
func decodeMarketSymbol(hexSym string) string {
	s := strings.TrimPrefix(hexSym, "0x")
	if len(s)%2 != 0 {
		return hexSym
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return hexSym
	}
	// Keep the leading run of printable ASCII; stop at the first NUL/non-printable.
	end := 0
	for end < len(raw) {
		b := raw[end]
		if b == 0x00 || b < 0x20 || b > 0x7e {
			break
		}
		end++
	}
	label := strings.TrimSpace(string(raw[:end]))
	// A real pair name contains the "/" separator and at least one base char. The
	// raw poolID hex decodes to bytes that rarely form such a string, so requiring
	// the separator avoids surfacing garbage for unbound markets.
	if strings.Contains(label, "/") && len(label) >= 3 {
		return label
	}
	return hexSym
}

// formatFloat renders a CLOB float price/size for storage. The on-chain integer
// truth stays on the D-Chain; this is the display projection the read RPC already
// computed. -1 precision uses the shortest round-trip representation.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
