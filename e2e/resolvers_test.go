//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/storage"
)

// ---------------------------------------------------------------------------
// Helper: create engine with all schemas loaded and a seeded store.
// ---------------------------------------------------------------------------

func newTestEngine(t *testing.T) (*engine.Engine, *storage.Store) {
	t.Helper()
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(store, nil)
	if err := eng.LoadBuiltin("all"); err != nil {
		t.Fatal(err)
	}
	return eng, store
}

func execOK(t *testing.T, eng *engine.Engine, query string) map[string]interface{} {
	t.Helper()
	resp := eng.Execute(context.Background(), &engine.Request{Query: query})
	if len(resp.Errors) > 0 {
		t.Fatalf("query error: %s\nquery: %s", resp.Errors[0].Message, query)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", resp.Data)
	}
	return data
}

func execErr(t *testing.T, eng *engine.Engine, query string) string {
	t.Helper()
	resp := eng.Execute(context.Background(), &engine.Request{Query: query})
	if len(resp.Errors) == 0 {
		t.Fatal("expected error, got none")
	}
	return resp.Errors[0].Message
}

func requireList(t *testing.T, data map[string]interface{}, key string, minLen int) []interface{} {
	t.Helper()
	raw, ok := data[key]
	if !ok {
		t.Fatalf("missing key %q in response", key)
	}
	list, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("key %q: expected []interface{}, got %T", key, raw)
	}
	if len(list) < minLen {
		t.Fatalf("key %q: expected >= %d items, got %d", key, minLen, len(list))
	}
	return list
}

func requireMap(t *testing.T, data map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	raw, ok := data[key]
	if !ok || raw == nil {
		t.Fatalf("missing or nil key %q in response", key)
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("key %q: expected map, got %T", key, raw)
	}
	return m
}

func requireField(t *testing.T, m map[string]interface{}, field string) interface{} {
	t.Helper()
	v, ok := m[field]
	if !ok || v == nil {
		t.Fatalf("missing field %q", field)
	}
	return v
}

// ---------------------------------------------------------------------------
// 1. AMM resolvers (C-Chain) — factory, bundle, pool, swap, token
// ---------------------------------------------------------------------------

func TestResolver_AMM_Factory(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount: 10, TxCount: 100,
		TotalVolumeUSD: "500000.00", TotalValueLockedUSD: "200000.00",
	})

	data := execOK(t, eng, `{ factory(id: "1") { id poolCount txCount totalVolumeUSD totalValueLockedUSD } }`)
	f := requireMap(t, data, "factory")
	if fmt.Sprint(f["poolCount"]) != "10" {
		t.Errorf("poolCount: got %v", f["poolCount"])
	}
}

func TestResolver_AMM_Factories(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SeedFactory("1", &storage.SeedFactoryData{PoolCount: 5, TxCount: 50, TotalVolumeUSD: "1000.00", TotalValueLockedUSD: "500.00"})

	data := execOK(t, eng, `{ factories(first: 10) { id poolCount } }`)
	requireList(t, data, "factories", 1)
}

func TestResolver_AMM_Bundle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SeedBundle("1", &storage.SeedBundleData{EthPriceUSD: "2.50"})

	data := execOK(t, eng, `{ bundle(id: "1") { ethPriceUSD } }`)
	b := requireMap(t, data, "bundle")
	requireField(t, b, "ethPriceUSD")
}

func TestResolver_AMM_Pools(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SeedToken("0xt0", &storage.SeedTokenData{Symbol: "A", Name: "TokenA", Decimals: 18})
	store.SeedToken("0xt1", &storage.SeedTokenData{Symbol: "B", Name: "TokenB", Decimals: 18})
	store.SeedPool("0xpool1", &storage.SeedPoolData{
		Token0: "0xt0", Token1: "0xt1", FeeTier: 3000,
		TotalValueLockedUSD: "100000.00", VolumeUSD: "50000.00",
		Token0Price: "1.00", Token1Price: "1.00", TxCount: 42,
	})

	data := execOK(t, eng, `{ pools(first: 10) { id feeTier totalValueLockedUSD token0 { id symbol } token1 { id symbol } } }`)
	pools := requireList(t, data, "pools", 1)
	p := pools[0].(map[string]interface{})
	requireField(t, p, "feeTier")
	requireField(t, p, "token0")
	requireField(t, p, "token1")
}

func TestResolver_AMM_Swaps(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SeedSwap("0xswap1", &storage.SeedSwapData{
		Timestamp: 1711929600, Pool: "0xpool1",
		Amount0: "100.0", Amount1: "-250.0", AmountUSD: "250.00", Sender: "0xuser1",
	})
	store.SeedSwap("0xswap2", &storage.SeedSwapData{
		Timestamp: 1711929601, Pool: "0xpool1",
		Amount0: "200.0", Amount1: "-500.0", AmountUSD: "500.00", Sender: "0xuser2",
	})

	data := execOK(t, eng, `{ swaps(first: 10, orderBy: timestamp, orderDirection: desc) { id timestamp amount0 amount1 amountUSD sender } }`)
	swaps := requireList(t, data, "swaps", 2)
	s := swaps[0].(map[string]interface{})
	requireField(t, s, "timestamp")
	requireField(t, s, "sender")
}

func TestResolver_AMM_SwapSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SeedSwap("0xswap-single", &storage.SeedSwapData{
		Timestamp: 100, Pool: "0xp", Amount0: "1", Amount1: "2", AmountUSD: "3", Sender: "0xs",
	})

	data := execOK(t, eng, `{ swap(id: "0xswap-single") { id amountUSD } }`)
	s := requireMap(t, data, "swap")
	if s["amountUSD"] != "3" {
		t.Errorf("amountUSD: got %v", s["amountUSD"])
	}
}

// ---------------------------------------------------------------------------
// 2. DEX resolvers (D-Chain) — orders, fills, markets, positions, funding, liquidations
// ---------------------------------------------------------------------------

func TestResolver_DEX_Orders(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Order", "order-1", map[string]interface{}{
		"id": "order-1", "market": "LUX/USD", "side": "buy", "price": "2.50", "amount": "100", "status": "open",
	})
	store.SetEntity("Order", "order-2", map[string]interface{}{
		"id": "order-2", "market": "LUX/USD", "side": "sell", "price": "2.60", "amount": "50", "status": "filled",
	})

	data := execOK(t, eng, `{ orders(first: 10) { id market side price amount status } }`)
	requireList(t, data, "orders", 2)
}

