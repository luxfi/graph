package indexer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luxfi/graph/storage"
)

// A swap's time comes from its block's header, and nothing else has one. These
// assert the two ways a row gets dated — as it arrives, and after the fact —
// because a day series is exactly as good as the clock underneath it, and the
// clock underneath it used to be a block number.

// chainAt serves a fake node: one PoolCreated, one Swap, and headers whose time
// is `mined`. It answers single calls and batches, as a node does.
func chainAt(t *testing.T, mined int64, logs []*logEntry) *httptest.Server {
	t.Helper()
	answer := func(method string, params json.RawMessage) interface{} {
		switch method {
		case "eth_blockNumber":
			return "0x9"
		case "eth_getBlockByNumber":
			var p []interface{}
			json.Unmarshal(params, &p)
			if len(p) > 0 && p[0] == "0x0" {
				return map[string]interface{}{"hash": "0xgenesis"}
			}
			return map[string]interface{}{"timestamp": hexUint(mined)}
		case "eth_getLogs":
			out := make([]interface{}, 0, len(logs))
			for _, l := range logs {
				out = append(out, map[string]interface{}{
					"address": l.Address, "topics": l.Topics, "data": l.Data,
					"blockNumber": l.BlockNumber, "transactionHash": l.TransactionHash,
					"logIndex": l.LogIndex,
				})
			}
			return out
		}
		return "0x"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		type call struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		reply := func(c call) map[string]interface{} {
			return map[string]interface{}{"jsonrpc": "2.0", "id": c.ID, "result": answer(c.Method, c.Params)}
		}
		if len(raw) > 0 && raw[0] == '[' {
			var batch []call
			json.Unmarshal(raw, &batch)
			out := make([]map[string]interface{}, 0, len(batch))
			for _, c := range batch {
				out = append(out, reply(c))
			}
			json.NewEncoder(w).Encode(out)
			return
		}
		var c call
		json.Unmarshal(raw, &c)
		json.NewEncoder(w).Encode(reply(c))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hexUint(n int64) string {
	const hex = "0123456789abcdef"
	if n == 0 {
		return "0x0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{hex[n&0xf]}, b...)
		n >>= 4
	}
	return "0x" + string(b)
}

// A swap indexed off the chain is dated by its block, not numbered by it. This
// drives the real poll loop, so it fails if the poll stops reading headers as
// surely as if the reader itself broke.
func TestPollDatesSwapsFromTheirBlock(t *testing.T) {
	const (
		token0 = "0x0000000000000000000000000000000000000011"
		token1 = "0x0000000000000000000000000000000000000022"
		pool   = "0x00000000000000000000000000000000000000c3"
		mined  = 1767225600 + 3600 // an hour into 2026-01-01
	)
	s := newMemSQLiteStore(t)
	srv := chainAt(t, mined, []*logEntry{
		poolCreatedLog(testFactoryV3, token0, token1, 3000, pool),
		swapV3Log(pool, token0, "0xreal", 1000, -2500),
	})
	idx := NewWithConfig(Config{RPC: srv.URL, FactoryV3: testFactoryV3}, s)

	if err := idx.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	raw := s.RecentSwapsRaw(10)
	if len(raw) != 1 {
		t.Fatalf("expected one swap, got %d", len(raw))
	}
	for _, sw := range raw {
		if sw.Timestamp != mined {
			t.Errorf("timestamp = %d, want %d — a swap's time is its block's time, not its number",
				sw.Timestamp, mined)
		}
		if sw.Block != 2 {
			t.Errorf("block = %d, want 2", sw.Block)
		}
		if !sw.Dated() {
			t.Error("a swap read from a live chain must be dated")
		}
	}
}

// A block that will not answer for its header is not indexed. Advancing past it
// would leave a permanent hole in the series that nothing revisits.
func TestPollWillNotAdvancePastAnUnreadableBlock(t *testing.T) {
	const pool = "0x00000000000000000000000000000000000000c3"
	s := newMemSQLiteStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		if len(raw) > 0 && raw[0] == '[' { // the header batch
			json.NewEncoder(w).Encode([]map[string]interface{}{})
			return
		}
		var c struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		json.Unmarshal(raw, &c)
		var result interface{} = "0x"
		switch c.Method {
		case "eth_blockNumber":
			result = "0x9"
		case "eth_getBlockByNumber":
			result = map[string]interface{}{"hash": "0xgenesis"}
		case "eth_getLogs":
			result = []interface{}{map[string]interface{}{
				"address": pool, "topics": []string{SigSwapV3, addrTopic(pool), addrTopic(pool)},
				"data": "0x", "blockNumber": "0x2", "transactionHash": "0xtx", "logIndex": "0x0",
			}}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": c.ID, "result": result})
	}))
	defer srv.Close()

	idx := NewWithConfig(Config{RPC: srv.URL, FactoryV3: testFactoryV3}, s)
	err := idx.poll(context.Background())
	if err == nil {
		t.Fatal("poll reported success for a block it could not date")
	}
	if !strings.Contains(err.Error(), "block times") {
		t.Errorf("error = %v, want it to name the unread block times", err)
	}
	if got := s.GetLastBlock(); got != 0 {
		t.Errorf("cursor advanced to %d past a block that was not fully read", got)
	}
}

