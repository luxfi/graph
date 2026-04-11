//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/luxfi/graph/wasm"
)

// Test WASM subgraph loading from actual compiled subgraph build dirs.

func TestWASM_LoadV2Subgraph(t *testing.T) {
	rt, err := wasm.NewRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	sg, err := rt.LoadSubgraph("/Users/z/work/lux/uni-v2-subgraph/build/")
	if err != nil {
		t.Fatalf("load v2 subgraph: %v", err)
	}

	if len(sg.DataSources) == 0 {
		t.Fatal("expected at least 1 data source")
	}
	if sg.DataSources[0].Name != "Factory" {
		t.Errorf("expected Factory data source, got %s", sg.DataSources[0].Name)
	}
	if len(sg.Templates) == 0 {
		t.Fatal("expected at least 1 template")
	}
	if sg.Schema == "" {
		t.Fatal("schema not loaded")
	}
	t.Logf("v2: %s", sg.Info())
}

func TestWASM_LoadV3Subgraph(t *testing.T) {
	rt, err := wasm.NewRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	sg, err := rt.LoadSubgraph("/Users/z/work/lux/uni-v3-subgraph/build/")
	if err != nil {
		t.Fatalf("load v3 subgraph: %v", err)
	}

	if len(sg.DataSources) == 0 {
		t.Fatal("expected at least 1 data source")
	}
	if sg.DataSources[0].Name != "Factory" {
		t.Errorf("expected Factory data source, got %s", sg.DataSources[0].Name)
	}
	if sg.Schema == "" {
		t.Fatal("schema not loaded")
	}
	t.Logf("v3: %s", sg.Info())
}

func TestWASM_LoadV4Subgraph(t *testing.T) {
	rt, err := wasm.NewRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	sg, err := rt.LoadSubgraph("/Users/z/work/lux/uni-v4-subgraph/build/")
	if err != nil {
		t.Fatalf("load v4 subgraph: %v", err)
	}

	if len(sg.DataSources) == 0 {
		t.Fatal("expected at least 1 data source")
	}
	if sg.DataSources[0].Name != "PoolManager" {
		t.Errorf("expected PoolManager data source, got %s", sg.DataSources[0].Name)
	}
	if sg.Schema == "" {
		t.Fatal("schema not loaded")
	}
	t.Logf("v4: %s", sg.Info())
}

func TestWASM_EntityStore(t *testing.T) {
	es := wasm.NewEntityStore()

	es.Set("Token", "0xabc", map[string]interface{}{
		"symbol": "WLUX", "decimals": 18,
	})

	data, ok := es.Get("Token", "0xabc")
	if !ok {
		t.Fatal("expected entity to exist")
	}
	if data["symbol"] != "WLUX" {
		t.Errorf("expected WLUX, got %v", data["symbol"])
	}

	flushed := es.Flush()
	if len(flushed["Token"]) != 1 {
		t.Error("expected 1 flushed token")
	}

	// After flush, store should be empty
	_, ok = es.Get("Token", "0xabc")
	if ok {
		t.Error("expected empty after flush")
	}
}
