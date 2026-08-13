package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The bytes below were recorded from exchange-api, the TypeScript service this
// endpoint replaces, against Lux mainnet. They are the whole point of the test:
// a wallet signs this calldata, so "equivalent" is not a standard that means
// anything here — a single byte out addresses a different pool or moves a
// different amount. Each case asserts the full response, not a field of it.

const (
	router  = "0x939bC0Bca6F9B9c52E6e3AD8A3C590b5d9B9D10E"
	wlux    = "0x4888E4a2Ee0F03051c72D2BD3ACf755eD3498B3E"
	lusd    = "0x848Cff46eb323f323b6Bbe1Df274E40793d7f2c2"
	cyrus   = "0x0A78f7Ce8D65e0FD4D6B78848483bA3C4fb895c5"
	trader  = "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"
	luxMain = 96369
)

// serveSwap runs a body through the handler as an HTTP request, so what the
// test exercises is the route a caller reaches and not a function beside it.
func serveSwap(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/swap", HandleSwap(luxMain, router, wlux))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/swap", strings.NewReader(body)))

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
	}
	return rec.Code, out
}

func swapCall(t *testing.T, body string) map[string]any {
	t.Helper()
	code, out := serveSwap(t, body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %v", code, out)
	}
	// requestId is a fresh correlator per call and is the one field that cannot
	// match a recording. It must still be there, and be a value.
	if id, ok := out["requestId"].(string); !ok || id == "" {
		t.Fatalf("requestId missing or empty: %v", out)
	}
	swap, ok := out["swap"].(map[string]any)
	if !ok {
		t.Fatalf("no swap object: %v", out)
	}
	return swap
}

func assertSwap(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("%s: missing from response", k)
			continue
		}
		if g != w {
			t.Errorf("%s:\n got  %v\n want %v", k, g, w)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: unexpected field %v — exchange-api did not send it", k, got[k])
		}
	}
}

// A single-hop exact-input trade, native in. exactInputSingle takes a fixed
// tuple, so every argument sits inline and the amountOutMinimum is the quoted
// output less the caller's 0.5%.
func TestSwapExactInputSingleMatchesRecording(t *testing.T) {
	got := swapCall(t, `{"quote":{
		"chainId":96369,
		"swapper":"`+trader+`",
		"input":{"token":"0x0000000000000000000000000000000000000000","amount":"1000000000000000000"},
		"output":{"token":"`+lusd+`","amount":"611022579164403","recipient":"`+trader+`"},
		"tradeType":"EXACT_INPUT",
		"slippage":0.5,
		"gasUseEstimate":"79990",
		"route":[[{"type":"v3-pool","address":"0x37011bB281676f85962fb35C674f7E9EB7584452",
			"tokenIn":{"address":"0x0000000000000000000000000000000000000000","chainId":96369,"symbol":"LUX","decimals":"18"},
			"tokenOut":{"address":"`+lusd+`","chainId":96369,"symbol":"LUSD","decimals":"18"},
			"fee":"3000","amountIn":"1000000000000000000","amountOut":"611022579164403"}]]
	}}`)

	assertSwap(t, got, map[string]any{
		"to":       router,
		"from":     trader,
		"value":    "1000000000000000000", // native in rides as msg.value
		"chainId":  float64(luxMain),
		"gasLimit": "79990",
		"data": "0x04e45aaf" +
			"0000000000000000000000004888e4a2ee0f03051c72d2bd3acf755ed3498b3e" + // tokenIn, sentinel wrapped
			"000000000000000000000000848cff46eb323f323b6bbe1df274e40793d7f2c2" + // tokenOut
			"0000000000000000000000000000000000000000000000000000000000000bb8" + // fee 3000
			"0000000000000000000000009011e888251ab053b7bd1cdb598db4f9ded94714" + // recipient
			"0000000000000000000000000000000000000000000000000de0b6b3a7640000" + // amountIn 1e18
			"000000000000000000000000000000000000000000000000000228f174dca7a4" + // amountOutMinimum
			"0000000000000000000000000000000000000000000000000000000000000000", // sqrtPriceLimitX96
	})
}