func TestResolver_DEX_OrderSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Order", "order-1", map[string]interface{}{
		"id": "order-1", "market": "LUX/USD", "side": "buy", "price": "2.50", "amount": "100",
	})

	data := execOK(t, eng, `{ order(id: "order-1") { id market side price } }`)
	o := requireMap(t, data, "order")
	if o["market"] != "LUX/USD" {
		t.Errorf("market: got %v", o["market"])
	}
}

func TestResolver_DEX_Fills(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Fill", "fill-1", map[string]interface{}{
		"id": "fill-1", "order": "order-1", "price": "2.50", "amount": "50", "timestamp": 1711929600,
	})

	data := execOK(t, eng, `{ fills(first: 10) { id } }`)
	requireList(t, data, "fills", 1)
}

func TestResolver_DEX_FillSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Fill", "fill-1", map[string]interface{}{
		"id": "fill-1", "price": "2.50",
	})

	data := execOK(t, eng, `{ fill(id: "fill-1") { id price } }`)
	f := requireMap(t, data, "fill")
	if f["price"] != "2.50" {
		t.Errorf("price: got %v", f["price"])
	}
}

func TestResolver_DEX_Markets(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Market", "LUX-USD", map[string]interface{}{
		"id": "LUX-USD", "base": "LUX", "quote": "USD", "lastPrice": "2.50", "volume24h": "1000000",
	})

	data := execOK(t, eng, `{ markets(first: 10) { id } }`)
	requireList(t, data, "markets", 1)
}

func TestResolver_DEX_MarketSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Market", "LUX-USD", map[string]interface{}{
		"id": "LUX-USD", "base": "LUX", "quote": "USD",
	})

	data := execOK(t, eng, `{ market(id: "LUX-USD") { id base quote } }`)
	m := requireMap(t, data, "market")
	if m["base"] != "LUX" {
		t.Errorf("base: got %v", m["base"])
	}
}

func TestResolver_DEX_PerpPositions(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PerpPosition", "pos-1", map[string]interface{}{
		"id": "pos-1", "market": "LUX/USD", "side": "long", "size": "1000", "entryPrice": "2.50",
	})

	data := execOK(t, eng, `{ perpPositions(first: 10) { id } }`)
	requireList(t, data, "perpPositions", 1)
}

func TestResolver_DEX_FundingRates(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("FundingRate", "fr-1", map[string]interface{}{
		"id": "fr-1", "market": "LUX/USD", "rate": "0.001", "timestamp": 1711929600,
	})

	data := execOK(t, eng, `{ fundingRates(first: 10) { id } }`)
	requireList(t, data, "fundingRates", 1)
}

func TestResolver_DEX_Liquidations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Liquidation", "liq-1", map[string]interface{}{
		"id": "liq-1", "position": "pos-1", "amount": "500", "price": "2.00",
	})

	data := execOK(t, eng, `{ liquidations(first: 10) { id } }`)
	requireList(t, data, "liquidations", 1)
}

func TestResolver_DEX_Orderbook(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Orderbook", "LUX-USD", map[string]interface{}{
		"id": "LUX-USD", "bids": 50, "asks": 45,
	})

	data := execOK(t, eng, `{ orderbook(id: "LUX-USD") { id bids asks } }`)
	ob := requireMap(t, data, "orderbook")
	requireField(t, ob, "bids")
}

// ---------------------------------------------------------------------------
// 3. Platform resolvers (P-Chain) — validators, delegators, subnets, blockchains
// ---------------------------------------------------------------------------

func TestResolver_Platform_Validators(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Validator", "NodeID-abc", map[string]interface{}{
		"id": "NodeID-abc", "stake": "2000000", "uptime": "99.95", "startTime": 1700000000, "endTime": 1800000000,
	})
	store.SetEntity("Validator", "NodeID-def", map[string]interface{}{
		"id": "NodeID-def", "stake": "1000000", "uptime": "99.80",
	})

	data := execOK(t, eng, `{ validators(first: 10) { id stake uptime } }`)
	requireList(t, data, "validators", 2)
}

func TestResolver_Platform_ValidatorSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Validator", "NodeID-abc", map[string]interface{}{
		"id": "NodeID-abc", "stake": "2000000", "uptime": "99.95",
	})

	data := execOK(t, eng, `{ validator(id: "NodeID-abc") { id stake uptime } }`)
	v := requireMap(t, data, "validator")
	if v["stake"] != "2000000" {
		t.Errorf("stake: got %v", v["stake"])
	}
}

func TestResolver_Platform_Delegators(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Delegator", "del-1", map[string]interface{}{
		"id": "del-1", "nodeID": "NodeID-abc", "stake": "100000", "rewardAddress": "P-lux1abc",
	})

	data := execOK(t, eng, `{ delegators(first: 10) { id } }`)
	requireList(t, data, "delegators", 1)
}

func TestResolver_Platform_Subnets(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Subnet", "sub-1", map[string]interface{}{
		"id": "sub-1", "controlKeys": 3, "threshold": 2,
	})

	data := execOK(t, eng, `{ subnets(first: 10) { id } }`)
	requireList(t, data, "subnets", 1)
}

func TestResolver_Platform_Blockchains(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Blockchain", "bc-1", map[string]interface{}{
		"id": "bc-1", "name": "Zoo", "vmID": "srEXiWaHuhNyGwPUi444Tw", "subnetID": "sub-1",
	})

	data := execOK(t, eng, `{ blockchains(first: 10) { id } }`)
	requireList(t, data, "blockchains", 1)
}

func TestResolver_Platform_StakingRewards(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("StakingReward", "sr-1", map[string]interface{}{
		"id": "sr-1", "nodeID": "NodeID-abc", "amount": "5000", "timestamp": 1711929600,
	})

	data := execOK(t, eng, `{ stakingRewards(first: 10) { id } }`)
	requireList(t, data, "stakingRewards", 1)
}

