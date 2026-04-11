package engine

import (
	"testing"
)

func TestParseTopFields_SingleField(t *testing.T) {
	fields, err := parseTopFields(`{ tokens(first: 10) { id symbol } }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if fields[0].name != "tokens" {
		t.Errorf("expected tokens, got %s", fields[0].name)
	}
	if fields[0].args["first"] != "10" {
		t.Errorf("expected first=10, got %v", fields[0].args["first"])
	}
}

func TestParseTopFields_MultipleFields(t *testing.T) {
	fields, err := parseTopFields("{\n  bundle(id: \"1\") { ethPriceUSD }\n  factories(first: 1) { poolCount }\n}")
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[0].name != "bundle" {
		t.Errorf("expected bundle, got %s", fields[0].name)
	}
	if fields[1].name != "factories" {
		t.Errorf("expected factories, got %s", fields[1].name)
	}
}

func TestParseTopFields_Alias(t *testing.T) {
	fields, err := parseTopFields("{\n  myTokens: tokens(first: 5) { id }\n}")
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if fields[0].alias != "myTokens" {
		t.Errorf("expected alias myTokens, got %s", fields[0].alias)
	}
	if fields[0].name != "tokens" {
		t.Errorf("expected name tokens, got %s", fields[0].name)
	}
}

func TestParseArgs_WhereFilter(t *testing.T) {
	args := parseArgs(`first: 200, orderBy: timestamp, orderDirection: desc, where: { pool: "0xabc", date_gte: 12345 }`)

	if args["first"] != "200" {
		t.Errorf("first: got %v", args["first"])
	}
	if args["orderBy"] != "timestamp" {
		t.Errorf("orderBy: got %v", args["orderBy"])
	}
	if args["orderDirection"] != "desc" {
		t.Errorf("orderDirection: got %v", args["orderDirection"])
	}

	where, ok := args["where"].(map[string]interface{})
	if !ok {
		t.Fatalf("where not a map: %T", args["where"])
	}
	if where["pool"] != "0xabc" {
		t.Errorf("where.pool: got %v", where["pool"])
	}
	if where["date_gte"] != "12345" {
		t.Errorf("where.date_gte: got %v", where["date_gte"])
	}
}

func TestParseArgs_NestedBraces(t *testing.T) {
	args := parseArgs(`where: { token: { id: "0x123" } }`)
	where, ok := args["where"].(map[string]interface{})
	if !ok {
		t.Fatal("where not a map")
	}
	inner, ok := where["token"].(map[string]interface{})
	if !ok {
		t.Fatal("where.token not a map")
	}
	if inner["id"] != "0x123" {
		t.Errorf("expected 0x123, got %v", inner["id"])
	}
}

func TestParseArgs_QuotedString(t *testing.T) {
	args := parseArgs(`id: "hello, world"`)
	if args["id"] != "hello, world" {
		t.Errorf("expected 'hello, world', got %v", args["id"])
	}
}

func TestParseArgs_Empty(t *testing.T) {
	args := parseArgs("")
	if len(args) != 0 {
		t.Errorf("expected empty, got %v", args)
	}
}

func TestQueryDepth(t *testing.T) {
	tests := []struct {
		query string
		depth int
	}{
		{`{ tokens { id } }`, 2},
		{`{ tokens { id name } }`, 2},
		{`{ pools { token0 { id } } }`, 3},
		{`{ a { b { c { d } } } }`, 4},
		{`{ factory(id: "1") { id } }`, 2},
		{`{ tokens(first: 1) { id } "quoted { brace" }`, 2}, // quoted braces ignored
	}

	for _, tt := range tests {
		got := queryDepth(tt.query)
		if got != tt.depth {
			t.Errorf("queryDepth(%q) = %d, want %d", tt.query, got, tt.depth)
		}
	}
}

func TestFindMatchingParen(t *testing.T) {
	tests := []struct {
		s   string
		pos int
		end int
	}{
		{"(abc)", 0, 4},
		{"(a, b: {c: 1})", 0, 13},
		{`(id: "test (paren)")`, 0, 19},
		{"(no close", 0, -1},
	}

	for _, tt := range tests {
		got := findMatchingParen(tt.s, tt.pos)
		if got != tt.end {
			t.Errorf("findMatchingParen(%q, %d) = %d, want %d", tt.s, tt.pos, got, tt.end)
		}
	}
}

func TestParseListArgs_Clamping(t *testing.T) {
	// Negative
	args := map[string]interface{}{"first": "-5"}
	limit, _, _, _ := parseListArgs(args, 100)
	if limit < 1 {
		t.Errorf("negative limit not clamped: %d", limit)
	}

	// Too large
	args = map[string]interface{}{"first": "999999"}
	limit, _, _, _ = parseListArgs(args, 100)
	if limit > 1000 {
		t.Errorf("large limit not clamped: %d", limit)
	}

	// Default
	args = map[string]interface{}{}
	limit, _, _, _ = parseListArgs(args, 50)
	if limit != 50 {
		t.Errorf("default not applied: %d", limit)
	}
}
