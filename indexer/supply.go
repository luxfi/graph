package indexer

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
)

// What a native token is worth depends on three numbers, and they live in three
// different places. Getting the first one wrong is how a two-trillion-supply
// token reported a market cap of $4.8M.
//
//	minted        — every unit created at block 0. A fact of genesis, not a
//	                query: the alloc is not exposed over RPC and summing every
//	                account is not something a node will do for you.
//	undistributed — what the treasury still holds, read live, because it falls
//	                as tokens reach holders. Lux minted 2T to one address and
//	                that address holds 994.7B today; the trillion that left is
//	                exactly the point.
//	staked        — bonded to a validator on the primary network. It has reached
//	                holders but cannot be sold without unstaking first.
//
// Circulating is what is left, and it is the only sound basis for a market cap.
// Fully diluted value uses the whole minted supply, which is why they differ.
//
// The trap worth naming: platform.getCurrentSupply is NOT the token's supply.
// It is the P-Chain's own counter, and on Lux it reads 13.27 billion against a
// real supply of two trillion. Trusting it put the market cap out by 150x.
const nLUX = 1e9

// fmtUnits writes a whole-token amount without exponent or separators, so the
// client parses it as a number rather than guessing at a format.
func fmtUnits(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// NativeSupply is what the chain says about its own token, in whole units.
type NativeSupply struct {
	// Minted is every unit created at genesis.
	Minted float64
	// Undistributed is what the treasury still holds.
	Undistributed float64
	// Staked is bonded to a validator.
	Staked float64
}

// Circulating is what can actually change hands.
func (n NativeSupply) Circulating() float64 {
	c := n.Minted - n.Undistributed - n.Staked
	if c < 0 {
		return 0
	}
	return c
}

// primaryNetworkID is the ID every chain's own primary network answers to.
const primaryNetworkID = "11111111111111111111111111111111LpoYY"

// readNativeSupply gathers the three figures.
func (idx *Indexer) readNativeSupply(ctx context.Context) (NativeSupply, bool) {
	minted, err := strconv.ParseFloat(idx.genesisSupply, 64)
	if err != nil || minted <= 0 {
		return NativeSupply{}, false // no declared supply ⇒ say nothing
	}
	out := NativeSupply{Minted: minted}

	if idx.treasury != "" {
		if bal, ok := idx.nativeBalance(ctx, idx.treasury); ok {
			out.Undistributed = bal
		}
	}
	if base, ok := platformEndpoint(idx.rpc); ok {
		// A chain with no validators of its own answers zero, which is honest.
		out.Staked, _ = idx.platformCall(ctx, base, "platform.getTotalStake",
			map[string]any{"subnetID": primaryNetworkID}, "stake")
	}
	return out, true
}

// nativeBalance reads an account's native balance in whole units.
func (idx *Indexer) nativeBalance(ctx context.Context, addr string) (float64, bool) {
	raw, err := idx.rpcCall(ctx, "eth_getBalance", []interface{}{addr, "latest"})
	if err != nil {
		return 0, false
	}
	var hex string
	if json.Unmarshal(raw, &hex) != nil {
		return 0, false
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(hex, "0x"), 16)
	if !ok {
		return 0, false
	}
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(n), big.NewFloat(1e18)).Float64()
	return f, true
}

// platformEndpoint turns a C-Chain RPC URL into its P-Chain sibling. The two
// chains sit beside each other under the same node, so what is staked is one
// derived URL away and nothing needs configuring.
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

// publishNativeSupply records the figures on the wrapped native token — the
// entity a token page opens for LUX.
//
// Written each pass rather than once: the treasury balance falls as tokens are
// distributed and the staked figure moves with every delegation, so a value
// read at boot would be quietly stale within the hour.
func (idx *Indexer) publishNativeSupply(ctx context.Context) {
	if idx.native == "" {
		return
	}
	n, ok := idx.readNativeSupply(ctx)
	if !ok {
		return
	}
	tok := idx.store.TokensRaw()[idx.native]
	if tok == nil {
		return
	}
	tok.TotalSupply = fmtUnits(n.Minted)
	// One field for everything that exists but cannot be sold, because that is
	// the single quantity the market-cap subtraction needs.
	tok.Staked = fmtUnits(n.Staked + n.Undistributed)
	idx.store.SeedToken(idx.native, tok)
	idx.logf("[supply] native %s: %s minted, %s undistributed, %s staked, %s circulating",
		tok.Symbol, fmtUnits(n.Minted), fmtUnits(n.Undistributed),
		fmtUnits(n.Staked), fmtUnits(n.Circulating()))
}