func TestResolver_Platform_NetworkStats(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("NetworkStats", "1", map[string]interface{}{
		"id": "1", "totalStake": "50000000", "validatorCount": 5, "delegatorCount": 100,
	})

	data := execOK(t, eng, `{ networkStats { id totalStake validatorCount } }`)
	ns := requireMap(t, data, "networkStats")
	if ns["totalStake"] != "50000000" {
		t.Errorf("totalStake: got %v", ns["totalStake"])
	}
}

// ---------------------------------------------------------------------------
// 4. Exchange resolvers (X-Chain) — assets, UTXOs, transfers, imports, exports
// ---------------------------------------------------------------------------

func TestResolver_Exchange_Assets(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Asset", "LUX", map[string]interface{}{
		"id": "LUX", "symbol": "LUX", "name": "Lux", "denomination": 9,
	})
	store.SetEntity("Asset", "ZOO", map[string]interface{}{
		"id": "ZOO", "symbol": "ZOO", "name": "Zoo Token", "denomination": 6,
	})

	data := execOK(t, eng, `{ assets(first: 10) { id symbol name denomination } }`)
	requireList(t, data, "assets", 2)
}

func TestResolver_Exchange_AssetSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Asset", "LUX", map[string]interface{}{
		"id": "LUX", "symbol": "LUX", "name": "Lux", "denomination": 9,
	})

	data := execOK(t, eng, `{ asset(id: "LUX") { id symbol denomination } }`)
	a := requireMap(t, data, "asset")
	if a["symbol"] != "LUX" {
		t.Errorf("symbol: got %v", a["symbol"])
	}
}

func TestResolver_Exchange_UTXOs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("UTXO", "utxo-1", map[string]interface{}{
		"id": "utxo-1", "txID": "0xabc", "outputIndex": 0, "amount": "1000000000",
	})

	data := execOK(t, eng, `{ utxos(first: 10) { id } }`)
	requireList(t, data, "utxos", 1)
}

func TestResolver_Exchange_XTransfers(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("XTransfer", "xt-1", map[string]interface{}{
		"id": "xt-1", "from": "X-lux1abc", "to": "X-lux1def", "amount": "500000",
	})

	data := execOK(t, eng, `{ xTransfers(first: 10) { id } }`)
	requireList(t, data, "xTransfers", 1)
}

func TestResolver_Exchange_ImportExportTxs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ImportTx", "imp-1", map[string]interface{}{
		"id": "imp-1", "sourceChain": "C", "amount": "1000",
	})
	store.SetEntity("ExportTx", "exp-1", map[string]interface{}{
		"id": "exp-1", "destChain": "P", "amount": "2000",
	})

	data := execOK(t, eng, `{ importTxs(first: 10) { id } }`)
	requireList(t, data, "importTxs", 1)

	data = execOK(t, eng, `{ exportTxs(first: 10) { id } }`)
	requireList(t, data, "exportTxs", 1)
}

// ---------------------------------------------------------------------------
// 5. Bridge resolvers (B-Chain) — transfers, MPC sigs, wrapped assets, requests
// ---------------------------------------------------------------------------

func TestResolver_Bridge_Transfers(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("BridgeTransfer", "bt-1", map[string]interface{}{
		"id": "bt-1", "sourceChain": "C", "destChain": "ethereum", "amount": "1000", "status": "completed",
	})

	data := execOK(t, eng, `{ bridgeTransfers(first: 10) { id sourceChain destChain amount } }`)
	list := requireList(t, data, "bridgeTransfers", 1)
	bt := list[0].(map[string]interface{})
	if bt["sourceChain"] != "C" {
		t.Errorf("sourceChain: got %v", bt["sourceChain"])
	}
}

func TestResolver_Bridge_TransferSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("BridgeTransfer", "bt-1", map[string]interface{}{
		"id": "bt-1", "sourceChain": "C", "destChain": "ethereum", "amount": "1000",
	})

	data := execOK(t, eng, `{ bridgeTransfer(id: "bt-1") { id amount } }`)
	bt := requireMap(t, data, "bridgeTransfer")
	if bt["amount"] != "1000" {
		t.Errorf("amount: got %v", bt["amount"])
	}
}

func TestResolver_Bridge_MPCSignatures(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("MPCSignature", "mpc-1", map[string]interface{}{
		"id": "mpc-1", "txHash": "0xabc", "signers": 3, "threshold": 2,
	})

	data := execOK(t, eng, `{ mpcSignatures(first: 10) { id } }`)
	requireList(t, data, "mpcSignatures", 1)
}

func TestResolver_Bridge_WrappedAssets(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("WrappedAsset", "wa-1", map[string]interface{}{
		"id": "wa-1", "original": "ETH", "wrapped": "WETH.lux", "totalLocked": "500",
	})

	data := execOK(t, eng, `{ wrappedAssets(first: 10) { id } }`)
	requireList(t, data, "wrappedAssets", 1)
}

func TestResolver_Bridge_BridgeRequests(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("BridgeRequest", "br-1", map[string]interface{}{
		"id": "br-1", "user": "0xabc", "amount": "100", "status": "pending",
	})

	data := execOK(t, eng, `{ bridgeRequests(first: 10) { id } }`)
	requireList(t, data, "bridgeRequests", 1)
}

// ---------------------------------------------------------------------------
// 6. FHE/Threshold resolvers (T-Chain) — DKG, decryption, compute, ciphertexts
// ---------------------------------------------------------------------------

func TestResolver_FHE_DKGCeremonies(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DKGCeremony", "dkg-1", map[string]interface{}{
		"id": "dkg-1", "participants": 5, "threshold": 3, "status": "complete", "round": 1,
	})

	data := execOK(t, eng, `{ dkgCeremonies(first: 10) { id participants threshold status } }`)
	list := requireList(t, data, "dkgCeremonies", 1)
	dkg := list[0].(map[string]interface{})
	if dkg["status"] != "complete" {
		t.Errorf("status: got %v", dkg["status"])
	}
}

func TestResolver_FHE_DKGCeremonySingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DKGCeremony", "dkg-1", map[string]interface{}{
		"id": "dkg-1", "participants": 5, "threshold": 3, "status": "complete",
	})

	data := execOK(t, eng, `{ dkgCeremony(id: "dkg-1") { id threshold } }`)
	dkg := requireMap(t, data, "dkgCeremony")
	if fmt.Sprint(dkg["threshold"]) != "3" {
		t.Errorf("threshold: got %v", dkg["threshold"])
	}
}

