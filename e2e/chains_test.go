//go:build e2e

package e2e

import (
	"testing"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/storage"
)

// Test all chain schemas load and resolve without errors.

func setupChainEngine(t *testing.T, schema string) *engine.Engine {
	t.Helper()
	store, _ := storage.New(t.TempDir())
	store.Init(nil)

	// Seed generic entities for chain resolvers
	store.SetEntity("Validator", "NodeID-abc", map[string]interface{}{
		"id": "NodeID-abc", "stake": "1000000", "uptime": "99.9",
	})
	store.SetEntity("Order", "order-1", map[string]interface{}{
		"id": "order-1", "market": "LUX/USD", "side": "buy", "price": "2.50", "amount": "100",
	})
	store.SetEntity("DKGCeremony", "dkg-1", map[string]interface{}{
		"id": "dkg-1", "participants": 5, "threshold": 3, "status": "complete",
	})
	store.SetEntity("Asset", "LUX", map[string]interface{}{
		"id": "LUX", "symbol": "LUX", "name": "Lux", "denomination": 6,
	})
	store.SetEntity("BridgeTransfer", "bt-1", map[string]interface{}{
		"id": "bt-1", "sourceChain": "C", "destChain": "ethereum", "amount": "1000",
	})
	store.SetEntity("ShieldedTransfer", "st-1", map[string]interface{}{
		"id": "st-1", "nullifier": "0xabc", "commitment": "0xdef",
	})

	eng := engine.New(store, nil)
	if err := eng.LoadBuiltin(schema); err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestSchema_DEX(t *testing.T) {
	eng := setupChainEngine(t, "dex")
	resp := eng.Execute(nil, &engine.Request{Query: `{ orders(first: 10) { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}

	resp = eng.Execute(nil, &engine.Request{Query: `{ order(id: "order-1") { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}
	if resp.Data == nil {
		t.Fatal("expected data")
	}
}

func TestSchema_FHE(t *testing.T) {
	eng := setupChainEngine(t, "fhe")
	resp := eng.Execute(nil, &engine.Request{Query: `{ dkgCeremonies(first: 10) { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}

	resp = eng.Execute(nil, &engine.Request{Query: `{ dkgCeremony(id: "dkg-1") { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}
}

func TestSchema_Platform(t *testing.T) {
	eng := setupChainEngine(t, "platform")
	resp := eng.Execute(nil, &engine.Request{Query: `{ validators(first: 10) { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}

	resp = eng.Execute(nil, &engine.Request{Query: `{ validator(id: "NodeID-abc") { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}
}

func TestSchema_XChain(t *testing.T) {
	eng := setupChainEngine(t, "xchain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ assets(first: 10) { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}

	resp = eng.Execute(nil, &engine.Request{Query: `{ asset(id: "LUX") { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}
}

func TestSchema_Bridge(t *testing.T) {
	eng := setupChainEngine(t, "bridge")
	resp := eng.Execute(nil, &engine.Request{Query: `{ bridgeTransfers(first: 10) { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}

	resp = eng.Execute(nil, &engine.Request{Query: `{ bridgeTransfer(id: "bt-1") { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}
}

func TestSchema_Privacy(t *testing.T) {
	eng := setupChainEngine(t, "zchain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ shieldedTransfers(first: 10) { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}

	resp = eng.Execute(nil, &engine.Request{Query: `{ shieldedTransfer(id: "st-1") { id } }`})
	if len(resp.Errors) > 0 {
		t.Fatal(resp.Errors[0].Message)
	}
}

func TestSchema_Quantum(t *testing.T) {
	eng := setupChainEngine(t, "qchain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ ringtailSignatures(first: 10) { id } }`})
	if len(resp.Errors) > 0 { t.Fatal(resp.Errors[0].Message) }
}

func TestSchema_Key(t *testing.T) {
	eng := setupChainEngine(t, "kchain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ managedKeys(first: 10) { id } }`})
	if len(resp.Errors) > 0 { t.Fatal(resp.Errors[0].Message) }
}

func TestSchema_AI(t *testing.T) {
	eng := setupChainEngine(t, "achain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ inferenceProofs(first: 10) { id } }`})
	if len(resp.Errors) > 0 { t.Fatal(resp.Errors[0].Message) }
}

func TestSchema_Identity(t *testing.T) {
	eng := setupChainEngine(t, "ichain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ dids(first: 10) { id } }`})
	if len(resp.Errors) > 0 { t.Fatal(resp.Errors[0].Message) }
}

func TestSchema_Oracle(t *testing.T) {
	eng := setupChainEngine(t, "ochain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ priceFeeds(first: 10) { id } }`})
	if len(resp.Errors) > 0 { t.Fatal(resp.Errors[0].Message) }
}

func TestSchema_Relay(t *testing.T) {
	eng := setupChainEngine(t, "rchain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ warpMessages(first: 10) { id } }`})
	if len(resp.Errors) > 0 { t.Fatal(resp.Errors[0].Message) }
}

func TestSchema_ServiceNode(t *testing.T) {
	eng := setupChainEngine(t, "schain")
	resp := eng.Execute(nil, &engine.Request{Query: `{ serviceNodes(first: 10) { id } }`})
	if len(resp.Errors) > 0 { t.Fatal(resp.Errors[0].Message) }
}

func TestSchema_Precompile(t *testing.T) {
	eng := setupChainEngine(t, "precompile")
	queries := []string{
		`{ precompileCalls(first: 10) { id } }`,
		`{ aiWorkProofs(first: 10) { id } }`,
		`{ fheOperations(first: 10) { id } }`,
		`{ zkVerifications(first: 10) { id } }`,
		`{ ringSignatures(first: 10) { id } }`,
		`{ pqCryptoOps(first: 10) { id } }`,
		`{ thresholdOps(first: 10) { id } }`,
	}
	for _, q := range queries {
		resp := eng.Execute(nil, &engine.Request{Query: q})
		if len(resp.Errors) > 0 {
			t.Errorf("query %q failed: %s", q, resp.Errors[0].Message)
		}
	}
}

func TestSchema_All(t *testing.T) {
	eng := setupChainEngine(t, "all")

	// Every chain type must resolve without errors
	queries := []string{
		`{ factories(first: 1) { id } }`,          // AMM (C-Chain)
		`{ orders(first: 1) { id } }`,              // DEX (D-Chain)
		`{ dkgCeremonies(first: 1) { id } }`,      // FHE (T-Chain)
		`{ validators(first: 1) { id } }`,          // Platform (P-Chain)
		`{ assets(first: 1) { id } }`,              // Exchange (X-Chain)
		`{ bridgeTransfers(first: 1) { id } }`,     // Bridge (B-Chain)
		`{ shieldedTransfers(first: 1) { id } }`,   // Privacy (Z-Chain)
		`{ ringtailSignatures(first: 1) { id } }`,  // Quantum (Q-Chain)
		`{ managedKeys(first: 1) { id } }`,         // Key (K-Chain)
		`{ inferenceProofs(first: 1) { id } }`,     // AI (A-Chain)
		`{ dids(first: 1) { id } }`,                // Identity (I-Chain)
		`{ priceFeeds(first: 1) { id } }`,          // Oracle (O-Chain)
		`{ warpMessages(first: 1) { id } }`,        // Relay (R-Chain)
		`{ serviceNodes(first: 1) { id } }`,        // ServiceNode (S-Chain)
		`{ precompileCalls(first: 1) { id } }`,     // Precompiles
		`{ fheOperations(first: 1) { id } }`,       // FHE precompile
		`{ zkVerifications(first: 1) { id } }`,     // ZK precompile
		`{ tokens(first: 1) { id } }`,              // ERC20
		`{ nfts(first: 1) { id } }`,                // ERC721
	}

	for _, q := range queries {
		resp := eng.Execute(nil, &engine.Request{Query: q})
		if len(resp.Errors) > 0 {
			t.Errorf("query %q failed: %s", q, resp.Errors[0].Message)
		}
	}
}

func TestSchema_AMMV4(t *testing.T) {
	eng := setupChainEngine(t, "amm-v4")

	// V4 resolvers should be available
	queries := []string{
		`{ factories(first: 1) { id } }`,
		`{ pools(first: 1) { id } }`,
		`{ poolManagers(first: 1) { id } }`,
	}

	for _, q := range queries {
		resp := eng.Execute(nil, &engine.Request{Query: q})
		if len(resp.Errors) > 0 {
			t.Errorf("query %q failed: %s", q, resp.Errors[0].Message)
		}
	}
}
