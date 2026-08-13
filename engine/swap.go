package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
)

// The transaction a person signs to take the quote they were shown.
//
// This asks the chain nothing. Everything it needs is in the quote it is handed
// — the route, the amounts, the fee of each hop — and the only work is to say
// that in the router's own language and bound it by the slippage the caller
// accepted. Reading state here would be a second opinion about a price the
// caller has already agreed to, arriving a block later than the one they saw.
//
// The bound is the whole point of the endpoint. An exact-input trade names the
// least it will accept out; an exact-output trade names the most it will pay
// in. Past that limit the router reverts, which is the difference between a bad
// fill and no fill.
//
// There is no Permit2 here, so the router spends against a plain ERC-20
// approval and `permitData` on a quote is always null. HandleCheckApproval is
// the other half of that.

var (
	selExactInputSingle  = selector("exactInputSingle((address,address,uint24,address,uint256,uint256,uint160))")
	selExactInput        = selector("exactInput((bytes,address,uint256,uint256))")
	selExactOutputSingle = selector("exactOutputSingle((address,address,uint24,address,uint256,uint256,uint160))")
	selExactOutput       = selector("exactOutput((bytes,address,uint256,uint256))")
)

// ── the wire ──────────────────────────────────────────────────────────────

type swapRequest struct {
	Quote swapQuote `json:"quote"`
}

// swapQuote is the part of a quote this endpoint reads back. It is deliberately
// a subset: the caller returns the quote it was given, and the fields not named
// here are for the person looking at it, not for building the call.
type swapQuote struct {
	ChainID   int64       `json:"chainId"`
	Swapper   string      `json:"swapper"`
	Input     quoteSide   `json:"input"`
	Output    quoteSide   `json:"output"`
	TradeType string      `json:"tradeType"`
	Slippage  *float64    `json:"slippage"`
	Route     [][]hopJSON `json:"route"`
	// GasUseEstimate rides along to the wallet as a gas limit. A quote that
	// carried none leaves the field off entirely rather than sending a zero,
	// which a wallet would read as "this transaction may use no gas".
	GasUseEstimate string `json:"gasUseEstimate"`
}

type swapResponse struct {
	RequestID string       `json:"requestId"`
	Swap      swapCallJSON `json:"swap"`
}

type swapCallJSON struct {
	To       string `json:"to"`
	From     string `json:"from"`
	Data     string `json:"data"`
	Value    string `json:"value"`
	ChainID  int64  `json:"chainId"`
	GasLimit string `json:"gasLimit,omitempty"`
}

// ── the handler ───────────────────────────────────────────────────────────

// HandleSwap builds the router call for a quote. router is this chain's V3
// periphery SwapRouter02 — the contract the calldata is addressed to and the
// contract check_approval asks about, so both endpoints name one address.
func HandleSwap(chainID int64, router, wrapped string) http.HandlerFunc {
	wrapped = strings.ToLower(strings.TrimSpace(wrapped))
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req swapRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badQuote(w, "body must be JSON")
			return
		}
		q := req.Quote

		if len(q.Route) == 0 || len(q.Route[0]) == 0 {
			badQuote(w, "quote.route must contain at least one hop")
			return
		}
		if !isHexAddress(q.Swapper) {
			badQuote(w, "quote.swapper must be an address")
			return
		}
		swapper := checksumAddress(q.Swapper)

		amountIn, okIn := parseWei(q.Input.Amount)
		amountOut, okOut := parseWei(q.Output.Amount)
		if !okIn || !okOut || amountIn.Sign() <= 0 || amountOut.Sign() <= 0 {
			badQuote(w, "quote input/output amounts must be > 0")
			return
		}

		// The route names the tokens; the native sentinel among them routes as
		// the wrapped token, exactly as the quote priced it.
		hops := q.Route[0]
		tokens := make([]string, 0, len(hops)+1)
		fees := make([]int64, 0, len(hops))
		for i, h := range hops {
			fee, err := strconv.ParseInt(h.Fee, 10, 64)
			if h.TokenIn.Address == "" || h.TokenOut.Address == "" || h.Fee == "" || err != nil {
				badQuote(w, "quote.route hop "+strconv.Itoa(i)+" is missing tokenIn/tokenOut/fee")
				return
			}
			if i == 0 {
				tokens = append(tokens, wrap(h.TokenIn.Address, wrapped))
			}
			tokens = append(tokens, wrap(h.TokenOut.Address, wrapped))
			fees = append(fees, fee)
		}

		slippage := 0.5
		if q.Slippage != nil {
			slippage = *q.Slippage
		}
		exactIn := q.TradeType != "EXACT_OUTPUT"

		var data string
		switch {
		case exactIn && len(hops) == 1:
			data = selExactInputSingle +
				addrWord(tokens[0]) + addrWord(tokens[1]) + numWord(big.NewInt(fees[0])) +
				addrWord(swapper) + numWord(amountIn) + numWord(down(amountOut, slippage)) +
				numWord(zero)
		case exactIn:
			// A multi-hop exact-input path reads the way the trade travels.
			data = selExactInput + pathCall(encodePath(tokens, fees), swapper, amountIn, down(amountOut, slippage))
		case len(hops) == 1:
			data = selExactOutputSingle +
				addrWord(tokens[0]) + addrWord(tokens[1]) + numWord(big.NewInt(fees[0])) +
				addrWord(swapper) + numWord(amountOut) + numWord(up(amountIn, slippage)) +
				numWord(zero)
		default:
			// A multi-hop exact-output path is encoded backwards: the router
			// works out what each hop must receive by walking from the amount
			// asked for back to the amount paid.
			data = selExactOutput + pathCall(encodePath(reverse(tokens), reverse(fees)), swapper, amountOut, up(amountIn, slippage))
		}

		// Native input is not transferred, it is sent. The router wraps whatever
		// arrives with the call, so the value carries the input amount and the
		// path still names the wrapped token.
		value := "0"
		if isNativeAddr(q.Input.Token) {
			value = amountIn.String()
		}

		writeJSON(w, http.StatusOK, swapResponse{
			RequestID: requestID(),
			Swap: swapCallJSON{
				To:       checksumAddress(router),
				From:     swapper,
				Data:     data,
				Value:    value,
				ChainID:  chainID,
				GasLimit: q.GasUseEstimate,
			},
		})
	}
}