func TestResolver_FHE_DecryptionRequests(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DecryptionRequest", "dr-1", map[string]interface{}{
		"id": "dr-1", "requester": "0xabc", "ciphertextID": "ct-1", "status": "pending",
	})

	data := execOK(t, eng, `{ decryptionRequests(first: 10) { id } }`)
	requireList(t, data, "decryptionRequests", 1)
}

func TestResolver_FHE_ComputeJobs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ComputeJob", "cj-1", map[string]interface{}{
		"id": "cj-1", "operation": "add", "inputA": "ct-1", "inputB": "ct-2", "result": "ct-3",
	})

	data := execOK(t, eng, `{ computeJobs(first: 10) { id } }`)
	requireList(t, data, "computeJobs", 1)
}

func TestResolver_FHE_Ciphertexts(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Ciphertext", "ct-1", map[string]interface{}{
		"id": "ct-1", "owner": "0xabc", "scheme": "BGV", "size": 4096,
	})

	data := execOK(t, eng, `{ ciphertexts(first: 10) { id } }`)
	requireList(t, data, "ciphertexts", 1)
}

// ---------------------------------------------------------------------------
// 7. Privacy resolvers (Z-Chain) — shielded transfers, ZK proofs, nullifiers
// ---------------------------------------------------------------------------

func TestResolver_Privacy_ShieldedTransfers(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ShieldedTransfer", "st-1", map[string]interface{}{
		"id": "st-1", "nullifier": "0xnull1", "commitment": "0xcomm1", "amount": "encrypted",
	})

	data := execOK(t, eng, `{ shieldedTransfers(first: 10) { id nullifier commitment } }`)
	list := requireList(t, data, "shieldedTransfers", 1)
	st := list[0].(map[string]interface{})
	if st["nullifier"] != "0xnull1" {
		t.Errorf("nullifier: got %v", st["nullifier"])
	}
}

func TestResolver_Privacy_ShieldedTransferSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ShieldedTransfer", "st-1", map[string]interface{}{
		"id": "st-1", "nullifier": "0xnull1", "commitment": "0xcomm1",
	})

	data := execOK(t, eng, `{ shieldedTransfer(id: "st-1") { id nullifier } }`)
	st := requireMap(t, data, "shieldedTransfer")
	if st["nullifier"] != "0xnull1" {
		t.Errorf("nullifier: got %v", st["nullifier"])
	}
}

func TestResolver_Privacy_ZKProofs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ZKProof", "zk-1", map[string]interface{}{
		"id": "zk-1", "proofType": "groth16", "verifier": "0xverify", "verified": true,
	})

	data := execOK(t, eng, `{ zkProofs(first: 10) { id } }`)
	requireList(t, data, "zkProofs", 1)
}

func TestResolver_Privacy_Nullifiers(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Nullifier", "null-1", map[string]interface{}{
		"id": "null-1", "hash": "0xnullhash", "spent": true,
	})

	data := execOK(t, eng, `{ nullifiers(first: 10) { id } }`)
	requireList(t, data, "nullifiers", 1)
}

func TestResolver_Privacy_Commitments(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Commitment", "comm-1", map[string]interface{}{
		"id": "comm-1", "leafIndex": 42, "value": "0xcommhash",
	})

	data := execOK(t, eng, `{ commitments(first: 10) { id } }`)
	requireList(t, data, "commitments", 1)
}

// ---------------------------------------------------------------------------
// 8. Quantum resolvers (Q-Chain) — ringtail sigs, quantum proofs, PQ keys
// ---------------------------------------------------------------------------

func TestResolver_Quantum_RingtailSignatures(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("RingtailSignature", "rs-1", map[string]interface{}{
		"id": "rs-1", "algorithm": "ML-DSA-65", "keyID": "key-1", "verified": true,
	})

	data := execOK(t, eng, `{ ringtailSignatures(first: 10) { id algorithm verified } }`)
	list := requireList(t, data, "ringtailSignatures", 1)
	rs := list[0].(map[string]interface{})
	if rs["algorithm"] != "ML-DSA-65" {
		t.Errorf("algorithm: got %v", rs["algorithm"])
	}
}

func TestResolver_Quantum_RingtailSignatureSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("RingtailSignature", "rs-1", map[string]interface{}{
		"id": "rs-1", "algorithm": "ML-DSA-65",
	})

	data := execOK(t, eng, `{ ringtailSignature(id: "rs-1") { id algorithm } }`)
	rs := requireMap(t, data, "ringtailSignature")
	if rs["algorithm"] != "ML-DSA-65" {
		t.Errorf("algorithm: got %v", rs["algorithm"])
	}
}

func TestResolver_Quantum_QuantumProofs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("QuantumProof", "qp-1", map[string]interface{}{
		"id": "qp-1", "proofType": "lattice", "securityLevel": 128,
	})

	data := execOK(t, eng, `{ quantumProofs(first: 10) { id } }`)
	requireList(t, data, "quantumProofs", 1)
}

func TestResolver_Quantum_PQKeyPairs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PQKeyPair", "kp-1", map[string]interface{}{
		"id": "kp-1", "algorithm": "ML-KEM-768", "publicKeyHash": "0xpkhash",
	})

	data := execOK(t, eng, `{ pqKeyPairs(first: 10) { id } }`)
	requireList(t, data, "pqKeyPairs", 1)
}

func TestResolver_Quantum_Stats(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("QuantumStats", "1", map[string]interface{}{
		"id": "1", "totalSignatures": 1000, "totalProofs": 500,
	})

	data := execOK(t, eng, `{ quantumStats { id totalSignatures totalProofs } }`)
	qs := requireMap(t, data, "quantumStats")
	if fmt.Sprint(qs["totalSignatures"]) != "1000" {
		t.Errorf("totalSignatures: got %v", qs["totalSignatures"])
	}
}

// ---------------------------------------------------------------------------
// 9. Key resolvers (K-Chain) — managed keys, rotations, ceremonies, attestations
// ---------------------------------------------------------------------------

