package indexer

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luxfi/graph/storage"
)

// fixedGenesis returns a genesisHashFn that always reports the same hash — the
// signature of a canonical chain (no re-genesis), even when a backend is lagging.
func fixedGenesis(hash string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return hash, nil }
}

// word renders a non-negative big.Int as a 64-hex-char ABI word.
func word(n *big.Int) string { return fmt.Sprintf("%064x", n) }

// signedWord renders a (possibly negative) big.Int as a two's-complement
// 64-hex-char ABI word — the on-wire form for V4 int128 amounts.
func signedWord(n *big.Int) string {
	if n.Sign() >= 0 {
		return word(n)
	}
	twos := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 256), n)
	return fmt.Sprintf("%064x", twos)
}

// topic32 left-pads a hex value to a 32-byte (66-char 0x) topic.
func topic32(hexNo0x string) string { return "0x" + fmt.Sprintf("%064s", hexNo0x) }

// addrTopic encodes an address as an indexed topic (left-padded to 32 bytes).
func addrTopic(addr string) string {
	return "0x" + fmt.Sprintf("%064s", addr[2:])
}

func newMemSQLiteStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDecodeInt256_SignedV4Amounts(t *testing.T) {
	cases := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(251250000),
		big.NewInt(-1),
		big.NewInt(-251250000),
	}
	for _, want := range cases {
		data := "0x" + signedWord(want)
		got := decodeInt256(data, 0)
		if got.Cmp(want) != 0 {
			t.Errorf("decodeInt256(%s) = %s, want %s", want, got, want)
		}
	}
}

// A V4 swap records ONLY an AMM swap (with signed legs), never a CLOB Fill — the
// Fill has a single source (the DEXFill event, see TestHandleDEXFill_*). This
// holds whether or not the emitter is the 0x9999 settlement precompile: the
// AMM-side view and the CLOB-side view come from distinct events.
func TestHandleSwapV4_WritesAmmSwapOnly(t *testing.T) {
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused", PoolManager: LXSettleAddress}, s)

	poolID := topic32("abcd")
	taker := "0x00000000000000000000000000000000000000aa"
	amount0 := big.NewInt(1000)  // taker pays in
	amount1 := big.NewInt(-2500) // taker receives out (negative to pool)

	l := &logEntry{
		Address:         LXSettleAddress, // even at 0x9999 — still AMM-only here
		Topics:          []string{SigSwapV4, poolID, addrTopic(taker)},
		Data:            "0x" + signedWord(amount0) + signedWord(amount1) + word(big.NewInt(0)) + word(big.NewInt(0)) + word(big.NewInt(0)) + word(big.NewInt(3000)),
		BlockNumber:     "0x10",
		TransactionHash: "0xdeadbeef",
		LogIndex:        "0x0",
	}
	idx.processLog(context.Background(), l)

	// AMM swap present with correctly signed legs.
	sw, err := s.GetSwaps(nil, 10, "timestamp", "desc", nil)
	if err != nil {
		t.Fatal(err)
	}
	swaps := sw.([]interface{})
	if len(swaps) != 1 {
		t.Fatalf("want 1 amm swap, got %d", len(swaps))
	}
	if swaps[0].(map[string]interface{})["amount1"] != "-2500" {
		t.Errorf("amount1 = %v, want -2500 (signed)", swaps[0].(map[string]interface{})["amount1"])
	}

	// No Fill — a V4 Swap is never a CLOB fill (that comes from DEXFill only).
	fills, _ := s.ListByType("Fill", 10)
	if fills != nil && len(fills.([]interface{})) != 0 {
		t.Fatalf("V4 swap must not create a fill (DEXFill is the only source), got %d", len(fills.([]interface{})))
	}
}

// An explicit DEXFill at 0x9999 creates a Fill and rolls market volume; a
// spoofed DEXFill from another address is ignored.
func TestHandleDEXFill_ScopedToPoolManager(t *testing.T) {
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused"}, s) // default 0x9999

	poolID := topic32("cafe")
	taker := "0x00000000000000000000000000000000000000cc"
	mkFill := func(addr, tx string, out int64) *logEntry {
		return &logEntry{
			Address:         addr,
			Topics:          []string{SigDEXFill, poolID, addrTopic(taker)},
			Data:            "0x" + word(big.NewInt(out)) + word(big.NewInt(99)),
			BlockNumber:     "0x30",
			TransactionHash: tx,
			LogIndex:        "0x0",
		}
	}
	// Legit fill at 0x9999.
	idx.processLog(context.Background(), mkFill(LXSettleAddress, "0xaa", 400))
	// Spoofed fill from a rogue address — must be dropped.
	idx.processLog(context.Background(), mkFill("0x0000000000000000000000000000000000009998", "0xbb", 999999))

	fills, _ := s.ListByType("Fill", 10)
	fl := fills.([]interface{})
	if len(fl) != 1 {
		t.Fatalf("want exactly 1 fill (spoof dropped), got %d", len(fl))
	}
	mk, _ := s.GetByType("Market", poolID)
	mm := mk.(map[string]interface{})
	if mm["volume24h"] != "400" {
		t.Errorf("market volume24h = %v, want 400 (spoof excluded)", mm["volume24h"])
	}
}

