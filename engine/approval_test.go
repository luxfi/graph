package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// As with the swap calldata, the approval bytes below were recorded from
// exchange-api against Lux mainnet. The allowance is served by a stub node so
// the test states what the chain said rather than waiting to find out — and so
// the two answers that matter, "already approved" and "not approved", can both
// be exercised on demand.

// stubNode answers an allowance() call with the value given, and records what
// it was asked. Serving real JSON-RPC rather than faking the call keeps the
// encoding of the request under test too.
func stubNode(t *testing.T, allowance string) (*httptest.Server, *string) {
	t.Helper()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// An eth_call's params are the call and then the block to read it at,
		// so the array is not one type and only its head is a call.
		var reqs []struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &reqs); err != nil || len(reqs) == 0 || len(reqs[0].Params) == 0 {
			t.Errorf("node asked something that is not a JSON-RPC batch: %s", body)
			return
		}
		if reqs[0].Method != "eth_call" {
			t.Errorf("method = %q, want eth_call", reqs[0].Method)
		}
		var call struct{ To, Data string }
		if err := json.Unmarshal(reqs[0].Params[0], &call); err != nil {
			t.Errorf("first param is not a call: %s", reqs[0].Params[0])
		}
		seen = call.To + " " + call.Data
		fmt.Fprintf(w, `[{"jsonrpc":"2.0","id":0,"result":"0x%064s"}]`, allowance)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func serveApproval(t *testing.T, rpc, body string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/check_approval", HandleCheckApproval(luxMain, rpc, router))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/check_approval", strings.NewReader(body)))

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
	}
	return rec.Code, out
}

// A wallet that has granted nothing gets the grant transaction, and it must be
// an unlimited approval addressed to the router — the exact bytes exchange-api
// returned for the same request.
func TestApprovalNeededMatchesRecording(t *testing.T) {
	srv, asked := stubNode(t, "0") // no allowance
	code, out := serveApproval(t, srv.URL, `{
		"walletAddress":"0x00000000000000000000000000000000DeaDBeef",
		"token":"`+lusd+`","amount":"1000000000000000000","chainId":96369}`)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", code, out)
	}
	// The allowance was read from the token, for this wallet, against the router.
	want := strings.ToLower(lusd) + " " + selAllowance +
		"00000000000000000000000000000000000000000000000000000000deadbeef" +
		"000000000000000000000000939bc0bca6f9b9c52e6e3ad8a3c590b5d9b9d10e"
	if strings.ToLower(*asked) != want {
		t.Errorf("allowance call:\n got  %s\n want %s", strings.ToLower(*asked), want)
	}

	approval, ok := out["approval"].(map[string]any)
	if !ok {
		t.Fatalf("approval is not an object: %v", out)
	}
	assertSwap(t, approval, map[string]any{
		"to":      lusd,
		"from":    "0x00000000000000000000000000000000DeaDBeef",
		"value":   "0",
		"chainId": float64(luxMain),
		"data": "0x095ea7b3" +
			"000000000000000000000000939bc0bca6f9b9c52e6e3ad8a3c590b5d9b9d10e" + // the router
			"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", // unlimited
	})
}

// An allowance that already covers the trade is not a smaller approval, it is
// no approval — the swap form shows one signature instead of two.
func TestApprovalNotNeededWhenAllowanceCovers(t *testing.T) {
	for _, c := range []struct {
		name, allowance, amount string
	}{
		{"exactly covers", "de0b6b3a7640000", "1000000000000000000"},
		{"more than covers", "ffffffffffffffff", "1000000000000000000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := stubNode(t, c.allowance)
			code, out := serveApproval(t, srv.URL, `{
				"walletAddress":"`+trader+`","token":"`+lusd+`","amount":"`+c.amount+`"}`)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200", code)
			}
			if out["approval"] != nil {
				t.Errorf("approval = %v, want null", out["approval"])
			}
			if id, _ := out["requestId"].(string); id == "" {
				t.Error("requestId missing")
			}
		})
	}
}

// One unit short is still short. This is the boundary the whole endpoint turns
// on, so it is asserted rather than assumed.
func TestApprovalNeededWhenAllowanceOneShort(t *testing.T) {
	srv, _ := stubNode(t, "de0b6b3a763ffff") // 1e18 - 1
	_, out := serveApproval(t, srv.URL, `{
		"walletAddress":"`+trader+`","token":"`+lusd+`","amount":"1000000000000000000"}`)
	if out["approval"] == nil {
		t.Error("approval = null, but the allowance is one unit short of the amount")
	}
}

// The chain's own coin is sent with the swap, not spent from a balance, so
// there is nothing to approve and no call to make.
func TestApprovalNativeNeedsNoneAndAsksNothing(t *testing.T) {
	srv, asked := stubNode(t, "0")
	code, out := serveApproval(t, srv.URL, `{
		"walletAddress":"`+trader+`","token":"0x0000000000000000000000000000000000000000",
		"amount":"1000000000000000000","chainId":96369}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if out["approval"] != nil {
		t.Errorf("approval = %v, want null for the native coin", out["approval"])
	}
	if *asked != "" {
		t.Errorf("asked the chain %q about the native coin; it should ask nothing", *asked)
	}
}

// A node that cannot be reached is not an allowance of zero. Answering "no
// approval needed" there would send someone into a swap that reverts.
func TestApprovalNodeFailureIsNotZeroAllowance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	code, out := serveApproval(t, srv.URL, `{
		"walletAddress":"`+trader+`","token":"`+lusd+`","amount":"1000000000000000000"}`)
	if code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when the node is unreachable: %v", code, out)
	}
	if out["errorCode"] != "INTERNAL_ERROR" {
		t.Errorf("errorCode = %v, want INTERNAL_ERROR", out["errorCode"])
	}
}

func TestApprovalRejectsBadRequests(t *testing.T) {
	srv, _ := stubNode(t, "0")
	for _, c := range []struct{ name, body, detail string }{
		{"no wallet", `{"token":"` + lusd + `","amount":"1"}`,
			"walletAddress must be an address"},
		{"no token", `{"walletAddress":"` + trader + `","amount":"1"}`,
			"token is required"},
		{"amount not wei", `{"walletAddress":"` + trader + `","token":"` + lusd + `","amount":"1.5"}`,
			"amount must be a decimal wei string"},
		{"foreign chain", `{"walletAddress":"` + trader + `","token":"` + lusd + `","amount":"1","chainId":1}`,
			"unsupported chainId 1"},
		{"not JSON", `{`, "body must be JSON"},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, out := serveApproval(t, srv.URL, c.body)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", code)
			}
			if out["errorCode"] != "VALIDATION_ERROR" || out["detail"] != c.detail {
				t.Errorf("got %v, want detail %q", out, c.detail)
			}
		})
	}
}