// Two hops. The path is a variable-length argument, so the call is offsets then
// values then the packed path — address, uint24 fee, address, uint24 fee,
// address — padded out to a whole word.
func TestSwapExactInputMultiHopMatchesRecording(t *testing.T) {
	got := swapCall(t, `{"quote":{
		"chainId":96369,
		"swapper":"`+trader+`",
		"input":{"token":"0x0000000000000000000000000000000000000000","amount":"1000000000000000000"},
		"output":{"token":"`+cyrus+`","amount":"10552437","recipient":"`+trader+`"},
		"tradeType":"EXACT_INPUT",
		"slippage":0.5,
		"gasUseEstimate":"159895",
		"route":[[
			{"type":"v3-pool","address":"0x37011bB281676f85962fb35C674f7E9EB7584452",
			 "tokenIn":{"address":"0x0000000000000000000000000000000000000000","chainId":96369,"symbol":"LUX","decimals":"18"},
			 "tokenOut":{"address":"`+lusd+`","chainId":96369,"symbol":"LUSD","decimals":"18"},
			 "fee":"3000","amountIn":"1000000000000000000","amountOut":"611022579164403"},
			{"type":"v3-pool","address":"0x0000000000000000000000000000000000000001",
			 "tokenIn":{"address":"`+lusd+`","chainId":96369,"symbol":"LUSD","decimals":"18"},
			 "tokenOut":{"address":"`+cyrus+`","chainId":96369,"symbol":"CYRUS","decimals":"6"},
			 "fee":"3000","amountIn":"611022579164403","amountOut":"10552437"}]]
	}}`)

	assertSwap(t, got, map[string]any{
		"to":       router,
		"from":     trader,
		"value":    "1000000000000000000",
		"chainId":  float64(luxMain),
		"gasLimit": "159895",
		"data": "0xb858183f" +
			"0000000000000000000000000000000000000000000000000000000000000020" + // offset to the tuple
			"0000000000000000000000000000000000000000000000000000000000000080" + // offset to path within it
			"0000000000000000000000009011e888251ab053b7bd1cdb598db4f9ded94714" + // recipient
			"0000000000000000000000000000000000000000000000000de0b6b3a7640000" + // amountIn
			"0000000000000000000000000000000000000000000000000000000000a0365a" + // amountOutMinimum
			"0000000000000000000000000000000000000000000000000000000000000042" + // path length, 66 bytes
			"4888e4a2ee0f03051c72d2bd3acf755ed3498b3e" + "000bb8" +
			"848cff46eb323f323b6bbe1df274e40793d7f2c2" + "000bb8" +
			"0a78f7ce8d65e0fd4d6b78848483ba3c4fb895c5" +
			"000000000000000000000000000000000000000000000000000000000000", // pad to a word
	})
}

// Exact output inverts the bound: the caller names what it wants out and the
// call carries the most it will pay in.
func TestSwapExactOutputSingleMatchesRecording(t *testing.T) {
	got := swapCall(t, `{"quote":{
		"chainId":96369,
		"swapper":"`+trader+`",
		"input":{"token":"0x0000000000000000000000000000000000000000","amount":"1636688789921865294"},
		"output":{"token":"`+lusd+`","amount":"1000000000000000","recipient":"`+trader+`"},
		"tradeType":"EXACT_OUTPUT",
		"slippage":0.5,
		"gasUseEstimate":"80746",
		"route":[[{"type":"v3-pool","address":"0x37011bB281676f85962fb35C674f7E9EB7584452",
			"tokenIn":{"address":"0x0000000000000000000000000000000000000000","chainId":96369,"symbol":"LUX","decimals":"18"},
			"tokenOut":{"address":"`+lusd+`","chainId":96369,"symbol":"LUSD","decimals":"18"},
			"fee":"3000","amountIn":"1636688789921865294","amountOut":"1000000000000000"}]]
	}}`)

	assertSwap(t, got, map[string]any{
		"to":       router,
		"from":     trader,
		"value":    "1636688789921865294",
		"chainId":  float64(luxMain),
		"gasLimit": "80746",
		"data": "0x5023b4df" +
			"0000000000000000000000004888e4a2ee0f03051c72d2bd3acf755ed3498b3e" +
			"000000000000000000000000848cff46eb323f323b6bbe1df274e40793d7f2c2" +
			"0000000000000000000000000000000000000000000000000000000000000bb8" +
			"0000000000000000000000009011e888251ab053b7bd1cdb598db4f9ded94714" +
			"00000000000000000000000000000000000000000000000000038d7ea4c68000" + // amountOut
			"00000000000000000000000000000000000000000000000016d3c294f0cfffbc" + // amountInMaximum
			"0000000000000000000000000000000000000000000000000000000000000000",
	})
}