// initV4Log builds an Initialize log in the EXACT shape the 0x9999 precompile's
// emitInitializeEvent emits (verified by the producer test
// TestInitializeEvent_EmittedAt9999WithPairAndFee): topics [sig, poolId, currency0,
// currency1]; data = fee(uint24) | tickSpacing(int24) | hooks(address) |
// sqrtPriceX96(uint160) | tick(int24). NOT a fabricated layout — this mirrors the
// real producer so the consumer test exercises the genuine wire format.
func initV4Log(poolID, cur0, cur1 string, fee, tickSpacing, sqrtPriceX96, tick int64, blockHex string) *logEntry {
	return &logEntry{
		Address: LXSettleAddress,
		Topics:  []string{SigInitializeV4, poolID, addrTopic(cur0), addrTopic(cur1)},
		Data: "0x" + word(big.NewInt(fee)) + word(big.NewInt(tickSpacing)) +
			word(new(big.Int)) /*hooks=0*/ + word(big.NewInt(sqrtPriceX96)) + word(big.NewInt(tick)),
		BlockNumber: blockHex,
	}
}

// Initialize at 0x9999 creates an AMM pool, bumps the factory aggregate, and
// creates a RICH DEX Market carrying the token pair + fee tier (FIX B(a)) — the
// fields the native producer (emitInitializeEvent at 0x9999) now supplies.
func TestHandleInitializeV4_PoolMarketFactory(t *testing.T) {
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused"}, s)

	poolID := topic32("d00d")
	cur0 := "0x0000000000000000000000000000000000000011"
	cur1 := "0x0000000000000000000000000000000000000022"
	idx.processLog(context.Background(), initV4Log(poolID, cur0, cur1, 3000, 60, 0, 0, "0x1"))

	if p, _ := s.GetPool(nil, poolID); p == nil {
		t.Fatal("expected pool from InitializeV4")
	}
	// The create path owns the factory tx count; poolCount is derived from the
	// store by the valuation pass (TestRevalue_DerivesTVLPricesAndPoolCount).
	f, _ := s.GetFactory(nil, "1")
	if f == nil || asInt64(f.(map[string]interface{})["txCount"]) != 1 {
		t.Fatalf("expected factory txCount=1, got %v", f)
	}
	mk, _ := s.GetByType("Market", poolID)
	if mk == nil {
		t.Fatal("expected DEX Market from InitializeV4 at 0x9999")
	}
	mm := mk.(map[string]interface{})
	// The Market is RICH, not a poolID-only stub: token pair + fee tier present.
	if fmt.Sprint(mm["baseToken"]) != cur0 {
		t.Errorf("Market baseToken = %v, want %s", mm["baseToken"], cur0)
	}
	if fmt.Sprint(mm["quoteToken"]) != cur1 {
		t.Errorf("Market quoteToken = %v, want %s", mm["quoteToken"], cur1)
	}
	if asInt64(mm["feeTier"]) != 3000 {
		t.Errorf("Market feeTier = %v, want 3000", mm["feeTier"])
	}
}

// A Fill that arrives BEFORE its market's Initialize creates a volume stub; the
// later Initialize must MERGE — adding the rich token pair + fee tier WITHOUT
// resetting the accumulated volume/tradeCount to zero (LOW: stub-vs-rich merge).
func TestHandleInitializeV4_MergesStubVolume(t *testing.T) {
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused"}, s)

	poolID := topic32("beef")
	taker := "0x00000000000000000000000000000000000000aa"
	// 1) A DEXFill lands first → writeFill creates a Market stub with volume=400.
	idx.processLog(context.Background(), &logEntry{
		Address: LXSettleAddress, Topics: []string{SigDEXFill, poolID, addrTopic(taker)},
		Data: "0x" + word(big.NewInt(400)) + word(big.NewInt(7)), BlockNumber: "0x5", TransactionHash: "0xaa", LogIndex: "0x0",
	})
	// 2) Initialize arrives later → must enrich, not wipe.
	cur0 := "0x0000000000000000000000000000000000000011"
	cur1 := "0x0000000000000000000000000000000000000022"
	idx.processLog(context.Background(), initV4Log(poolID, cur0, cur1, 500, 10, 0, 0, "0x6"))

	mk, _ := s.GetByType("Market", poolID)
	mm := mk.(map[string]interface{})
	if mm["volume24h"] != "400" {
		t.Errorf("Initialize must preserve accumulated volume, got %v want 400", mm["volume24h"])
	}
	if asInt64(mm["tradeCount"]) != 1 {
		t.Errorf("Initialize must preserve tradeCount, got %v want 1", mm["tradeCount"])
	}
	if fmt.Sprint(mm["baseToken"]) != cur0 || asInt64(mm["feeTier"]) != 500 {
		t.Errorf("Initialize must add rich fields: baseToken=%v feeTier=%v", mm["baseToken"], mm["feeTier"])
	}
}

