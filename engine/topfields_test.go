package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/luxfi/graph/storage"
)

// The Explore transactions view asks for 23 tables in one request and got back
// "too many top-level fields (23), maximum 20" — the page died on a limit that
// was a bare literal while every neighbouring limit was configurable.
func query(n int) string {
	// One field per line, the shape Apollo actually sends — parseTopFields
	// splits on newlines, so a single-line query reads as one field.
	var b strings.Builder
	b.WriteString("{\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  f%d { id }\n", i)
	}
	b.WriteString("}")
	return b.String()
}

func newTestEngine(t *testing.T, cfg *Config, fields int) *Engine {
	t.Helper()
	e := New(nil, cfg)
	for i := 0; i < fields; i++ {
		e.RegisterResolver(fmt.Sprintf("f%d", i), func(_ context.Context, _ *storage.Store, _ map[string]interface{}) (interface{}, error) {
			return []interface{}{}, nil
		})
	}
	return e
}

func errText(r *Response) string {
	if len(r.Errors) == 0 {
		return ""
	}
	return r.Errors[0].Message
}

func TestTopFields_RealPageIsNotRejected(t *testing.T) {
	e := newTestEngine(t, nil, 23)
	if got := errText(e.Execute(context.Background(), &Request{Query: query(23)})); strings.Contains(got, "too many top-level fields") {
		t.Fatalf("23 fields rejected by default: %s", got)
	}
}

func TestTopFields_CapStillApplies(t *testing.T) {
	e := newTestEngine(t, &Config{MaxQueryDepth: 10, MaxTopFields: 5, MaxResultSize: 1 << 20, QueryTimeoutMs: 30000}, 8)
	got := errText(e.Execute(context.Background(), &Request{Query: query(8)}))
	if !strings.Contains(got, "too many top-level fields (8), maximum 5") {
		t.Fatalf("configured cap not enforced, got %q", got)
	}
}

func TestTopFields_UnsetConfigFallsBackToDefault(t *testing.T) {
	// A zero value means "unset", not "reject everything" — a config written
	// before this field existed must not brick every query.
	e := newTestEngine(t, &Config{MaxQueryDepth: 10, MaxResultSize: 1 << 20, QueryTimeoutMs: 30000}, 23)
	if got := errText(e.Execute(context.Background(), &Request{Query: query(23)})); strings.Contains(got, "too many top-level fields") {
		t.Fatalf("zero MaxTopFields rejected a normal query: %s", got)
	}
	if e.maxTopFields() != defaultMaxTopFields {
		t.Fatalf("expected default %d, got %d", defaultMaxTopFields, e.maxTopFields())
	}
}