// An ERC-20 input is transferred, not sent, so nothing rides as value.
func TestSwapErc20InputCarriesNoValue(t *testing.T) {
	got := swapCall(t, `{"quote":{
		"chainId":96369,
		"swapper":"`+trader+`",
		"input":{"token":"`+lusd+`","amount":"1000000000000000"},
		"output":{"token":"`+cyrus+`","amount":"10552437","recipient":"`+trader+`"},
		"tradeType":"EXACT_INPUT",
		"slippage":0.5,
		"route":[[{"type":"v3-pool","address":"0x0000000000000000000000000000000000000001",
			"tokenIn":{"address":"`+lusd+`","chainId":96369,"symbol":"LUSD","decimals":"18"},
			"tokenOut":{"address":"`+cyrus+`","chainId":96369,"symbol":"CYRUS","decimals":"6"},
			"fee":"3000","amountIn":"1000000000000000","amountOut":"10552437"}]]
	}}`)

	if got["value"] != "0" {
		t.Errorf("value = %v, want 0 for an ERC-20 input", got["value"])
	}
	// A quote with no gas estimate leaves the field off rather than sending 0.
	if _, present := got["gasLimit"]; present {
		t.Errorf("gasLimit present with no estimate on the quote: %v", got["gasLimit"])
	}
}

// Slippage is what makes the bound a bound. A wider tolerance must accept less
// out, and it must move in integer basis points rather than through a float.
func TestSwapSlippageWidensTheBound(t *testing.T) {
	body := func(slip string) string {
		return `{"quote":{"chainId":96369,"swapper":"` + trader + `",
			"input":{"token":"` + lusd + `","amount":"1000000000000000"},
			"output":{"token":"` + cyrus + `","amount":"10000000","recipient":"` + trader + `"},
			"tradeType":"EXACT_INPUT","slippage":` + slip + `,
			"route":[[{"type":"v3-pool","address":"0x0000000000000000000000000000000000000001",
				"tokenIn":{"address":"` + lusd + `","chainId":96369,"symbol":"LUSD","decimals":"18"},
				"tokenOut":{"address":"` + cyrus + `","chainId":96369,"symbol":"CYRUS","decimals":"6"},
				"fee":"3000","amountIn":"1000000000000000","amountOut":"10000000"}]]}}`
	}
	// exactInputSingle is seven inline words after the selector; amountOutMinimum
	// is the sixth. Reading it back as a number keeps the assertion about the
	// bound rather than about hex.
	for _, c := range []struct {
		slip string
		want int64
	}{
		{"0", 10_000_000},  // no tolerance: the bound is the quote itself
		{"0.5", 9_950_000}, // 0.5% of 10_000_000 is 50_000
		{"5", 9_500_000},   // 5% is 500_000
		{"100", 0},         // a tolerance of everything bounds nothing
	} {
		got := swapCall(t, body(c.slip))
		data, _ := got["data"].(string)
		w := words(strings.TrimPrefix(data, "0x")[8:])
		if len(w) != 7 {
			t.Fatalf("slippage %s: got %d words, want 7", c.slip, len(w))
		}
		if w[5].Int64() != c.want {
			t.Errorf("slippage %s: amountOutMinimum = %d, want %d", c.slip, w[5].Int64(), c.want)
		}
	}
}

// The failures a caller can cause must be told apart from a server fault.
func TestSwapRejectsBadRequests(t *testing.T) {
	for _, c := range []struct{ name, body, detail string }{
		{"no route", `{"quote":{"swapper":"` + trader + `","route":[]}}`,
			"quote.route must contain at least one hop"},
		{"no swapper", `{"quote":{"swapper":"nope","route":[[{"fee":"3000",
			"tokenIn":{"address":"` + lusd + `"},"tokenOut":{"address":"` + cyrus + `"}}]]}}`,
			"quote.swapper must be an address"},
		{"zero amounts", `{"quote":{"swapper":"` + trader + `",
			"input":{"token":"` + lusd + `","amount":"0"},"output":{"token":"` + cyrus + `","amount":"0"},
			"route":[[{"fee":"3000","tokenIn":{"address":"` + lusd + `"},"tokenOut":{"address":"` + cyrus + `"}}]]}}`,
			"quote input/output amounts must be > 0"},
		{"hop without fee", `{"quote":{"swapper":"` + trader + `",
			"input":{"token":"` + lusd + `","amount":"1"},"output":{"token":"` + cyrus + `","amount":"1"},
			"route":[[{"tokenIn":{"address":"` + lusd + `"},"tokenOut":{"address":"` + cyrus + `"}}]]}}`,
			"quote.route hop 0 is missing tokenIn/tokenOut/fee"},
		{"not JSON", `{`, "body must be JSON"},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, out := serveSwap(t, c.body)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", code)
			}
			if out["errorCode"] != "VALIDATION_ERROR" || out["detail"] != c.detail {
				t.Errorf("got %v, want detail %q", out, c.detail)
			}
		})
	}
}
