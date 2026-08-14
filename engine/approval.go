package engine

import (
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
)

// Whether the router may already spend this token, and the transaction that
// lets it if not.
//
// An ERC-20 moves only on the owner's say-so, so a swap of one is two
// transactions: grant, then trade. Asking first is what keeps the swap form
// from showing a second signature prompt to someone who granted it months ago,
// and from omitting one for someone who never did.
//
// Two cases need no approval and both answer null rather than an amount of
// zero: the chain's own coin, which is sent with the call and never spent from
// a balance, and an allowance that already covers the trade.

// unlimited is what an approval grants: every value a word holds. A person
// approves a router once and trades against it for as long as they use it; an
// approval sized to the trade in hand would put a signature in front of every
// swap after it.
var unlimited = maxWord

var (
	selAllowance = selector("allowance(address,address)")
	selApprove   = selector("approve(address,uint256)")
)

type approvalRequest struct {
	WalletAddress string `json:"walletAddress"`
	Token         string `json:"token"`
	Amount        string `json:"amount"`
	ChainID       *int64 `json:"chainId"`
}

type approvalResponse struct {
	RequestID string      `json:"requestId"`
	Approval  *approvalTx `json:"approval"`
}

type approvalTx struct {
	To      string `json:"to"`
	From    string `json:"from"`
	Data    string `json:"data"`
	Value   string `json:"value"`
	ChainID int64  `json:"chainId"`
}

// HandleCheckApproval answers whether the router can spend a token on the
// caller's behalf. router is the same SwapRouter02 HandleSwap addresses — the
// approval has to name the contract that will actually do the spending, so the
// two endpoints read one address or the grant is useless.
func HandleCheckApproval(chainID int64, rpc, router string) http.HandlerFunc {
	n := dial(rpc)
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req approvalRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badQuote(w, "body must be JSON")
			return
		}

		if !isHexAddress(req.WalletAddress) {
			badQuote(w, "walletAddress must be an address")
			return
		}
		if req.Token == "" {
			badQuote(w, "token is required")
			return
		}
		amount, ok := parseWei(req.Amount)
		if !ok {
			badQuote(w, "amount must be a decimal wei string")
			return
		}
		if req.ChainID != nil && *req.ChainID != chainID {
			badQuote(w, "unsupported chainId "+strconv.FormatInt(*req.ChainID, 10))
			return
		}

		wallet := checksumAddress(req.WalletAddress)
		none := approvalResponse{RequestID: requestID()}

		// The coin itself is not an ERC-20 and has nothing to approve.
		if isNativeAddr(req.Token) {
			writeJSON(w, http.StatusOK, none)
			return
		}
		if !isHexAddress(req.Token) {
			badQuote(w, "token must be an address")
			return
		}
		token := checksumAddress(req.Token)

		res, err := n.call(r.Context(), []ethCall{{
			To:   token,
			Data: selAllowance + addrWord(wallet) + addrWord(router),
		}})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"errorCode": "INTERNAL_ERROR",
				"detail":    "Internal server error",
			})
			return
		}

		// A token that answers nothing has granted nothing. Reading silence as
		// an allowance would skip the approval and leave the swap to revert.
		allowance := new(big.Int)
		if got := words(res[0]); len(got) > 0 {
			allowance = got[0]
		}
		if allowance.Cmp(amount) >= 0 {
			writeJSON(w, http.StatusOK, none)
			return
		}

		writeJSON(w, http.StatusOK, approvalResponse{
			RequestID: requestID(),
			Approval: &approvalTx{
				To:      token,
				From:    wallet,
				Data:    selApprove + addrWord(router) + numWord(unlimited),
				Value:   "0",
				ChainID: chainID,
			},
		})
	}
}
