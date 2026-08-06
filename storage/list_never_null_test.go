package storage

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// A store getter that answers with a list must answer with an EMPTY list when it
// has nothing, never a nil slice.
//
// This is not cosmetic. A nil Go slice marshals to JSON `null`, and on a GraphQL
// wire `{"data":{"markets":null}}` is the signature of a subgraph that is absent,
// unsubscribed or erroring — while `{"data":{"markets":[]}}` says "deployed,
// healthy, nothing has happened yet". The two are opposite diagnoses, and the
// `dex` subgraph shipped the wrong one: it was mounted, subscribed to the 0x9999
// settlement precompile and caught up to the chain head, yet reported itself dead
// on every query because no DEXFill had been emitted.
//
// The check is reflective rather than a hand-written list so a getter added later
// cannot reintroduce the bug by being forgotten here.
func TestStoreGettersNeverReturnNilList(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	var (
		anyType = reflect.TypeOf((*interface{})(nil)).Elem()
		errType = reflect.TypeOf((*error)(nil)).Elem()
	)

	v := reflect.ValueOf(store)
	checked := 0
	for i := range v.NumMethod() {
		m, name := v.Type().Method(i), v.Type().Method(i).Name
		ft := m.Type
		// Select the resolver-facing getters: (…) (interface{}, error).
		if ft.NumOut() != 2 || ft.Out(0) != anyType || ft.Out(1) != errType || ft.IsVariadic() {
			continue
		}

		args := make([]reflect.Value, 0, ft.NumIn()-1)
		for j := 1; j < ft.NumIn(); j++ { // skip the receiver
			if ft.In(j).Kind() == reflect.Int {
				args = append(args, reflect.ValueOf(100)) // limit
				continue
			}
			args = append(args, reflect.Zero(ft.In(j)))
		}

		out := v.Method(i).Call(args)
		if !out[1].IsNil() {
			t.Fatalf("%s on an empty store: %v", name, out[1].Interface())
		}
		got := out[0].Interface()

		// An untyped nil is a single-entity getter reporting a miss — legitimate.
		rv := reflect.ValueOf(got)
		if rv.Kind() != reflect.Slice {
			continue
		}
		checked++
		if rv.IsNil() {
			t.Errorf("%s returned a nil slice on an empty store; it must return an empty list", name)
			continue
		}
		if raw, _ := json.Marshal(got); string(raw) != "[]" {
			t.Errorf("%s encoded as %s on an empty store, want []", name, raw)
		}
	}
	if checked == 0 {
		t.Fatal("no list getters exercised — the reflective selection is broken, not the store")
	}
	t.Logf("%d list getters return an empty list on an empty store", checked)
}

// FilterResults is the shared tail of the pool/token/swap getters, so it must
// preserve the same guarantee: filtering everything out yields an empty list.
func TestFilterResultsNeverReturnsNil(t *testing.T) {
	in := []interface{}{map[string]interface{}{"id": "1", "pool": "0xA"}}
	out := FilterResults(in, map[string]interface{}{"pool": "0xB"})
	if out == nil {
		t.Fatal("FilterResults returned nil for a total miss; want an empty list")
	}
	if raw, _ := json.Marshal(out); string(raw) != "[]" {
		t.Fatalf("FilterResults encoded as %s, want []", raw)
	}
}
