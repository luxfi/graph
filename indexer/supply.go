package indexer

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
)

// The native token lives on two chains at once, and a supply figure that reads
// only one of them is wrong in a way nobody can see.
//
// The EVM side is what an ERC-20 reports: WLUX's totalSupply() is how much LUX
// has been wrapped, not how much exists. The whole supply, and the part of it
// locked in validators, are facts of the primary network — the P-Chain keeps
// both, and answers in nLUX, nine decimals, not the eighteen an EVM uses.
//
// Fully diluted value is price times the whole supply. Market cap is price
// times what actually circulates, which is the supply less what is staked: that
// LUX is bonded to a validator and cannot be sold without unstaking first. The
// figures are read from the chain, not configured, so they follow every
// delegation and every reward without anyone editing a list.
const nLUX = 1e9

// fmtUnits writes a whole-token amount without exponent or separators, so the
// client parses it as a number rather than guessing at a format.
func fmtUnits(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// NativeSupply is what the primary network says about its own token.
type NativeSupply struct {
	// Total is every unit in existence, staked or not.
	Total float64
	// Staked is bonded to a validator or a delegation, and cannot be sold
	// while it is.
	Staked float64
}

// Circulating is what can actually change hands.
func (n NativeSupply) Circulating() float64 { return n.Total - n.Staked }

// primaryNetworkID is the ID every chain's own primary network answers to. It
// is the same string on every network we run; the chain differs, the primary
// network's identifier does not.
const primaryNetworkID = "11111111111111111111111111111111LpoYY"

// readNativeSupply asks the P-Chain what exists and what is bonded.
//
// The endpoint is derived from the EVM one the indexer already holds: the two
// chains sit side by side under the same node — /v1/bc/C/rpc and /v1/bc/P — so
// there is nothing new to configure and nothing that can drift out of step with
// the chain being indexed.
func (idx *Indexer) readNativeSupply(ctx context.Context) (NativeSupply, bool) {
	base, ok := platformEndpoint(idx.rpc)
	if !ok {
		return NativeSupply{}, false
	}
	total, ok := idx.platformCall(ctx, base, "platform.getCurrentSupply", map[string]any{}, "supply")
	if !ok {
		return NativeSupply{}, false
	}
	// A chain with no validators of its own answers zero, which is honest: it
	// has nothing staked, so all of its supply circulates.
	staked, _ := idx.platformCall(ctx, base, "platform.getTotalStake",
		map[string]any{"subnetID": primaryNetworkID}, "stake")
	return NativeSupply{Total: total, Staked: staked}, true
}

// platformEndpoint turns a C-Chain RPC URL into its P-Chain sibling.
func platformEndpoint(evmRPC string) (string, bool) {
	i := strings.Index(evmRPC, "/bc/")
	if i < 0 {
		return "", false
	}
	return evmRPC[:i] + "/bc/P", true
}

// platformCall issues one platform.* call and reads a single nLUX field.
func (idx *Indexer) platformCall(ctx context.Context, url, method string, params map[string]any, field string) (float64, bool) {
	raw, err := idx.rpcCallTo(ctx, url, method, params)
	if err != nil {
		return 0, false
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil {
		return 0, false
	}
	var s string
	if json.Unmarshal(out[field], &s) != nil {
		return 0, false
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return 0, false
	}
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(n), big.NewFloat(nLUX)).Float64()
	return f, true
}

// publishNativeSupply records what the primary network says about its own token
// on the wrapped native token's entity — the one a token page opens for LUX.
//
// Written each pass rather than once at start: supply grows with every staking
// reward and the staked figure moves with every delegation, so a value read at
// boot would be quietly stale within the hour.
func (idx *Indexer) publishNativeSupply(ctx context.Context) {
	if idx.native == "" {
		return
	}
	n, ok := idx.readNativeSupply(ctx)
	if !ok || n.Total <= 0 {
		return
	}
	tok := idx.store.TokensRaw()[idx.native]
	if tok == nil {
		return
	}
	tok.TotalSupply = fmtUnits(n.Total)
	tok.Staked = fmtUnits(n.Staked)
	idx.logf("[supply] native %s: %s total, %s staked, %s circulating",
		tok.Symbol, fmtUnits(n.Total), fmtUnits(n.Staked), fmtUnits(n.Circulating()))
	idx.store.SeedToken(idx.native, tok)
}
