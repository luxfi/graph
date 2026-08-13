package indexer

import "testing"

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
