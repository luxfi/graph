package indexer

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/luxfi/graph/storage"
)

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
	idx.processLog(l)

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
	idx.processLog(mkFill(LXSettleAddress, "0xaa", 400))
	// Spoofed fill from a rogue address — must be dropped.
	idx.processLog(mkFill("0x0000000000000000000000000000000000009998", "0xbb", 999999))

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

// Initialize at 0x9999 creates an AMM pool, bumps the factory aggregate, and
// creates a DEX Market.
func TestHandleInitializeV4_PoolMarketFactory(t *testing.T) {
	s := newMemSQLiteStore(t)
	idx := NewWithConfig(Config{RPC: "http://unused"}, s)

	poolID := topic32("d00d")
	cur0 := "0x0000000000000000000000000000000000000011"
	cur1 := "0x0000000000000000000000000000000000000022"
	l := &logEntry{
		Address:     LXSettleAddress,
		Topics:      []string{SigInitializeV4, poolID, addrTopic(cur0), addrTopic(cur1)},
		Data:        "0x" + word(big.NewInt(3000)) + word(big.NewInt(60)) + word(big.NewInt(0)) + word(big.NewInt(0)) + word(big.NewInt(0)),
		BlockNumber: "0x1",
	}
	idx.processLog(l)

	if p, _ := s.GetPool(nil, poolID); p == nil {
		t.Fatal("expected pool from InitializeV4")
	}
	f, _ := s.GetFactory(nil, "1")
	if f == nil || asInt64(f.(map[string]interface{})["poolCount"]) != 1 {
		t.Fatalf("expected factory poolCount=1, got %v", f)
	}
	if mk, _ := s.GetByType("Market", poolID); mk == nil {
		t.Fatal("expected DEX Market from InitializeV4 at 0x9999")
	}
}

// The re-genesis self-heal: a persisted cursor far above a freshly-relaunched
// chain's head must rewind to StartBlock so the new chain indexes from genesis.
func TestPoll_RegenesisSelfHeal(t *testing.T) {
	s := newMemSQLiteStore(t)
	s.SetLastBlock(5_000_000) // stale cursor from the previous chain
	idx := NewWithConfig(Config{RPC: "http://unused", StartBlock: 0}, s)
	if idx.lastBlock != 5_000_000 {
		t.Fatalf("precondition: lastBlock=%d", idx.lastBlock)
	}

	// Simulate the head as observed on a fresh chain (head=3, far below cursor).
	idx.maybeResetForRegenesis(3)
	if idx.lastBlock != 0 {
		t.Fatalf("expected rewind to StartBlock 0, got %d", idx.lastBlock)
	}
	if s.GetLastBlock() != 0 {
		t.Fatalf("expected persisted cursor reset to 0, got %d", s.GetLastBlock())
	}
}

// A shallow reorg (head dips a few blocks) must NOT trigger a reset.
func TestPoll_ShallowReorgNoReset(t *testing.T) {
	s := newMemSQLiteStore(t)
	s.SetLastBlock(1000)
	idx := NewWithConfig(Config{RPC: "http://unused", StartBlock: 0}, s)

	idx.maybeResetForRegenesis(995) // 5 blocks back — within reorgDepth
	if idx.lastBlock != 1000 {
		t.Fatalf("shallow reorg must not reset; lastBlock=%d", idx.lastBlock)
	}
}
