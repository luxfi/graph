package engine

import (
	"testing"
	"time"
)

// A document is the same document minified. Splitting top-level fields on
// newlines answered a one-line query with its FIRST field and dropped the rest
// without an error — the shape every client library produces when it strips
// whitespace, and the shape of the explore page's whole document.

func names(t *testing.T, query string) []string {
	t.Helper()
	fs, err := parseTopFields(query)
	if err != nil {
		t.Fatalf("parse %q: %v", query, err)
	}
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		n := f.name
		if f.alias != "" {
			n = f.alias + ":" + f.name
		}
		out = append(out, n)
	}
	return out
}

func TestTopFieldsSurviveMinification(t *testing.T) {
	const pretty = `{
  bundle(id: "1") { ethPriceUSD }
  factories(first: 1) { id poolCount }
  tokenDayDatas(where: { date_gte: 1767225600 }, orderBy: date, orderDirection: desc, first: 100) {
    token { id }
    date
    volumeUSD
  }
  poolDayDatas(first: 90, orderBy: date, orderDirection: desc, where: { pool: "0xabc" }) { date open high low close }
}`
	minified := `{bundle(id:"1"){ethPriceUSD} factories(first:1){id poolCount} tokenDayDatas(where:{date_gte:1767225600},orderBy:date,orderDirection:desc,first:100){token{id} date volumeUSD} poolDayDatas(first:90,orderBy:date,orderDirection:desc,where:{pool:"0xabc"}){date open high low close}}`

	want := []string{"bundle", "factories", "tokenDayDatas", "poolDayDatas"}
	for label, q := range map[string]string{"pretty": pretty, "minified": minified} {
		got := names(t, q)
		if len(got) != len(want) {
			t.Fatalf("%s: parsed %v, want %v", label, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: field %d = %q, want %q", label, i, got[i], want[i])
			}
		}
	}

	// The arguments have to survive the split too, or a chart asks for 90 days
	// of one pool and is answered with a default page of everything.
	fs, err := parseTopFields(minified)
	if err != nil {
		t.Fatal(err)
	}
	limit, orderBy, dir, where := parseListArgs(fs[3].args, 30)
	if limit != 90 || orderBy != "date" || dir != "desc" {
		t.Errorf("poolDayDatas args = first %d, orderBy %q, %q; want 90, date, desc", limit, orderBy, dir)
	}
	if where == nil || where["pool"] != "0xabc" {
		t.Errorf("poolDayDatas where = %v, want pool 0xabc", where)
	}
}

func TestTopFieldsParseAliasesAndBareFields(t *testing.T) {
	for _, c := range []struct {
		query string
		want  []string
	}{
		{`{a:pools(first:1){id} b:pools(first:2){id}}`, []string{"a:pools", "b:pools"}},
		{`{bundle{ethPriceUSD} factory{id}}`, []string{"bundle", "factory"}},
		{"{ bundles\n factories }", []string{"bundles", "factories"}},
		{`{pools(where:{token0:"0x1"}){id token0{id}} swaps(first:1){id}}`, []string{"pools", "swaps"}},
		// Whitespace is insignificant in GraphQL, including before a field's
		// own punctuation.
		{`{bundle (id: "1") { ethPriceUSD } factory (id: "1") { id }}`, []string{"bundle", "factory"}},
		{`{ mine : pools(first: 1) { id }  theirs : swaps(first: 1) { id } }`, []string{"mine:pools", "theirs:swaps"}},
	} {
		got := names(t, c.query)
		if len(got) != len(c.want) {
			t.Errorf("%s parsed %v, want %v", c.query, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%s: field %d = %q, want %q", c.query, i, got[i], c.want[i])
			}
		}
	}
}

// A parser that cannot advance must say so. Standing still on input it does not
// understand holds the request open forever rather than answering it — and this
// is a public endpoint.
func TestUnparseableQueryIsRefusedNotHung(t *testing.T) {
	for _, q := range []string{
		`{bundle(id:"1"}`,    // unclosed arguments
		`{bundle{id`,         // unclosed selection set
		`{)}`,                // punctuation where a field belongs
		`{bundle(id:"1"){id`, // truncated mid-selection
	} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			parseTopFields(q)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("parseTopFields(%q) never returned", q)
		}
	}
}
