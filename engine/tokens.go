package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/luxfi/graph/storage"
)

// The list of tokens a person can pick in the swap form.
//
// It was served by exchange-api: a TypeScript forwarder deployed once per brand
// per network, whose answer came from the same indexer this binary IS. The list
// is the token table, so serving it from anywhere else is a copy that can
// disagree — and did, on supply, for two days.
//
// The shape is exchange-api's, because the client already reads it and a client
// is not something to break while moving a service. Everything in it is either
// indexed or derived; nothing is invented.

// swapToken is one row as the swap form expects it.
type swapToken struct {
	Address  string      `json:"address"`
	ChainID  int64       `json:"chainId"`
	Name     string      `json:"name"`
	Symbol   string      `json:"symbol"`
	Decimals int64       `json:"decimals"`
	IsSpam   bool        `json:"isSpam"`
	Project  swapProject `json:"project"`
}

type swapProject struct {
	Logo        swapLogo `json:"logo"`
	SafetyLevel string   `json:"safetyLevel"`
	IsSpam      bool     `json:"isSpam"`
}

type swapLogo struct {
	URL string `json:"url"`
}

// iconURL is where a token's mark lives. One host, one lowercase symbol, one
// extension — a token needs no per-token configuration to have a picture, and
// the client already falls back to a lettermark when the fetch fails, so a
// missing file costs nothing.
func iconURL(symbol string) string {
	return "https://cdn.lux.network/exchange/icon-png/" + strings.ToLower(symbol) + ".png"
}

// requestID is echoed by the client in its logs, so it exists to be correlated
// and nothing more.
func requestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// HandleSwappableTokens answers the swap form's token list for one chain.
func HandleSwappableTokens(store *storage.Store, chainID int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The client sends the chain it is looking at. Honour it when it does;
		// this process indexes one chain, so a request for another is a question
		// it cannot answer and should not answer wrongly.
		id := chainID
		if q := r.URL.Query().Get("chainId"); q != "" {
			n, err := strconv.ParseInt(q, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"errorCode": "VALIDATION_ERROR",
					"detail":    "chainId must be an integer",
				})
				return
			}
			if n != chainID {
				writeJSON(w, http.StatusNotFound, map[string]any{
					"errorCode": "UNKNOWN_CHAIN",
					"detail":    "this indexer serves chain " + strconv.FormatInt(chainID, 10),
				})
				return
			}
			id = n
		}

		raw := store.TokensRaw()
		out := make([]swapToken, 0, len(raw))
		for addr, t := range raw {
			if t == nil || t.Symbol == "" {
				continue
			}
			out = append(out, swapToken{
				Address:  addr,
				ChainID:  id,
				Name:     t.Name,
				Symbol:   t.Symbol,
				Decimals: t.Decimals,
				Project: swapProject{
					Logo: swapLogo{URL: iconURL(t.Symbol)},
					// Every token here was found by following this chain's own
					// factory, so it is on the chain by construction. That is a
					// weaker claim than a curated allowlist and the honest one.
					SafetyLevel: "VERIFIED",
				},
			})
		}
		// A stable order, so a reload does not reshuffle the list under someone
		// mid-selection. Map iteration alone would.
		sort.Slice(out, func(i, j int) bool {
			if out[i].Symbol != out[j].Symbol {
				return out[i].Symbol < out[j].Symbol
			}
			return out[i].Address < out[j].Address
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"requestId": requestID(),
			"tokens":    out,
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// chainIDFromRPC asks the chain what it is.
//
// Nothing else can answer without being able to disagree: a flag, an env var or
// a table keyed by deployment is a second copy of a fact the node already holds,
// and the whole reason the exchange served Zoo's tokens under Lux's name was two
// copies of "which chain is this" drifting apart.
// ChainIDFromRPC asks the chain what it is.
func ChainIDFromRPC(ctx context.Context, rpc string) (int64, error) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpc, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(out.Result, "0x"), 16)
	if !ok {
		return 0, errBadChainID
	}
	return n.Int64(), nil
}

// errBadChainID is returned when eth_chainId answers something that is not a
// number, which means the endpoint is not the chain it claims to be.
var errBadChainID = errorString("eth_chainId did not return a hex number")

type errorString string

func (e errorString) Error() string { return string(e) }