func TestResolver_Key_ManagedKeys(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ManagedKey", "mk-1", map[string]interface{}{
		"id": "mk-1", "algorithm": "ECDSA-secp256k1", "owner": "0xabc", "status": "active",
	})

	data := execOK(t, eng, `{ managedKeys(first: 10) { id algorithm owner status } }`)
	list := requireList(t, data, "managedKeys", 1)
	mk := list[0].(map[string]interface{})
	if mk["algorithm"] != "ECDSA-secp256k1" {
		t.Errorf("algorithm: got %v", mk["algorithm"])
	}
}

func TestResolver_Key_ManagedKeySingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ManagedKey", "mk-1", map[string]interface{}{
		"id": "mk-1", "algorithm": "ECDSA-secp256k1",
	})

	data := execOK(t, eng, `{ managedKey(id: "mk-1") { id algorithm } }`)
	mk := requireMap(t, data, "managedKey")
	if mk["algorithm"] != "ECDSA-secp256k1" {
		t.Errorf("algorithm: got %v", mk["algorithm"])
	}
}

func TestResolver_Key_Rotations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("KeyRotation", "kr-1", map[string]interface{}{
		"id": "kr-1", "keyID": "mk-1", "oldPublicKey": "0xold", "newPublicKey": "0xnew",
	})

	data := execOK(t, eng, `{ keyRotations(first: 10) { id } }`)
	requireList(t, data, "keyRotations", 1)
}

func TestResolver_Key_Ceremonies(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("KeyCeremony", "kc-1", map[string]interface{}{
		"id": "kc-1", "participants": 5, "threshold": 3, "protocol": "FROST",
	})

	data := execOK(t, eng, `{ keyCeremonies(first: 10) { id } }`)
	requireList(t, data, "keyCeremonies", 1)
}

func TestResolver_Key_Attestations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("KeyAttestation", "ka-1", map[string]interface{}{
		"id": "ka-1", "keyID": "mk-1", "attester": "0xatt", "valid": true,
	})

	data := execOK(t, eng, `{ keyAttestations(first: 10) { id } }`)
	requireList(t, data, "keyAttestations", 1)
}

// ---------------------------------------------------------------------------
// 10. AI resolvers (A-Chain) — inference proofs, models, attestations, jobs
// ---------------------------------------------------------------------------

func TestResolver_AI_InferenceProofs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("InferenceProof", "ip-1", map[string]interface{}{
		"id": "ip-1", "modelHash": "0xmodel1", "inputHash": "0xinput1", "outputHash": "0xout1", "prover": "0xprover1",
	})

	data := execOK(t, eng, `{ inferenceProofs(first: 10) { id modelHash prover } }`)
	list := requireList(t, data, "inferenceProofs", 1)
	ip := list[0].(map[string]interface{})
	if ip["modelHash"] != "0xmodel1" {
		t.Errorf("modelHash: got %v", ip["modelHash"])
	}
}

func TestResolver_AI_InferenceProofSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("InferenceProof", "ip-1", map[string]interface{}{
		"id": "ip-1", "modelHash": "0xmodel1",
	})

	data := execOK(t, eng, `{ inferenceProof(id: "ip-1") { id modelHash } }`)
	ip := requireMap(t, data, "inferenceProof")
	if ip["modelHash"] != "0xmodel1" {
		t.Errorf("modelHash: got %v", ip["modelHash"])
	}
}

func TestResolver_AI_ModelHashes(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ModelHash", "mh-1", map[string]interface{}{
		"id": "mh-1", "hash": "0xhash", "name": "qwen3-70b", "version": "1.0",
	})

	data := execOK(t, eng, `{ modelHashes(first: 10) { id } }`)
	requireList(t, data, "modelHashes", 1)
}

func TestResolver_AI_ComputeAttestations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ComputeAttestation", "ca-1", map[string]interface{}{
		"id": "ca-1", "attester": "NodeID-abc", "gpuModel": "H100", "tflops": 989,
	})

	data := execOK(t, eng, `{ computeAttestations(first: 10) { id } }`)
	requireList(t, data, "computeAttestations", 1)
}

func TestResolver_AI_Jobs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("AIJob", "job-1", map[string]interface{}{
		"id": "job-1", "model": "qwen3-70b", "status": "running", "cost": "50",
	})

	data := execOK(t, eng, `{ aiJobs(first: 10) { id } }`)
	requireList(t, data, "aiJobs", 1)
}

func TestResolver_AI_TrainingRuns(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("TrainingRun", "tr-1", map[string]interface{}{
		"id": "tr-1", "dataset": "ds-1", "epochs": 10, "loss": "0.05",
	})

	data := execOK(t, eng, `{ trainingRuns(first: 10) { id } }`)
	requireList(t, data, "trainingRuns", 1)
}

// ---------------------------------------------------------------------------
// 11. Identity resolvers (I-Chain) — DIDs, credentials, attestations
// ---------------------------------------------------------------------------

func TestResolver_Identity_DIDs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DID", "did:lux:abc123", map[string]interface{}{
		"id": "did:lux:abc123", "controller": "0xabc", "created": 1711929600, "method": "lux",
	})

	data := execOK(t, eng, `{ dids(first: 10) { id controller method } }`)
	list := requireList(t, data, "dids", 1)
	did := list[0].(map[string]interface{})
	if did["method"] != "lux" {
		t.Errorf("method: got %v", did["method"])
	}
}

func TestResolver_Identity_DIDSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DID", "did:lux:abc123", map[string]interface{}{
		"id": "did:lux:abc123", "controller": "0xabc",
	})

	data := execOK(t, eng, `{ did(id: "did:lux:abc123") { id controller } }`)
	did := requireMap(t, data, "did")
	if did["controller"] != "0xabc" {
		t.Errorf("controller: got %v", did["controller"])
	}
}

func TestResolver_Identity_VerifiableCredentials(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("VerifiableCredential", "vc-1", map[string]interface{}{
		"id": "vc-1", "issuer": "did:lux:issuer1", "subject": "did:lux:abc123", "type": "KYC",
	})

	data := execOK(t, eng, `{ verifiableCredentials(first: 10) { id } }`)
	requireList(t, data, "verifiableCredentials", 1)
}

