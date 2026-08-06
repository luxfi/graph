package indexer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/indexer"
	"github.com/luxfi/graph/storage"
)

// One DEXFill log on the chain must become one fill on the GraphQL wire.
//
// Everything between those two ends is the real code: the real poll loop, the
// real eth_getLogs topic filter, the real 0x9999 emitter gate, the real decode,
// the real store, the real dex schema and the real resolvers. Only the chain is
// substituted, by a JSON-RPC server that serves exactly one DEXFill.
//
// This exists because the D-Chain cannot settle yet, so no DEXFill has ever been
// emitted on any network and the pipe cannot be validated by waiting. A green
// handler unit test does not answer "will a fill reach the page"; the failure
// modes live in the seams — a topic0 the filter never asks for, a field name the
// frontend does not read, an empty result that encodes as null. This asserts the
// seams.
//
// The query and the field names below are copied from the explorer's DEX page
// (lib/api/dchain): `fills(first:25){ id market taker amountOut timestamp txHash }`.
func TestDEXFillReachesTheWire(t *testing.T) {
	const (
		poolID = "0x" + "ab" // padded below
		taker  = "0x1234567890123456789012345678901234567890"
		txHash = "0xfeed000000000000000000000000000000000000000000000000000000000001"
	)
	pool := "0x" + strings.Repeat("ab", 32)
	// DEXFill(bytes32 indexed poolId, address indexed taker, uint256 amountOut,
	// uint256 blockNumber): amountOut = 400, blockNumber = 7.
	data := "0x" + fmt.Sprintf("%064x%064x", 400, 7)

	fill := map[string]interface{}{
		"address":          indexer.LXSettleAddress,
		"topics":           []string{indexer.SigDEXFill, pool, "0x000000000000000000000000" + taker[2:]},
		"data":             data,
		"blockNumber":      "0x7",
		"transactionHash":  txHash,
		"logIndex":         "0x0",
		"transactionIndex": "0x0",
	}

	var sawTopicFilter bool
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		var result interface{}
		switch req.Method {
		case "eth_blockNumber":
			result = "0x7"
		case "eth_getBlockByNumber":
			result = map[string]interface{}{"hash": "0x" + strings.Repeat("11", 32)}
		case "eth_getLogs":
			// The pipe only works if the poller ASKS for the DEXFill topic. If it
			// does not, the log below would still be served here and the test would
			// pass while production saw nothing — so assert the subscription.
			if strings.Contains(string(req.Params), indexer.SigDEXFill) {
				sawTopicFilter = true
			}
			result = []interface{}{fill}
		default:
			// Valuation reads chain state (eth_call); it shares only the store and
			// must not be able to stall the log path.
			result = "0x"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0", "id": req.ID, "result": result,
		})
	}))
	defer rpc.Close()

	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	idx := indexer.NewWithConfig(indexer.Config{RPC: rpc.URL, Label: "test/dex"}, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go idx.Run(ctx)

	eng := engine.New(store, &engine.Config{MaxQueryDepth: 10, MaxResultSize: 1 << 20, QueryTimeoutMs: 30000})
	if err := eng.LoadBuiltin("dex"); err != nil {
		t.Fatal(err)
	}
	const query = "{ fills(first: 25){ id market taker amountOut timestamp txHash } }"

	var body string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp := eng.Execute(ctx, &engine.Request{Query: query})
		if len(resp.Errors) != 0 {
			t.Fatalf("fills query errored: %+v", resp.Errors)
		}
		raw, _ := json.Marshal(resp.Data)
		body = string(raw)
		if strings.Contains(body, txHash) {
			break
		}
		// Until the fill lands the answer must still be a well-formed empty list,
		// never null — that is what tells an operator the pipe is alive.
		if strings.Contains(body, "null") {
			t.Fatalf("idle subgraph answered %s; want an empty list", body)
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !sawTopicFilter {
		t.Error("eth_getLogs never filtered on the DEXFill topic0 — the subgraph is not subscribed")
	}
	if !strings.Contains(body, txHash) {
		t.Fatalf("a DEXFill at 0x9999 never reached the wire; last answer: %s", body)
	}
	// The frontend reads these exact fields; a rename anywhere in the pipe blanks
	// the page without erroring.
	for _, want := range []string{pool, strings.ToLower(taker), `"amountOut":"400"`, `"timestamp":7`} {
		if !strings.Contains(body, want) {
			t.Errorf("fill on the wire is missing %s; got %s", want, body)
		}
	}

	// The same fill must roll up into the market the page's headline stats read.
	resp := eng.Execute(ctx, &engine.Request{Query: "{ markets { id volume24h tradeCount } }"})
	if len(resp.Errors) != 0 {
		t.Fatalf("markets query errored: %+v", resp.Errors)
	}
	raw, _ := json.Marshal(resp.Data)
	for _, want := range []string{pool, `"volume24h":"400"`, `"tradeCount":1`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("market rollup is missing %s; got %s", want, raw)
		}
	}
}
