package indexer

import "testing"

// Staked units exist but cannot be sold. Circulating is what is left, and it is
// the difference between a market cap and a fully diluted value — report the
// diluted figure as market cap and you overstate it by everything bonded to a
// validator. Lux had 2.5B of 13.27B staked when this was written: an 18.8%
// error, silently.
func TestCirculatingExcludesStake(t *testing.T) {
	n := NativeSupply{Total: 13_272_095_200, Staked: 2_500_000_002}
	if got := n.Circulating(); got != 10_772_095_198 {
		t.Errorf("circulating = %.0f, want 10772095198", got)
	}
}

// A chain with no validators of its own has nothing bonded, so all of its
// supply circulates. That is a real answer, not a missing one.
func TestNoStakeMeansAllCirculates(t *testing.T) {
	n := NativeSupply{Total: 1000}
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

// Whole units, no exponent: 13.27 billion in scientific notation parses to a
// different number in some clients and to NaN in others.
func TestUnitsAreWrittenPlainly(t *testing.T) {
	if got := fmtUnits(13_272_095_200); got != "13272095200" {
		t.Errorf("fmtUnits = %q, want 13272095200", got)
	}
}
