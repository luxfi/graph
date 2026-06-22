package fhe

import "testing"

// LOW (query-DoS): parseLimit must clamp `first` exactly like the dex namespace —
// default 100, floor <1→100, hard cap 1000. Before the fix this namespace's
// parseLimit had NEITHER a floor NOR a cap (`return limit`), so any `first` flowed
// straight to ListByType — an unbounded scan (and `first:-1` = "no limit" in
// SQLite). This proves both the floor and the cap now match dex.
func TestParseLimit_ClampsFirst(t *testing.T) {
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
			if got := parseLimit(tc.args); got != tc.want {
				t.Errorf("parseLimit(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