func TestResolver_Identity_Attestations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("Attestation", "att-1", map[string]interface{}{
		"id": "att-1", "attester": "did:lux:att1", "subject": "did:lux:abc123", "claim": "verified",
	})

	data := execOK(t, eng, `{ attestations(first: 10) { id } }`)
	requireList(t, data, "attestations", 1)
}

func TestResolver_Identity_CredentialSchemas(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("CredentialSchema", "cs-1", map[string]interface{}{
		"id": "cs-1", "name": "KYCSchema", "version": "1.0", "fields": 5,
	})

	data := execOK(t, eng, `{ credentialSchemas(first: 10) { id } }`)
	requireList(t, data, "credentialSchemas", 1)
}

// ---------------------------------------------------------------------------
// 12. Oracle resolvers (O-Chain) — price feeds, data requests, reports, nodes
// ---------------------------------------------------------------------------

func TestResolver_Oracle_PriceFeeds(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PriceFeed", "LUX-USD", map[string]interface{}{
		"id": "LUX-USD", "pair": "LUX/USD", "price": "2.50", "timestamp": 1711929600, "decimals": 8,
	})

	data := execOK(t, eng, `{ priceFeeds(first: 10) { id pair price } }`)
	list := requireList(t, data, "priceFeeds", 1)
	pf := list[0].(map[string]interface{})
	if pf["price"] != "2.50" {
		t.Errorf("price: got %v", pf["price"])
	}
}

func TestResolver_Oracle_PriceFeedSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PriceFeed", "LUX-USD", map[string]interface{}{
		"id": "LUX-USD", "price": "2.50",
	})

	data := execOK(t, eng, `{ priceFeed(id: "LUX-USD") { id price } }`)
	pf := requireMap(t, data, "priceFeed")
	if pf["price"] != "2.50" {
		t.Errorf("price: got %v", pf["price"])
	}
}

func TestResolver_Oracle_DataRequests(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("DataRequest", "dreq-1", map[string]interface{}{
		"id": "dreq-1", "requester": "0xabc", "dataType": "price", "status": "fulfilled",
	})

	data := execOK(t, eng, `{ dataRequests(first: 10) { id } }`)
	requireList(t, data, "dataRequests", 1)
}

func TestResolver_Oracle_OracleReports(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("OracleReport", "or-1", map[string]interface{}{
		"id": "or-1", "feedID": "LUX-USD", "value": "2.50", "reporter": "NodeID-abc",
	})

	data := execOK(t, eng, `{ oracleReports(first: 10) { id } }`)
	requireList(t, data, "oracleReports", 1)
}

func TestResolver_Oracle_OracleNodes(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("OracleNode", "on-1", map[string]interface{}{
		"id": "on-1", "nodeID": "NodeID-abc", "stake": "100000", "reputation": 95,
	})

	data := execOK(t, eng, `{ oracleNodes(first: 10) { id } }`)
	requireList(t, data, "oracleNodes", 1)
}

// ---------------------------------------------------------------------------
// 13. Relay resolvers (R-Chain) — warp messages, relay requests/proofs/routes
// ---------------------------------------------------------------------------

func TestResolver_Relay_WarpMessages(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("WarpMessage", "wm-1", map[string]interface{}{
		"id": "wm-1", "sourceChain": "C", "destChain": "Zoo", "payload": "0xdata", "timestamp": 1711929600,
	})

	data := execOK(t, eng, `{ warpMessages(first: 10) { id sourceChain destChain } }`)
	list := requireList(t, data, "warpMessages", 1)
	wm := list[0].(map[string]interface{})
	if wm["sourceChain"] != "C" {
		t.Errorf("sourceChain: got %v", wm["sourceChain"])
	}
}

func TestResolver_Relay_WarpMessageSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("WarpMessage", "wm-1", map[string]interface{}{
		"id": "wm-1", "sourceChain": "C", "destChain": "Zoo",
	})

	data := execOK(t, eng, `{ warpMessage(id: "wm-1") { id sourceChain } }`)
	wm := requireMap(t, data, "warpMessage")
	if wm["sourceChain"] != "C" {
		t.Errorf("sourceChain: got %v", wm["sourceChain"])
	}
}

func TestResolver_Relay_RelayRequests(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("RelayRequest", "rr-1", map[string]interface{}{
		"id": "rr-1", "messageID": "wm-1", "relayer": "0xrelayer", "fee": "10",
	})

	data := execOK(t, eng, `{ relayRequests(first: 10) { id } }`)
	requireList(t, data, "relayRequests", 1)
}

func TestResolver_Relay_RelayProofs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("RelayProof", "rp-1", map[string]interface{}{
		"id": "rp-1", "messageID": "wm-1", "proof": "0xproof", "verified": true,
	})

	data := execOK(t, eng, `{ relayProofs(first: 10) { id } }`)
	requireList(t, data, "relayProofs", 1)
}

func TestResolver_Relay_MessageReceipts(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("MessageReceipt", "mr-1", map[string]interface{}{
		"id": "mr-1", "messageID": "wm-1", "status": "delivered", "timestamp": 1711929700,
	})

	data := execOK(t, eng, `{ messageReceipts(first: 10) { id } }`)
	requireList(t, data, "messageReceipts", 1)
}

// ---------------------------------------------------------------------------
// 14. ServiceNode resolvers (S-Chain) — nodes, registrations, SLA, uptime
// ---------------------------------------------------------------------------

func TestResolver_ServiceNode_Nodes(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ServiceNode", "sn-1", map[string]interface{}{
		"id": "sn-1", "nodeID": "NodeID-abc", "stake": "500000", "serviceType": "rpc", "status": "active",
	})

	data := execOK(t, eng, `{ serviceNodes(first: 10) { id nodeID stake serviceType status } }`)
	list := requireList(t, data, "serviceNodes", 1)
	sn := list[0].(map[string]interface{})
	if sn["serviceType"] != "rpc" {
		t.Errorf("serviceType: got %v", sn["serviceType"])
	}
}

func TestResolver_ServiceNode_NodeSingle(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ServiceNode", "sn-1", map[string]interface{}{
		"id": "sn-1", "serviceType": "rpc",
	})

	data := execOK(t, eng, `{ serviceNode(id: "sn-1") { id serviceType } }`)
	sn := requireMap(t, data, "serviceNode")
	if sn["serviceType"] != "rpc" {
		t.Errorf("serviceType: got %v", sn["serviceType"])
	}
}