// Rows an older build wrote hold a block NUMBER where the time belongs. They are
// the entire indexed history, so a series that cannot date them starts from
// today — which is not a chart.
func TestHealDatesTheHistory(t *testing.T) {
	const mined = 1767225600 + 7200
	s := newMemSQLiteStore(t)
	s.SeedSwap("0xold#0x1", &storage.SeedSwapData{
		Timestamp: 1136227, Pool: "0xpool", Amount0: "1", Amount1: "-2", AmountUSD: "0",
	})
	s.SeedSwap("0xnew#0x1", &storage.SeedSwapData{
		Timestamp: mined, Block: 99, Pool: "0xpool", Amount0: "1", Amount1: "-2", AmountUSD: "0",
	})
	idx := NewWithConfig(Config{RPC: chainAt(t, mined, nil).URL}, s)

	left, err := idx.healSwapTimes(context.Background())
	if err != nil || left != 0 {
		t.Fatalf("heal left %d rows undated: %v", left, err)
	}
	got := s.RecentSwapsRaw(10)
	old := got["0xold#0x1"]
	if old == nil {
		t.Fatal("the healed row vanished")
	}
	if old.Timestamp != mined || old.Block != 1136227 {
		t.Errorf("healed row = {ts %d, block %d}, want {ts %d, block 1136227}", old.Timestamp, old.Block, mined)
	}
	if n := got["0xnew#0x1"]; n.Timestamp != mined || n.Block != 99 {
		t.Errorf("an already-dated row was rewritten: %+v", n)
	}
	// Idempotent: a second run has nothing to do and must not re-date anything.
	if left, err := idx.healSwapTimes(context.Background()); left != 0 || err != nil {
		t.Errorf("second heal reported %d undated rows: %v", left, err)
	}
}

// The day a trade belongs to is the UTC day containing it.
func TestDayStartIsUTCMidnight(t *testing.T) {
	const midnight = 1767225600 // 2026-01-01 00:00:00 UTC
	for _, c := range []struct {
		ts   int64
		want int64
	}{
		{midnight, midnight},
		{midnight + 1, midnight},
		{midnight + 86399, midnight},
		{midnight + 86400, midnight + 86400},
		{midnight - 1, midnight - 86400},
	} {
		if got := dayStart(c.ts); got != c.want {
			t.Errorf("dayStart(%d) = %d, want %d", c.ts, got, c.want)
		}
	}
	if got := dayID("0xABC", midnight); got != "0xabc-20454" {
		t.Errorf("dayID = %q, want 0xabc-20454", got)
	}
}
