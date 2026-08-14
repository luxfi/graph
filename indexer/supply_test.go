package indexer

import (
	"context"
	"fmt"
	"testing"

	"github.com/luxfi/graph/storage"
)

// Circulating is what is left after everything that exists but cannot be sold:
// what the treasury has not distributed, and what is bonded to a validator. It
// is the difference between a market cap and a fully diluted value.
func TestCirculatingExcludesStake(t *testing.T) {
	// Lux as it actually stands: 2T minted, 994.7B still in the treasury,
	// 2.5B bonded. Reading the P-Chain's own counter instead reported 13.27B
	// as the supply and priced the token 150x under.
	n := NativeSupply{Minted: 2_000_000_000_000, Undistributed: 994_738_895_226, Staked: 2_500_000_002}
	if got := n.Circulating(); got != 1_002_761_104_772 {
		t.Errorf("circulating = %.0f, want 1002761104772", got)
	}
}

// Undistributed supply is not circulating supply. A treasury holding half the
// mint would otherwise double the reported market cap.
func TestTreasuryIsNotCirculating(t *testing.T) {
	n := NativeSupply{Minted: 1000, Undistributed: 500}
	if got := n.Circulating(); got != 500 {
		t.Errorf("circulating = %.0f, want 500", got)
	}
}

// A treasury that somehow exceeds the mint must not produce a negative supply,
// which would render as a negative market cap.
func TestCirculatingNeverNegative(t *testing.T) {
	n := NativeSupply{Minted: 100, Undistributed: 200}
	if got := n.Circulating(); got != 0 {
		t.Errorf("circulating = %.0f, want 0", got)
	}
}

// A chain with no validators of its own has nothing bonded, so all of its
// supply circulates. That is a real answer, not a missing one.
func TestNoStakeMeansAllCirculates(t *testing.T) {
	n := NativeSupply{Minted: 1000}
	if got := n.Circulating(); got != 1000 {
		t.Errorf("circulating = %.0f, want 1000", got)
	}
}

// The P-Chain sits beside the EVM chain under one node, so its endpoint is
// derived rather than configured — nothing to set, nothing to drift.
func TestPlatformEndpointDerivesFromEVM(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://api.lux.network/v1/bc/C/rpc", "https://api.lux.network/v1/bc/P"},
		{"http://luxd:9630/v1/bc/C/rpc", "http://luxd:9630/v1/bc/P"},
	} {
		got, ok := platformEndpoint(c.in)
		if !ok || got != c.want {
			t.Errorf("platformEndpoint(%q) = %q,%v; want %q", c.in, got, ok, c.want)
		}
	}
	// An endpoint that names no chain cannot be turned into one, and guessing
	// would point supply reads at whatever answered.
	if _, ok := platformEndpoint("https://example.com/rpc"); ok {
		t.Error("a URL with no /bc/ must not yield a platform endpoint")
	}
}

// Whole units, no exponent: two trillion in scientific notation parses to a
// different number in some clients and to NaN in others.
func TestUnitsAreWrittenPlainly(t *testing.T) {
	if got := fmtUnits(2_000_000_000_000); got != "2000000000000" {
		t.Errorf("fmtUnits = %q, want 2000000000000", got)
	}
}

// The coin's minted supply survives a copy of its row written after the publish.
//
// The valuation pass reads every token into memory at the top and writes those
// copies back near the end. A supply written only to the store sits inside that
// window: the copy read before it lands on top, and the chain's own coin then
// reports whatever its wrapper holds wrapped — Zoo's two trillion showed as the
// fifteen billion sitting in the contract.
func TestNativeSupplySurvivesAStaleCopy(t *testing.T) {
	const wrapper = "0x4888e4a2ee0f03051c72d2bd3acf755ed3498b3e"
	s := newMemSQLiteStore(t)
	// The wrapper as the chain reports it: what has actually been wrapped.
	s.SeedToken(wrapper, &storage.SeedTokenData{
		Symbol: "WLUX", Name: "Wrapped LUX", Decimals: 18, TotalSupply: "15026903324",
	})
	idx := NewWithConfig(Config{
		RPC:           chainAt(t, 1767225600, nil).URL,
		Native:        wrapper,
		GenesisSupply: "2000000000000",
	}, s)

	// What the pass does: read the rows, publish the supply, write the rows back.
	inFlight := s.TokensRaw()
	idx.publishNativeSupply(context.Background(), inFlight)
	for addr, t := range inFlight {
		s.SeedToken(addr, t)
	}

	got := s.TokensRaw()[wrapper]
	if got == nil {
		t.Fatal("the wrapper's row vanished")
	}
	if got.TotalSupply != "2000000000000" {
		t.Errorf("supply = %q, want the minted 2000000000000 — a copy read before the publish landed on top", got.TotalSupply)
	}
}

// Volume for all time is the whole table, not the window a pass reads.
//
// A pass values a bounded slice of the swap table because that table grows
// forever. The bound is right for the pass and wrong for the field the wire
// calls total volume. Summing the day series does not escape it — those rows
// are folded from the same window, so their sum is that window again. This
// gives the store more trades than any window would carry and asks it what has
// traded.
func TestFactoryVolumeCoversEveryTrade(t *testing.T) {
	s := newMemSQLiteStore(t)

	for i, usd := range []string{"1000.00", "2000.00", "500.00"} {
		s.SeedSwap(fmt.Sprintf("0xabc#%d", i), &storage.SeedSwapData{
			Timestamp: int64(1767225600 + i*86400),
			Block:     uint64(100 + i),
			Pool:      "0xpool",
			AmountUSD: usd,
		})
	}

	traded, volume, err := s.Traded()
	if err != nil {
		t.Fatalf("Traded: %v", err)
	}
	if traded != 3 {
		t.Errorf("trades = %d, want 3", traded)
	}
	if volume != 3500 {
		t.Errorf("volume = %v, want 3500 \u2014 a trade outside the window still traded", volume)
	}
}