func TestResolver_ServiceNode_Registrations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ServiceRegistration", "sr-1", map[string]interface{}{
		"id": "sr-1", "nodeID": "sn-1", "service": "graphql", "endpoint": "https://graph.lux.network",
	})

	data := execOK(t, eng, `{ serviceRegistrations(first: 10) { id } }`)
	requireList(t, data, "serviceRegistrations", 1)
}

func TestResolver_ServiceNode_SLARecords(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("SLARecord", "sla-1", map[string]interface{}{
		"id": "sla-1", "nodeID": "sn-1", "uptime": "99.99", "latencyMs": 12,
	})

	data := execOK(t, eng, `{ slaRecords(first: 10) { id } }`)
	requireList(t, data, "slaRecords", 1)
}

func TestResolver_ServiceNode_UptimeProofs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("UptimeProof", "up-1", map[string]interface{}{
		"id": "up-1", "nodeID": "sn-1", "timestamp": 1711929600, "proof": "0xuptimeproof",
	})

	data := execOK(t, eng, `{ uptimeProofs(first: 10) { id } }`)
	requireList(t, data, "uptimeProofs", 1)
}

func TestResolver_ServiceNode_Endpoints(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ServiceEndpoint", "ep-1", map[string]interface{}{
		"id": "ep-1", "nodeID": "sn-1", "url": "https://rpc.lux.network", "protocol": "jsonrpc",
	})

	data := execOK(t, eng, `{ serviceEndpoints(first: 10) { id } }`)
	requireList(t, data, "serviceEndpoints", 1)
}

// ---------------------------------------------------------------------------
// 15. Precompile resolvers — all precompile types
// ---------------------------------------------------------------------------

func TestResolver_Precompile_Calls(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PrecompileCall", "pc-1", map[string]interface{}{
		"id": "pc-1", "address": "0x0200000000000000000000000000000000000007", "caller": "0xabc", "gasUsed": 21000,
	})

	data := execOK(t, eng, `{ precompileCalls(first: 10) { id address caller gasUsed } }`)
	list := requireList(t, data, "precompileCalls", 1)
	pc := list[0].(map[string]interface{})
	if fmt.Sprint(pc["gasUsed"]) != "21000" {
		t.Errorf("gasUsed: got %v", pc["gasUsed"])
	}
}

func TestResolver_Precompile_AIWorkProofs(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("AIWorkProof", "awp-1", map[string]interface{}{
		"id": "awp-1", "miner": "0xminer", "nonce": 42, "difficulty": "1000000",
	})

	data := execOK(t, eng, `{ aiWorkProofs(first: 10) { id } }`)
	requireList(t, data, "aiWorkProofs", 1)
}

func TestResolver_Precompile_FHEOperations(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("FHEOperation", "fheop-1", map[string]interface{}{
		"id": "fheop-1", "operation": "add", "gasUsed": 500000,
	})

	data := execOK(t, eng, `{ fheOperations(first: 10) { id } }`)
	requireList(t, data, "fheOperations", 1)
}

func TestResolver_Precompile_ZKVerifications(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ZKVerification", "zkv-1", map[string]interface{}{
		"id": "zkv-1", "proofSystem": "groth16", "verified": true,
	})

	data := execOK(t, eng, `{ zkVerifications(first: 10) { id } }`)
	requireList(t, data, "zkVerifications", 1)
}

func TestResolver_Precompile_RingSignatures(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("RingSignature", "ring-1", map[string]interface{}{
		"id": "ring-1", "ringSize": 8, "keyImage": "0xkeyimage",
	})

	data := execOK(t, eng, `{ ringSignatures(first: 10) { id } }`)
	requireList(t, data, "ringSignatures", 1)
}

func TestResolver_Precompile_PQCryptoOps(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("PQCryptoOp", "pq-1", map[string]interface{}{
		"id": "pq-1", "algorithm": "ML-DSA-65", "operation": "verify",
	})

	data := execOK(t, eng, `{ pqCryptoOps(first: 10) { id } }`)
	requireList(t, data, "pqCryptoOps", 1)
}

func TestResolver_Precompile_ThresholdOps(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("ThresholdOp", "top-1", map[string]interface{}{
		"id": "top-1", "protocol": "FROST", "participants": 5, "threshold": 3,
	})

	data := execOK(t, eng, `{ thresholdOps(first: 10) { id } }`)
	requireList(t, data, "thresholdOps", 1)
}

func TestResolver_Precompile_WarpCalls(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("WarpCall", "wc-1", map[string]interface{}{
		"id": "wc-1", "sourceChain": "C", "destChain": "Zoo", "messageID": "0xmsg",
	})

	data := execOK(t, eng, `{ warpCalls(first: 10) { id } }`)
	requireList(t, data, "warpCalls", 1)
}

func TestResolver_Precompile_FHEACLGrants(t *testing.T) {
	eng, store := newTestEngine(t)
	store.SetEntity("FHEACLGrant", "acl-1", map[string]interface{}{
		"id": "acl-1", "ciphertextID": "ct-1", "grantee": "0xabc",
	})

	data := execOK(t, eng, `{ fheACLGrants(first: 10) { id } }`)
	requireList(t, data, "fheACLGrants", 1)
}

// ---------------------------------------------------------------------------
// 16. Stats resolvers — singleton stats for each chain
// ---------------------------------------------------------------------------