// bumpFactory must PRESERVE the USD aggregates across its read-modify-write — the
// landing-page header's TVL/volume. SeedFactory is INSERT OR REPLACE, so writing
// them empty on every pool-create/swap would clobber them (MEDIUM).
func TestBumpFactory_PreservesTVLAndVolume(t *testing.T) {
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused"}, s)

	// Seed the factory with USD aggregates (as the price-oracle path would).
	s.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount: 2, TxCount: 5, TotalValueLockedUSD: "1000000", TotalVolumeUSD: "5000000",
	})

	// A pool-create bumps the tx count — and must not disturb any field it does
	// not own: not the USD aggregates, and not poolCount (derived elsewhere).
	idx.bumpFactory()

	f, _ := s.GetFactory(nil, "1")
	fm := f.(map[string]interface{})
	if asInt64(fm["txCount"]) != 6 {
		t.Errorf("txCount = %v, want 6", fm["txCount"])
	}
	if asInt64(fm["poolCount"]) != 2 {
		t.Errorf("poolCount must be carried through untouched: got %v want 2", fm["poolCount"])
	}
	if fmt.Sprint(fm["totalValueLockedUSD"]) != "1000000" {
		t.Errorf("totalValueLockedUSD clobbered: got %v want 1000000", fm["totalValueLockedUSD"])
	}
	if fmt.Sprint(fm["totalVolumeUSD"]) != "5000000" {
		t.Errorf("totalVolumeUSD clobbered: got %v want 5000000", fm["totalVolumeUSD"])
	}
}

// The re-genesis self-heal: a persisted cursor far above a freshly-relaunched
// chain's head must rewind to StartBlock so the new chain indexes from genesis —
// but only after the low head is CONFIRMED across regenesisConfirmations polls AND
// the genesis hash has actually CHANGED (the deterministic re-genesis signal).
func TestPoll_RegenesisSelfHeal(t *testing.T) {
	s := newMemSQLiteStore(t)
	s.SetLastBlock(5_000_000) // stale cursor from the previous chain
	idx := NewWithConfig(Config{RPC: "http://unused", StartBlock: 0}, s)
	if idx.lastBlock != 5_000_000 {
		t.Fatalf("precondition: lastBlock=%d", idx.lastBlock)
	}
	ctx := context.Background()
	// Baseline: the OLD chain's genesis hash, captured before the relaunch.
	idx.genesisHash = "0xold"
	// The fresh chain reports a DIFFERENT genesis hash — the re-genesis signal.
	idx.genesisHashFn = fixedGenesis("0xnew")

	// A fresh chain reports head=3, far below cursor — sustained across the
	// confirmation window. Only the FINAL confirming poll rewinds.
	for i := 0; i < regenesisConfirmations-1; i++ {
		idx.maybeResetForRegenesis(ctx, 3)
		if idx.lastBlock != 5_000_000 {
			t.Fatalf("must not rewind before %d confirmations (poll %d), lastBlock=%d", regenesisConfirmations, i+1, idx.lastBlock)
		}
	}
	idx.maybeResetForRegenesis(ctx, 3) // the confirming poll
	if idx.lastBlock != 0 {
		t.Fatalf("expected rewind to StartBlock 0 after %d confirmations + changed genesis, got %d", regenesisConfirmations, idx.lastBlock)
	}
	if s.GetLastBlock() != 0 {
		t.Fatalf("expected persisted cursor reset to 0, got %d", s.GetLastBlock())
	}
	if idx.genesisHash != "0xnew" {
		t.Fatalf("expected baseline to advance to the new chain's genesis, got %s", idx.genesisHash)
	}
}

