package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/luxfi/graph/storage"
)

// dex_route_test.go is the end-to-end proof for the exchange-api FIX D routing: the
// FE's market/fill (CLOB) query shapes resolve against the `dex` schema and NOT the
// `amm` schema. Red proved live that `amm` returns "unknown field: markets"; this
// pins that markets/fills ARE dex-schema fields that resolve to the entities the
// 0x9999 indexer writes (Market from the Initialize log, Fill from DEXFill), so the
// exchange-api routing target (/v1/graph/cchain/dex/graphql) is correct.

func newDexEngine(t *testing.T) (*Engine, *storage.Store) {
	t.Helper()
	s, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	eng := New(s, &Config{MaxQueryDepth: 10, MaxResultSize: 1 << 20, QueryTimeoutMs: 30000})
	if err := eng.LoadBuiltin("dex"); err != nil {
		t.Fatalf("LoadBuiltin(dex): %v", err)
	}
	return eng, s
}

// TestDexSchema_ResolvesMarketsAndFills proves the FE's CLOB query shapes resolve on
// the dex schema against the exact entities the native 0x9999 indexer produces.
func TestDexSchema_ResolvesMarketsAndFills(t *testing.T) {
	eng, s := newDexEngine(t)

	// Seed exactly what the indexer writes: a rich Market (from the 0x9999 Initialize
	// log) and a Fill (from a DEXFill at 0x9999).
	poolID := "0x" + strings.Repeat("ab", 32)
	s.SetEntity("Market", poolID, map[string]interface{}{
		"id": poolID, "symbol": poolID, "baseToken": "0x11", "quoteToken": "0x22",
		"feeTier": int64(3000), "volume24h": "400", "tradeCount": int64(1), "lastPrice": "0",
	})
	s.SetEntity("Fill", "0xtx#0", map[string]interface{}{
		"id": "0xtx#0", "market": poolID, "taker": "0xaa", "amountOut": "400", "timestamp": int64(9),
	})

	// The FE markets query shape.
	resp := eng.Execute(context.Background(), &Request{
		Query: "{ markets(first: 5){ id baseToken quoteToken feeTier volume24h } }",
	})
	if len(resp.Errors) != 0 {
		t.Fatalf("markets must resolve on dex schema, got errors: %+v", resp.Errors)
	}
	// Round-trip through JSON to assert the field actually carries the seeded data.
	raw, _ := json.Marshal(resp.Data)
	got := string(raw)
	for _, want := range []string{`"markets"`, poolID, `"feeTier"`, `400`} {
		if !strings.Contains(got, want) {
			t.Errorf("markets response missing %q; got %s", want, got)
		}
	}

	// The FE fills query shape.
	resp = eng.Execute(context.Background(), &Request{
		Query: "{ fills(first: 5){ id market taker amountOut } }",
	})
	if len(resp.Errors) != 0 {
		t.Fatalf("fills must resolve on dex schema, got errors: %+v", resp.Errors)
	}
	raw, _ = json.Marshal(resp.Data)
	if !strings.Contains(string(raw), `"fills"`) || !strings.Contains(string(raw), "0xtx#0") {
		t.Errorf("fills response missing seeded fill; got %s", string(raw))
	}
}

// TestDexSchema_EmptyIsAnEmptyList pins the wire answer of a healthy-but-idle dex
// subgraph. Before this was fixed, a mounted subgraph that was subscribed to 0x9999
// and caught up to the chain head still answered `{"data":{"markets":null}}` — the
// wire signature of a subgraph that is NOT deployed. Operators cannot tell "no fills
// yet" from "the pipe is broken" unless empty means [].
//
// The three states must be distinguishable on the wire:
//
//	not deployed  -> {"errors":[{"message":"unknown field: markets"}]}
//	erroring      -> {"errors":[…]}
//	deployed,idle -> {"data":{"markets":[]}}
func TestDexSchema_EmptyIsAnEmptyList(t *testing.T) {
	eng, _ := newDexEngine(t) // no entities seeded: the live mainnet condition

	for _, q := range []string{
		"{ markets { id } }",
		"{ fills(first: 25){ id market taker amountOut timestamp txHash } }",
		"{ orders(first: 25){ id } }",
	} {
		resp := eng.Execute(context.Background(), &Request{Query: q})
		if len(resp.Errors) != 0 {
			t.Fatalf("%s: unexpected errors on an idle subgraph: %+v", q, resp.Errors)
		}
		raw, _ := json.Marshal(resp.Data)
		if strings.Contains(string(raw), "null") {
			t.Errorf("%s answered %s; an idle subgraph must answer with an empty list, not null", q, raw)
		}
	}
}

// TestAmmSchema_DoesNotResolveMarkets pins the negative half: the amm schema does NOT
// know `markets` (it returns an unknown-field error) — which is WHY the exchange-api
// must route CLOB queries to the dex endpoint. This mirrors Red's live finding.
func TestAmmSchema_DoesNotResolveMarkets(t *testing.T) {
	s, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	eng := New(s, &Config{MaxQueryDepth: 10, MaxResultSize: 1 << 20, QueryTimeoutMs: 30000})
	if err := eng.LoadBuiltin("amm"); err != nil {
		t.Fatalf("LoadBuiltin(amm): %v", err)
	}

	resp := eng.Execute(context.Background(), &Request{Query: "{ markets(first: 1){ id } }"})
	if len(resp.Errors) == 0 {
		t.Fatal("amm schema must NOT resolve `markets` (it is a dex-schema field) — routing depends on this")
	}
	// And amm DOES resolve its own root (pools), confirming the engine is live.
	resp = eng.Execute(context.Background(), &Request{Query: "{ pools(first: 1){ id } }"})
	if len(resp.Errors) != 0 {
		t.Fatalf("amm schema must resolve `pools`, got: %+v", resp.Errors)
	}
}
