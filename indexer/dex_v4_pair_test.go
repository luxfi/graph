package indexer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// abiString ABI-encodes a short string as an ERC20 symbol()/name() return.
func abiString(s string) string {
	h := hex.EncodeToString([]byte(s))
	for len(h)%64 != 0 {
		h += "0"
	}
	return "0x" + fmt.Sprintf("%064x", 32) + fmt.Sprintf("%064x", len(s)) + h
}

// perAddrERC20RPC answers eth_call symbol() per-token from a map, decimals()=18.
func perAddrERC20RPC(symbols map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string        `json:"method"`
			Params []interface{} `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		result := "0x"
		if req.Method == "eth_call" {
			if call, ok := req.Params[0].(map[string]interface{}); ok {
				to, _ := call["to"].(string)
				switch call["data"] {
				case selSymbol:
					if sym, ok := symbols[strings.ToLower(to)]; ok {
						result = abiString(sym)
					}
				case selName:
					if sym, ok := symbols[strings.ToLower(to)]; ok {
						result = abiString(sym)
					}
				case selDecimals:
					result = "0x" + strings.Repeat("0", 62) + "12" // 18
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + result + `"}`))
	}
}

// A market whose two currencies resolve to clean ERC20 symbols gets a human
// BASE/QUOTE `symbol` and `assetsBound=true` — EXACTLY the shape the public
// exchange-api real-asset gate (dexMarkets.ts isStructurallyReal) requires to
// surface a native 0x9999 market. Without this the gate strips every EVM market
// (poolId-hex symbol fails the clean-pair check) and the markets surface is empty
// despite real on-chain DEXFills.
func TestHandleInitializeV4_BindsCleanPairSymbol(t *testing.T) {
	cur0 := "0x0000000000000000000000000000000000000a01"
	cur1 := "0x0000000000000000000000000000000000000b02"
	srv := httptest.NewServer(perAddrERC20RPC(map[string]string{
		cur0: "LUX",
		cur1: "LUSD",
	}))
	defer srv.Close()

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: srv.URL}, s)

	poolID := topic32("cafe")
	idx.processLog(context.Background(), initV4Log(poolID, cur0, cur1, 3000, 60, 0, 0, "0x1"))

	mk, _ := s.GetByType("Market", poolID)
	if mk == nil {
		t.Fatal("expected DEX Market from InitializeV4")
	}
	mm := mk.(map[string]interface{})
	if mm["symbol"] != "LUX/LUSD" {
		t.Errorf("symbol = %v, want clean pair LUX/LUSD", mm["symbol"])
	}
	if mm["assetsBound"] != true {
		t.Errorf("assetsBound = %v, want true (both currencies are named real assets)", mm["assetsBound"])
	}
	// The rich token pair must still be present (regression guard on the merge).
	if fmt.Sprint(mm["baseToken"]) != cur0 || fmt.Sprint(mm["quoteToken"]) != cur1 {
		t.Errorf("base/quote = %v/%v, want %s/%s", mm["baseToken"], mm["quoteToken"], cur0, cur1)
	}
}

// A token with no identifiable symbol (symbol() reverts → address placeholder)
// must NOT bind the market: assetsBound stays false and the symbol falls back to
// the poolId, so the gate hides it rather than surfacing a junk-symbol market.
func TestHandleInitializeV4_UnnamedTokenStaysUnbound(t *testing.T) {
	s := newMemSQLiteStore(t)
	// RPC unreachable → readERC20 keeps the short-address placeholder symbol.
	idx := NewWithConfig(Config{RPC: "http://127.0.0.1:0"}, s)

	poolID := topic32("dead")
	cur0 := "0x0000000000000000000000000000000000000c03"
	cur1 := "0x0000000000000000000000000000000000000d04"
	idx.processLog(context.Background(), initV4Log(poolID, cur0, cur1, 3000, 60, 0, 0, "0x1"))

	mk, _ := s.GetByType("Market", poolID)
	if mk == nil {
		t.Fatal("expected DEX Market (unbound, but still recorded)")
	}
	mm := mk.(map[string]interface{})
	if mm["assetsBound"] == true {
		t.Error("unnamed tokens must NOT bind the market (assetsBound must stay false)")
	}
	if mm["symbol"] != poolID {
		t.Errorf("symbol = %v, want poolId fallback %s", mm["symbol"], poolID)
	}
}