// A shallow reorg (head dips a few blocks) must NOT trigger a reset.
func TestPoll_ShallowReorgNoReset(t *testing.T) {
	s := newMemSQLiteStore(t)
	s.SetLastBlock(1000)
	idx := NewWithConfig(Config{RPC: "http://unused", StartBlock: 0}, s)
	idx.genesisHash = "0xsame"
	idx.genesisHashFn = func(context.Context) (string, error) {
		t.Fatal("shallow reorg must not even probe the genesis hash")
		return "", nil
	}

	idx.maybeResetForRegenesis(context.Background(), 995) // 5 blocks back — within reorgDepth
	if idx.lastBlock != 1000 {
		t.Fatalf("shallow reorg must not reset; lastBlock=%d", idx.lastBlock)
	}
}

// An idle/lagging RPC that momentarily reports a low head and then RECOVERS must
// NOT wipe the cursor (re-genesis hysteresis — the DoS-amplification case Red
// flagged: a single low reading must not trigger a full re-index from genesis).
func TestPoll_IdleLowHeadRecoversNoWipe(t *testing.T) {
	s := newMemSQLiteStore(t)
	s.SetLastBlock(5_000_000)
	idx := NewWithConfig(Config{RPC: "http://unused", StartBlock: 0}, s)
	ctx := context.Background()
	idx.genesisHash = "0xsame"
	idx.genesisHashFn = func(context.Context) (string, error) {
		t.Fatal("transient/recovering low head must not reach the genesis probe")
		return "", nil
	}

	// A couple of transient low readings (idle "Building block" / lag) — fewer than
	// the confirmation window — must not rewind.
	idx.maybeResetForRegenesis(ctx, 0) // idle: eth_blockNumber=0x0
	idx.maybeResetForRegenesis(ctx, 1)
	if idx.lastBlock != 5_000_000 {
		t.Fatalf("transient low head must not wipe; lastBlock=%d", idx.lastBlock)
	}
	// RPC recovers to the true height — the streak must clear, no wipe ever.
	idx.maybeResetForRegenesis(ctx, 5_000_001)
	if idx.lastBlock != 5_000_000 {
		t.Fatalf("recovered head must not wipe; lastBlock=%d", idx.lastBlock)
	}
	if idx.lowHeadStreak != 0 {
		t.Fatalf("recovery must clear the low-head streak, got %d", idx.lowHeadStreak)
	}
	// And a subsequent single low blip still must not wipe (streak restarts at 1).
	idx.maybeResetForRegenesis(ctx, 2)
	if idx.lastBlock != 5_000_000 {
		t.Fatalf("post-recovery single blip must not wipe; lastBlock=%d", idx.lastBlock)
	}
}

// TestPoll_DeeplyBehindButCanonicalNoWipe is the item-4 hardening: a deeply-stale
// load-balanced RPC backend can report a head >reorgDepth below our cursor for the
// FULL confirmation window — satisfying the poll-count pre-filter — yet the chain is
// still canonical. The genesis hash is UNCHANGED, so the cursor must NOT be wiped.
// Under the old poll-count-only gate this scenario wiped progress and re-indexed
// from genesis (the false-positive Red flagged).
func TestPoll_DeeplyBehindButCanonicalNoWipe(t *testing.T) {
	s := newMemSQLiteStore(t)
	s.SetLastBlock(5_000_000)
	idx := NewWithConfig(Config{RPC: "http://unused", StartBlock: 0}, s)
	ctx := context.Background()
	idx.genesisHash = "0xcanonical"
	// The cursor block hash is still valid / genesis unchanged — same chain, just behind.
	probes := 0
	idx.genesisHashFn = func(context.Context) (string, error) {
		probes++
		return "0xcanonical", nil
	}

	// Hold the head deeply below the cursor for WELL past the confirmation window.
	for i := 0; i < regenesisConfirmations*3; i++ {
		idx.maybeResetForRegenesis(ctx, 3)
		if idx.lastBlock != 5_000_000 {
			t.Fatalf("deeply-behind-but-canonical RPC must NOT wipe (iter %d); lastBlock=%d", i, idx.lastBlock)
		}
	}
	if s.GetLastBlock() != 5_000_000 {
		t.Fatalf("persisted cursor must be intact; got %d", s.GetLastBlock())
	}
	if probes == 0 {
		t.Fatal("expected at least one genesis-hash probe once the streak confirmed")
	}
}

