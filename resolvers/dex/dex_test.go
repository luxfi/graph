package dex

import "testing"

// LOW (query-DoS): the DEX (CLOB) resolvers must clamp `first` exactly like every
// other resolver namespace — default 100, floor 1→100, hard cap 1000. An
// unbounded `first` would let a single query pull the entire table.
func TestParseLimit_Caps(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want int
	}{
		{"absent defaults to 100", map[string]interface{}{}, 100},
		{"in range honored", map[string]interface{}{"first": 250}, 250},
		{"string in range honored", map[string]interface{}{"first": "250"}, 250},
		{"over cap clamped to 1000", map[string]interface{}{"first": 1_000_000}, 1000},
		{"string over cap clamped", map[string]interface{}{"first": "999999999"}, 1000},
		{"exactly cap honored", map[string]interface{}{"first": 1000}, 1000},
		{"zero floors to 100", map[string]interface{}{"first": 0}, 100},
		{"negative floors to 100", map[string]interface{}{"first": -5}, 100},
		{"unparseable floors to 100", map[string]interface{}{"first": "not-a-number"}, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLimit(tc.args); got != tc.want {
				t.Errorf("parseLimit(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
