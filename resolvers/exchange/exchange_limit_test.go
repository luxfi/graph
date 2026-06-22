package exchange

import "testing"

// LOW (query-DoS): the resolver limit must clamp `first` exactly like the dex
// namespace — default 100, floor <1→100, hard cap 1000. Before the fix this
// namespace's pl() capped at 1000 but had NO floor, so a negative/zero `first`
// flowed straight to ListByType (and `first:-1` is "no limit" in SQLite — an
// unbounded scan). This proves the floor now matches dex.
func TestPL_ClampsFirst(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want int
	}{
		{"absent defaults to 100", map[string]interface{}{}, 100},
		{"in range honored", map[string]interface{}{"first": 250}, 250},
		{"over cap clamped to 1000", map[string]interface{}{"first": 1_000_000}, 1000},
		{"exactly cap honored", map[string]interface{}{"first": 1000}, 1000},
		{"zero floors to 100", map[string]interface{}{"first": 0}, 100},
		{"negative floors to 100", map[string]interface{}{"first": -1}, 100},
		{"unparseable floors to 100", map[string]interface{}{"first": "not-a-number"}, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pl(tc.args); got != tc.want {
				t.Errorf("pl(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