// TestPoll_RegenesisHashChangeWipes is the positive companion: a true re-genesis
// (genesis hash GONE/changed) must wipe to StartBlock, but only after the streak
// confirms. This is the "cursor hash gone ⇒ wipe" half of the deterministic gate.
func TestPoll_RegenesisHashChangeWipes(t *testing.T) {
	s := newMemSQLiteStore(t)
	s.SetLastBlock(5_000_000)
	idx := NewWithConfig(Config{RPC: "http://unused", StartBlock: 0}, s)
	ctx := context.Background()
	idx.genesisHash = "0xchainA"
	idx.genesisHashFn = fixedGenesis("0xchainB") // fresh chain — different genesis

	// Pre-confirmation polls must not wipe even though the hash differs (hysteresis
	// still gates the probe; we only probe once the streak completes).
	for i := 0; i < regenesisConfirmations-1; i++ {
		idx.maybeResetForRegenesis(ctx, 2)
		if idx.lastBlock != 5_000_000 {
			t.Fatalf("must not wipe before confirmation (iter %d)", i)
		}
	}
	idx.maybeResetForRegenesis(ctx, 2) // confirming poll — genesis changed ⇒ wipe
	if idx.lastBlock != 0 || s.GetLastBlock() != 0 {
		t.Fatalf("changed genesis must wipe to StartBlock; lastBlock=%d persisted=%d", idx.lastBlock, s.GetLastBlock())
	}
}

// TestPoll_RegenesisProbeFailsNoWipe: if the genesis probe errors at the confirming
// poll, we cannot prove a reset, so we must NOT wipe.
func TestPoll_RegenesisProbeFailsNoWipe(t *testing.T) {
	s := newMemSQLiteStore(t)
	s.SetLastBlock(5_000_000)
	idx := NewWithConfig(Config{RPC: "http://unused", StartBlock: 0}, s)
	ctx := context.Background()
	idx.genesisHash = "0xbaseline"
	idx.genesisHashFn = func(context.Context) (string, error) {
		return "", fmt.Errorf("backend unreachable")
	}

	for i := 0; i < regenesisConfirmations+2; i++ {
		idx.maybeResetForRegenesis(ctx, 3)
	}
	if idx.lastBlock != 5_000_000 {
		t.Fatalf("probe failure must not wipe; lastBlock=%d", idx.lastBlock)
	}
}

// MEDIUM: AMM V4 Initialize/Swap MUST be scoped to the canonical 0x9999
// PoolManager. A lookalike Initialize/Swap emitted by any OTHER contract must be
// rejected — otherwise anyone can seed phantom pools/tokens/swaps and inflate the
// explorer's public TVL/volume aggregates. (handleDEXFill is already so scoped;
// this proves Initialize and Swap now mirror it.)
func TestHandleV4_RejectsNonPoolManager(t *testing.T) {
	const rogue = "0x000000000000000000000000000000000000dEaD"
	cur0 := "0x0000000000000000000000000000000000000011"
	cur1 := "0x0000000000000000000000000000000000000022"

	// --- Initialize from a rogue contract: nothing seeded. ---
	t.Run("Initialize", func(t *testing.T) {
		s := newMemSQLiteStore(t)
		idx := NewWithConfig(Config{RPC: "http://unused"}, s) // default 0x9999

		poolID := topic32("1111")
		spoof := initV4Log(poolID, cur0, cur1, 3000, 60, 0, 0, "0x1")
		spoof.Address = rogue // same wire shape, wrong emitter
		idx.processLog(context.Background(), spoof)

		if p, _ := s.GetPool(nil, poolID); p != nil {
			t.Error("rogue Initialize must NOT seed a pool")
		}
		if tok, _ := s.GetByType("Token", cur0); tok != nil {
			t.Error("rogue Initialize must NOT seed a token")
		}
		if mk, _ := s.GetByType("Market", poolID); mk != nil {
			t.Error("rogue Initialize must NOT seed a DEX Market")
		}
		if f, _ := s.GetFactory(nil, "1"); f != nil && asInt64(f.(map[string]interface{})["poolCount"]) != 0 {
			t.Errorf("rogue Initialize must NOT inflate factory poolCount, got %v", f)
		}

		// The SAME event from the real 0x9999 IS honored (gate is the emitter, not the shape).
		idx.processLog(context.Background(), initV4Log(poolID, cur0, cur1, 3000, 60, 0, 0, "0x1"))
		if p, _ := s.GetPool(nil, poolID); p == nil {
			t.Fatal("canonical 0x9999 Initialize must seed a pool")
		}
	})

	// --- Swap from a rogue contract: no AMM swap, no volume. ---
	t.Run("Swap", func(t *testing.T) {
		s := newMemSQLiteStore(t)
		idx := NewWithConfig(Config{RPC: "http://unused"}, s)

		poolID := topic32("2222")
		taker := "0x00000000000000000000000000000000000000aa"
		mkSwap := func(addr, tx string) *logEntry {
			return &logEntry{
				Address: addr,
				Topics:  []string{SigSwapV4, poolID, addrTopic(taker)},
				Data: "0x" + signedWord(big.NewInt(1000)) + signedWord(big.NewInt(-2500)) +
					word(new(big.Int)) + word(new(big.Int)) + word(new(big.Int)) + word(big.NewInt(3000)),
				BlockNumber: "0x10", TransactionHash: tx, LogIndex: "0x0",
			}
		}
		idx.processLog(context.Background(), mkSwap(rogue, "0xspoof")) // rogue — dropped
		sw, _ := s.GetSwaps(nil, 10, "timestamp", "desc", nil)
		if sw != nil && len(sw.([]interface{})) != 0 {
			t.Fatalf("rogue Swap must NOT create an AMM swap, got %d", len(sw.([]interface{})))
		}

		idx.processLog(context.Background(), mkSwap(LXSettleAddress, "0xreal")) // canonical — honored
		sw, _ = s.GetSwaps(nil, 10, "timestamp", "desc", nil)
		if sw == nil || len(sw.([]interface{})) != 1 {
			t.Fatalf("canonical 0x9999 Swap must create exactly 1 AMM swap")
		}
	})
}