func TestResolver_Stats_All(t *testing.T) {
	eng, store := newTestEngine(t)

	store.SetEntity("BridgeStats", "1", map[string]interface{}{
		"id": "1", "totalTransfers": 5000, "totalVolume": "10000000",
	})
	store.SetEntity("FHEStats", "1", map[string]interface{}{
		"id": "1", "totalCeremonies": 100, "totalComputeJobs": 500,
	})
	store.SetEntity("OracleStats", "1", map[string]interface{}{
		"id": "1", "totalFeeds": 50, "totalReports": 10000,
	})
	store.SetEntity("RelayStats", "1", map[string]interface{}{
		"id": "1", "totalMessages": 2000, "totalRelayed": 1999,
	})
	store.SetEntity("ServiceStats", "1", map[string]interface{}{
		"id": "1", "totalNodes": 100, "averageUptime": "99.95",
	})
	store.SetEntity("KeyStats", "1", map[string]interface{}{
		"id": "1", "totalKeys": 500, "totalRotations": 50,
	})
	store.SetEntity("AIStats", "1", map[string]interface{}{
		"id": "1", "totalInferences": 100000, "totalModels": 25,
	})
	store.SetEntity("IdentityStats", "1", map[string]interface{}{
		"id": "1", "totalDIDs": 10000, "totalCredentials": 50000,
	})

	tests := []struct {
		query string
		key   string
	}{
		{`{ bridgeStats { id totalTransfers } }`, "bridgeStats"},
		{`{ fheStats { id totalCeremonies } }`, "fheStats"},
		{`{ oracleStats { id totalFeeds } }`, "oracleStats"},
		{`{ relayStats { id totalMessages } }`, "relayStats"},
		{`{ serviceStats { id totalNodes } }`, "serviceStats"},
		{`{ keyStats { id totalKeys } }`, "keyStats"},
		{`{ aiStats { id totalInferences } }`, "aiStats"},
		{`{ identityStats { id totalDIDs } }`, "identityStats"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			data := execOK(t, eng, tt.query)
			m := requireMap(t, data, tt.key)
			requireField(t, m, "id")
		})
	}
}

// ---------------------------------------------------------------------------
// 17. Error cases — missing ID, unknown field
// ---------------------------------------------------------------------------

func TestResolver_ErrorMissingID(t *testing.T) {
	eng, _ := newTestEngine(t)

	tests := []struct {
		name  string
		query string
	}{
		{"order", `{ order { id } }`},
		{"bridgeTransfer", `{ bridgeTransfer { id } }`},
		{"validator", `{ validator { id } }`},
		{"asset", `{ asset { id } }`},
		{"shieldedTransfer", `{ shieldedTransfer { id } }`},
		{"ringtailSignature", `{ ringtailSignature { id } }`},
		{"managedKey", `{ managedKey { id } }`},
		{"inferenceProof", `{ inferenceProof { id } }`},
		{"did", `{ did { id } }`},
		{"priceFeed", `{ priceFeed { id } }`},
		{"warpMessage", `{ warpMessage { id } }`},
		{"serviceNode", `{ serviceNode { id } }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := execErr(t, eng, tt.query)
			if msg == "" {
				t.Fatal("expected non-empty error message")
			}
		})
	}
}

func TestResolver_UnknownField(t *testing.T) {
	eng, _ := newTestEngine(t)
	msg := execErr(t, eng, `{ nonexistentResolver(first: 10) { id } }`)
	if msg == "" {
		t.Fatal("expected error for unknown field")
	}
}

// ---------------------------------------------------------------------------
// 18. Empty store returns empty lists, not errors
// ---------------------------------------------------------------------------

func TestResolver_EmptyStore_AllChains(t *testing.T) {
	eng, _ := newTestEngine(t)

	queries := []struct {
		name  string
		query string
		key   string
	}{
		{"orders", `{ orders(first: 10) { id } }`, "orders"},
		{"validators", `{ validators(first: 10) { id } }`, "validators"},
		{"assets", `{ assets(first: 10) { id } }`, "assets"},
		{"bridgeTransfers", `{ bridgeTransfers(first: 10) { id } }`, "bridgeTransfers"},
		{"dkgCeremonies", `{ dkgCeremonies(first: 10) { id } }`, "dkgCeremonies"},
		{"shieldedTransfers", `{ shieldedTransfers(first: 10) { id } }`, "shieldedTransfers"},
		{"ringtailSignatures", `{ ringtailSignatures(first: 10) { id } }`, "ringtailSignatures"},
		{"managedKeys", `{ managedKeys(first: 10) { id } }`, "managedKeys"},
		{"inferenceProofs", `{ inferenceProofs(first: 10) { id } }`, "inferenceProofs"},
		{"dids", `{ dids(first: 10) { id } }`, "dids"},
		{"priceFeeds", `{ priceFeeds(first: 10) { id } }`, "priceFeeds"},
		{"warpMessages", `{ warpMessages(first: 10) { id } }`, "warpMessages"},
		{"serviceNodes", `{ serviceNodes(first: 10) { id } }`, "serviceNodes"},
		{"precompileCalls", `{ precompileCalls(first: 10) { id } }`, "precompileCalls"},
	}

	for _, tt := range queries {
		t.Run(tt.name, func(t *testing.T) {
			data := execOK(t, eng, tt.query)
			// Either nil or empty slice is acceptable for empty store
			if data[tt.key] == nil {
				return
			}
			if list, ok := data[tt.key].([]interface{}); ok && len(list) > 0 {
				t.Fatalf("expected empty list for %s, got %d items", tt.key, len(list))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 19. First limit parameter works across all chain resolvers
// ---------------------------------------------------------------------------

func TestResolver_FirstLimitRespected(t *testing.T) {
	eng, store := newTestEngine(t)

	// Seed 5 orders
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("order-%d", i)
		store.SetEntity("Order", id, map[string]interface{}{
			"id": id, "market": "LUX/USD", "side": "buy", "price": "2.50",
		})
	}

	data := execOK(t, eng, `{ orders(first: 2) { id } }`)
	list := requireList(t, data, "orders", 1)
	if len(list) > 2 {
		t.Fatalf("first:2 should return <= 2 items, got %d", len(list))
	}
}

// ---------------------------------------------------------------------------
// 20. Single entity lookup returns nil for nonexistent ID (no error)
// ---------------------------------------------------------------------------

func TestResolver_NonexistentEntity_ReturnsNil(t *testing.T) {
	eng, _ := newTestEngine(t)

	// These should return nil data for the field, not an error
	data := execOK(t, eng, `{ validator(id: "NodeID-doesnotexist") { id } }`)
	if data["validator"] != nil {
		t.Errorf("expected nil for nonexistent validator, got %v", data["validator"])
	}

	data = execOK(t, eng, `{ bridgeTransfer(id: "nonexistent") { id } }`)
	if data["bridgeTransfer"] != nil {
		t.Errorf("expected nil for nonexistent bridgeTransfer, got %v", data["bridgeTransfer"])
	}
}