// ── amounts ───────────────────────────────────────────────────────────────

// Slippage is a percent, and it is applied in integer basis points because the
// alternative is a float multiplying someone's balance.

func down(amount *big.Int, pct float64) *big.Int {
	f := 10000 - bps(pct)
	if f < 0 {
		f = 0
	}
	return div10k(amount, f)
}

func up(amount *big.Int, pct float64) *big.Int {
	return div10k(amount, 10000+bps(pct))
}

func bps(pct float64) int64 { return int64(math.Round(pct * 100)) }

func div10k(amount *big.Int, factor int64) *big.Int {
	n := new(big.Int).Mul(amount, big.NewInt(factor))
	return n.Div(n, big.NewInt(10000))
}

// ── ABI ───────────────────────────────────────────────────────────────────

// pathCall lays out the three (bytes path, address, uint256, uint256) calls.
// The struct holds a variable-length path, so the argument is a pointer to a
// tuple that is itself a header and a tail — hence two offsets before the
// values and the path last.
func pathCall(path string, recipient string, amount, limit *big.Int) string {
	const tupleAt, pathAt = 0x20, 0x80
	bytes := len(path) / 2
	pad := (wordHex - len(path)%wordHex) % wordHex
	return numWord(big.NewInt(tupleAt)) +
		numWord(big.NewInt(pathAt)) +
		addrWord(recipient) +
		numWord(amount) +
		numWord(limit) +
		numWord(big.NewInt(int64(bytes))) +
		path + strings.Repeat("0", pad)
}

// encodePath packs a route the way the router reads it: each address followed
// by the fee of the pool it enters, with no padding between them. An address is
// 20 bytes and a fee is a uint24, so the widths are fixed and position alone
// says which is which — a fee written any narrower would shift every byte after
// it and address a different pool.
func encodePath(tokens []string, fees []int64) string {
	var b strings.Builder
	for i, t := range tokens {
		b.WriteString(strings.ToLower(strings.TrimPrefix(t, "0x")))
		if i < len(fees) {
			b.WriteString(fmt.Sprintf("%06x", fees[i]))
		}
	}
	return b.String()
}

func reverse[T any](in []T) []T {
	out := make([]T, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

// ── addresses ─────────────────────────────────────────────────────────────

func isNativeAddr(addr string) bool {
	return strings.EqualFold(strings.TrimSpace(addr), nativeSentinel)
}

// wrap substitutes the wrapped token for the native sentinel. A pool holds the
// wrapped token; the sentinel is only ever a way for a caller to say "the coin".
func wrap(addr, wrapped string) string {
	if isNativeAddr(addr) {
		return wrapped
	}
	return strings.ToLower(addr)
}