// LOW (DoS): a malformed RPC log (short/empty topics, truncated hex) must NOT
// crash the poll goroutine. processLog handlers length-check, and pollGuarded's
// recover() is the process-survival backstop. We assert (a) malformed logs are
// processed without panic, and (b) pollGuarded turns any panic into an error
// rather than tearing down the process.
func TestPoll_MalformedLogDoesNotCrash(t *testing.T) {
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused"}, s)

	// Each of these previously risked a slice-out-of-range panic via an unguarded
	// fixed-offset slice on a short field. None may panic now.
	malformed := []*logEntry{
		// PairCreated with a too-short data word.
		{Address: "0xab", Topics: []string{SigPairCreated, topic32("1"), topic32("2")}, Data: "0x1234"},
		// PoolCreated whose currency topics are themselves short.
		{Address: "0xab", Topics: []string{SigPoolCreated, "0x12", "0x34", "0x1f"}, Data: "0x" + word(big.NewInt(1))},
		// Transfer from a 2-char address (l.Address[:8] used to panic here).
		{Address: "0xa", Topics: []string{SigTransfer, topic32("1"), topic32("2")}, Data: "0x"},
		// InitializeV4 / SwapV4 at 0x9999 with truncated currency topics.
		{Address: LXSettleAddress, Topics: []string{SigInitializeV4, topic32("3"), "0x1", "0x2"}, Data: "0x"},
		{Address: LXSettleAddress, Topics: []string{SigSwapV4, topic32("4"), "0x9"}, Data: "0x", BlockNumber: "0x1", LogIndex: "0x0"},
		// Empty topics (topic0 access guard).
		{Address: "0xab", Topics: []string{}},
	}
	for i, l := range malformed {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("malformed log %d panicked in processLog (must be guarded): %v", i, r)
				}
			}()
			idx.processLog(context.Background(), l)
		}()
	}

	// pollGuarded backstop: a panic raised deep inside a real poll (here, from the
	// genesis-hash probe) must be RECOVERED into an error, never propagate and crash
	// the process. We point the indexer at a live in-process RPC so poll runs through
	// eth_blockNumber and reaches maybeResetForRegenesis, where the probe panics.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Report a head far below the cursor so the re-genesis pre-filter advances to
		// the (panicking) genesis probe.
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	}))
	defer srv.Close()

	pidx := NewWithConfig(Config{RPC: srv.URL, StartBlock: 0}, newMemSQLiteStore(t))
	pidx.lastBlock = 5_000_000
	pidx.genesisHash = "0xbaseline"
	pidx.lowHeadStreak = regenesisConfirmations - 1 // the next poll's probe fires
	pidx.genesisHashFn = func(context.Context) (string, error) { panic("boom from a bad RPC backend") }

	perr := pidx.pollGuarded(context.Background())
	if perr == nil {
		t.Fatal("pollGuarded must return an error when poll panics, not crash the process")
	}
	if !strings.Contains(perr.Error(), "panic recovered") {
		t.Fatalf("pollGuarded error must identify the recovered panic, got %v", perr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MEDIUM: V2/V3 AMM handlers must be scoped to the canonical factory.
//
// handlePairCreated/handlePoolCreated/handleSwapV2/handleSwapV3 fed the SAME
// Factory/TVL/volume aggregates the V4 handlers now gate to 0x9999, but accepted
// events from ANY emitter — a parallel path to seed phantom pairs/pools/swaps. The
// fix gates pair/pool creation to the canonical factory addresses, and Swaps to the
// pools those canonical events created. These tests prove a rogue emitter seeds
// nothing and does not inflate the factory aggregate, while the canonical-source
// event IS honored end-to-end (creation → swap).
// ─────────────────────────────────────────────────────────────────────────────

// Canonical factories used by these tests (distinct from the rogue address). They
// are passed explicitly so the test does not depend on the package defaults.
const (
	testFactoryV2 = "0x00000000000000000000000000000000000000F2"
	testFactoryV3 = "0x00000000000000000000000000000000000000F3"
)

// word20InWord32 right-aligns a 20-byte address into a 32-byte ABI data word —
// the encoding handlePairCreated/handlePoolCreated read the created pair/pool
// address from (the last 20 bytes of the first data word).
func word20InWord32(addr string) string {
	return fmt.Sprintf("%064s", strings.ToLower(strings.TrimPrefix(addr, "0x")))
}

// pairCreatedLog builds a V2 PairCreated whose created-pair address is `pair`.
// topics: [sig, token0, token1]; data word0 = pair (right-aligned), word1 = index.
func pairCreatedLog(emitter, token0, token1, pair string) *logEntry {
	return &logEntry{
		Address:         emitter,
		Topics:          []string{SigPairCreated, addrTopic(token0), addrTopic(token1)},
		Data:            "0x" + word20InWord32(pair) + word(big.NewInt(1)),
		BlockNumber:     "0x1",
		TransactionHash: "0xpair",
		LogIndex:        "0x0",
	}
}

// poolCreatedLog builds a V3 PoolCreated whose created-pool address is `pool`.
// topics: [sig, token0, token1, fee]; data word0 = pool (right-aligned).
func poolCreatedLog(emitter, token0, token1 string, fee int64, pool string) *logEntry {
	return &logEntry{
		Address:         emitter,
		Topics:          []string{SigPoolCreated, addrTopic(token0), addrTopic(token1), topic32(fmt.Sprintf("%x", fee))},
		Data:            "0x" + word20InWord32(pool),
		BlockNumber:     "0x1",
		TransactionHash: "0xpool",
		LogIndex:        "0x0",
	}
}

// swapV2Log builds a V2 Swap emitted by `emitter` (the pair contract).
func swapV2Log(emitter, sender, tx string, amt0In, amt1Out int64) *logEntry {
	return &logEntry{
		Address: emitter,
		Topics:  []string{SigSwapV2, addrTopic(sender)},
		Data: "0x" + word(big.NewInt(amt0In)) + word(new(big.Int)) +
			word(new(big.Int)) + word(big.NewInt(amt1Out)),
		BlockNumber: "0x2", TransactionHash: tx, LogIndex: "0x0",
	}
}

// swapV3Log builds a V3 Swap emitted by `emitter` (the pool contract); legs signed.
func swapV3Log(emitter, sender, tx string, amt0, amt1 int64) *logEntry {
	return &logEntry{
		Address:     emitter,
		Topics:      []string{SigSwapV3, addrTopic(sender), addrTopic(sender)},
		Data:        "0x" + signedWord(big.NewInt(amt0)) + signedWord(big.NewInt(amt1)),
		BlockNumber: "0x2", TransactionHash: tx, LogIndex: "0x0",
	}
}

func TestHandleV2_RejectsNonFactory(t *testing.T) {
	const rogue = "0x000000000000000000000000000000000000dEaD"
	token0 := "0x0000000000000000000000000000000000000011"
	token1 := "0x0000000000000000000000000000000000000022"
	rt := "0x00000000000000000000000000000000000000a1" // rogue pair address
	ct := "0x00000000000000000000000000000000000000b1" // canonical pair address

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused", FactoryV2: testFactoryV2, FactoryV3: testFactoryV3}, s)

	// --- Rogue PairCreated: nothing seeded, factory not inflated. ---
	idx.processLog(context.Background(), pairCreatedLog(rogue, token0, token1, rt))
	if p, _ := s.GetPool(nil, rt); p != nil {
		t.Error("rogue PairCreated must NOT seed a pair")
	}
	if tok, _ := s.GetByType("Token", token0); tok != nil {
		t.Error("rogue PairCreated must NOT seed a token")
	}
	if f, _ := s.GetFactory(nil, "1"); f != nil && asInt64(f.(map[string]interface{})["poolCount"]) != 0 {
		t.Errorf("rogue PairCreated must NOT inflate factory poolCount, got %v", f)
	}

	// --- A Swap from the rogue (never-registered) pair: no swap recorded. ---
	idx.processLog(context.Background(), swapV2Log(rt, token0, "0xspoof", 1000, 2500))
	if sw, _ := s.GetSwaps(nil, 10, "timestamp", "desc", nil); sw != nil && len(sw.([]interface{})) != 0 {
		t.Fatalf("Swap from a rogue V2 pair must NOT be recorded, got %d", len(sw.([]interface{})))
	}

	// --- Canonical PairCreated IS honored; its pair's Swap IS recorded. ---
	idx.processLog(context.Background(), pairCreatedLog(testFactoryV2, token0, token1, ct))
	if p, _ := s.GetPool(nil, ct); p == nil {
		t.Fatal("canonical PairCreated must seed the pair")
	}
	// poolCount is DERIVED by the valuation pass from the store (see
	// TestRevalue_DerivesTVLPricesAndPoolCount); what the create handler owns is
	// the factory tx count, and the registration itself — asserted above.
	if f, _ := s.GetFactory(nil, "1"); f == nil || asInt64(f.(map[string]interface{})["txCount"]) != 1 {
		t.Fatalf("canonical PairCreated must bump factory txCount to 1, got %v", f)
	}
	idx.processLog(context.Background(), swapV2Log(ct, token0, "0xreal", 1000, 2500))
	sw, _ := s.GetSwaps(nil, 10, "timestamp", "desc", nil)
	if sw == nil || len(sw.([]interface{})) != 1 {
		t.Fatalf("Swap from the canonical V2 pair must be recorded exactly once, got %v", sw)
	}
}

// A chain whose factories were never declared must index no AMM at all. The
// failure this pins down is not a crash: it is a Zoo indexer that quietly
// adopted Lux's factory address and served Lux's pools as Zoo's.
func TestUndeclaredFactory_IndexesNothing(t *testing.T) {
	token0 := "0x0000000000000000000000000000000000000011"
	token1 := "0x0000000000000000000000000000000000000022"
	pool := "0x00000000000000000000000000000000000000c3"

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused"}, s)

	// The address a previous build reached for when none was configured.
	const otherChainFactory = "0x80bBc7C4C7a59C899D1B37BC14539A22D5830a84"
	idx.processLog(context.Background(), poolCreatedLog(otherChainFactory, token0, token1, 3000, pool))
	if p, _ := s.GetPool(nil, pool); p != nil {
		t.Fatal("an undeclared chain must not adopt another chain's factory")
	}

	idx.processLog(context.Background(), swapV3Log(pool, token0, "0xspoof", 1000, -2500))
	if sw, _ := s.GetSwaps(nil, 10, "timestamp", "desc", nil); sw != nil && len(sw.([]interface{})) != 0 {
		t.Fatalf("a swap from an unregistered pool must not be recorded, got %d", len(sw.([]interface{})))
	}
}

func TestHandleV3_RejectsNonFactory(t *testing.T) {
	const rogue = "0x000000000000000000000000000000000000dEaD"
	token0 := "0x0000000000000000000000000000000000000011"
	token1 := "0x0000000000000000000000000000000000000022"
	rt := "0x00000000000000000000000000000000000000a3" // rogue pool address
	ct := "0x00000000000000000000000000000000000000b3" // canonical pool address

	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused", FactoryV2: testFactoryV2, FactoryV3: testFactoryV3}, s)

	// --- Rogue PoolCreated: nothing seeded, factory not inflated. ---
	idx.processLog(context.Background(), poolCreatedLog(rogue, token0, token1, 3000, rt))
	if p, _ := s.GetPool(nil, rt); p != nil {
		t.Error("rogue PoolCreated must NOT seed a pool")
	}
	if f, _ := s.GetFactory(nil, "1"); f != nil && asInt64(f.(map[string]interface{})["poolCount"]) != 0 {
		t.Errorf("rogue PoolCreated must NOT inflate factory poolCount, got %v", f)
	}

	// --- A Swap from the rogue (never-registered) pool: no swap recorded. ---
	idx.processLog(context.Background(), swapV3Log(rt, token0, "0xspoof", 1000, -2500))
	if sw, _ := s.GetSwaps(nil, 10, "timestamp", "desc", nil); sw != nil && len(sw.([]interface{})) != 0 {
		t.Fatalf("Swap from a rogue V3 pool must NOT be recorded, got %d", len(sw.([]interface{})))
	}

	// --- Canonical PoolCreated IS honored; its pool's Swap IS recorded with signed legs. ---
	idx.processLog(context.Background(), poolCreatedLog(testFactoryV3, token0, token1, 3000, ct))
	if p, _ := s.GetPool(nil, ct); p == nil {
		t.Fatal("canonical PoolCreated must seed the pool")
	}
	if f, _ := s.GetFactory(nil, "1"); f == nil || asInt64(f.(map[string]interface{})["txCount"]) != 1 {
		t.Fatalf("canonical PoolCreated must bump factory txCount to 1, got %v", f)
	}
	idx.processLog(context.Background(), swapV3Log(ct, token0, "0xreal", 1000, 2500))
	sw, _ := s.GetSwaps(nil, 10, "timestamp", "desc", nil)
	if sw == nil || len(sw.([]interface{})) != 1 {
		t.Fatalf("Swap from the canonical V3 pool must be recorded exactly once, got %v", sw)
	}
}
