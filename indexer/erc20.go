package indexer

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/luxfi/graph/storage"
)

// ERC20 metadata selectors (first 4 bytes of keccak256 of the signature).
//
//	symbol()   => 0x95d89b41
//	name()     => 0x06fdde03
//	decimals() => 0x313ce567
//	totalSupply() => 0x18160ddd
//
// AMM Token entities are token *contracts* (an ERC20 underlying, or the
// pair/LP token itself, which is also an ERC20 — e.g. "UNI-V2"). The pool
// creation logs only carry the address; the human symbol/name/decimals live
// on the contract and must be read with eth_call. (This is distinct from the
// no-eth_getCode re-genesis rule, which forbids eth_getCode of the *cursor*
// block for chain-identity probing — an eth_call of a token contract at head
// is a normal, cheap read and is cached one-per-token below.)
const (
	selSymbol      = "0x95d89b41"
	selName        = "0x06fdde03"
	selDecimals    = "0x313ce567"
	selTotalSupply = "0x18160ddd"
)

// defaultDecimals is the ERC20 default when decimals() is absent or reverts.
const defaultDecimals = 18

// unread marks a decimals field the contract has not answered, so a read that
// said nothing is distinguishable from one that said zero. It never leaves this
// file: resolve turns it into the ERC20 default.
const unread = -1

// token answers what a token is, and records the answer. It is the ONE place
// every pool/pair/transfer handler routes token identity through, so those
// handlers stay in their lane and no other code decides what a token is called
// or what its supply is.
//
// The store is asked first. Symbol, name and decimals are immutable, so a row
// that already carries them — and a supply — has nothing left to ask the chain.
//
// The chain is asked at most once per address for the life of the process.
// Transfer fires on every transfer and the pool handlers re-reference the same
// pair every block, so an un-memoised read would be four eth_calls per token per
// LOG — a per-block RPC storm. A contract that reverts is memoised too, so it is
// probed once and not again.
func (idx *Indexer) token(ctx context.Context, addr string) *storage.SeedTokenData {
	key := strings.ToLower(addr)
	if v, ok := idx.tokenCache.Load(key); ok {
		return v.(*storage.SeedTokenData)
	}
	stored := idx.storedToken(addr)
	if settled(stored, addr) {
		v, _ := idx.tokenCache.LoadOrStore(key, stored)
		return v.(*storage.SeedTokenData)
	}
	t := resolve(stored, idx.readERC20(ctx, addr), addr)
	// LoadOrStore so two goroutines racing the same fresh token converge on one
	// value, and only the winner writes it (handlers run on a single poll
	// goroutine today, but the cache must not depend on that).
	v, loaded := idx.tokenCache.LoadOrStore(key, t)
	if !loaded {
		idx.store.SeedToken(addr, t)
	}
	return v.(*storage.SeedTokenData)
}

// settled reports whether a stored row still has something to ask the chain for.
// A row is settled once it carries a real symbol AND a supply: those are the two
// figures only the contract can give, and a token page prints a dash where the
// market cap belongs without the second one.
//
// Keying this on the symbol alone is what left every already-indexed token
// supply-less: the row had a name, so nothing ever asked the contract again.
func settled(t *storage.SeedTokenData, addr string) bool {
	return t != nil && !isPlaceholderSymbol(t.Symbol, addr) && t.TotalSupply != ""
}

// resolve folds a fresh read over what the store already holds, then fills what
// neither could supply with the ERC20 defaults.
//
// A read that says nothing must never erase something already known: symbol()
// reverts on an RPC blip, and the address placeholder that follows would replace
// a real symbol on the next restart. It carries the aggregates through for the
// same reason — value locked, volume, price and trade count are accumulated onto
// this row by the valuation pass, and a write built from the contract read alone
// would zero every one of them.
func resolve(stored, read *storage.SeedTokenData, addr string) *storage.SeedTokenData {
	out := &storage.SeedTokenData{Decimals: unread}
	if stored != nil {
		c := *stored
		out = &c
	}
	if read != nil {
		if read.Symbol != "" {
			out.Symbol = read.Symbol
		}
		if read.Name != "" {
			out.Name = read.Name
		}
		if read.Decimals != unread {
			out.Decimals = read.Decimals
		}
		if read.TotalSupply != "" {
			out.TotalSupply = read.TotalSupply
		}
	}
	if out.Symbol == "" {
		out.Symbol = shortAddr(addr)
	}
	if out.Name == "" {
		out.Name = addr
	}
	if out.Decimals == unread {
		out.Decimals = defaultDecimals
	}
	return out
}

// BackfillTokens asks the chain for what stored Token rows are still missing: a
// symbol an earlier read never got, a supply a build that never asked for one
// left empty. It is the cheap, idempotent alternative to a re-sync — a settled
// row costs nothing, an unsettled one costs four eth_calls, and no block is
// re-scanned, no cursor rewound, no aggregate touched.
//
// Reports how many rows the pass settled. Honors ctx cancellation between rows.
func (idx *Indexer) BackfillTokens(ctx context.Context) (int, error) {
	const pageSize = 1 << 20 // GetTokens caps internally; this asks for "all"
	raw, err := idx.store.GetTokens(ctx, pageSize, "", "", nil)
	if err != nil {
		return 0, err
	}
	rows, ok := raw.([]interface{})
	if !ok {
		return 0, nil
	}
	filled := 0
	for _, r := range rows {
		if ctx.Err() != nil {
			return filled, ctx.Err()
		}
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		addr, _ := m["id"].(string)
		if addr == "" || settled(idx.storedToken(addr), addr) {
			continue
		}
		// Count only rows that actually changed: the read may have reverted, and
		// that token is now memoised and will not be retried this run.
		if settled(idx.token(ctx, addr), addr) {
			filled++
		}
	}
	idx.logf("[indexer] token backfill — %d/%d settled", filled, len(rows))
	return filled, nil
}

// storedToken loads a token's persisted row, or nil if absent. It carries every
// field, aggregates included: the row is read back to be written again, and a
// field dropped here is a field erased on disk.
func (idx *Indexer) storedToken(addr string) *storage.SeedTokenData {
	t, err := idx.store.GetToken(nil, addr)
	if err != nil || t == nil {
		return nil
	}
	m, ok := t.(map[string]interface{})
	if !ok {
		return nil
	}
	return &storage.SeedTokenData{
		Symbol:              asString(m["symbol"]),
		Name:                asString(m["name"]),
		Decimals:            asInt64(m["decimals"]),
		TotalSupply:         asString(m["totalSupply"]),
		Staked:              asString(m["staked"]),
		VolumeUSD:           asString(m["volumeUSD"]),
		TotalValueLockedUSD: asString(m["totalValueLockedUSD"]),
		DerivedETH:          asString(m["derivedETH"]),
		TxCount:             asInt64(m["txCount"]),
	}
}

// isPlaceholderSymbol reports whether a stored symbol is the address-derived
// fallback (shortAddr) rather than a real ERC20 symbol.
func isPlaceholderSymbol(symbol, addr string) bool {
	return symbol == "" || strings.EqualFold(symbol, shortAddr(addr))
}

// shortAddr returns a stable short symbol for an unknown token address. It is
// the fallback when the chain has no ERC20 symbol()/name() to offer (a revert, a
// non-compliant contract, or an unreachable RPC).
func shortAddr(addr string) string {
	if len(addr) >= 8 {
		return addr[:8]
	}
	return addr
}

// readERC20 asks a contract what it is: four eth_calls, decoded. Fields the
// contract did not answer are left empty (decimals: unread) so silence is
// distinguishable from a real value and resolve can keep what is already known.
//
// Decimals is read before supply because it is the scale supply is expressed in.
func (idx *Indexer) readERC20(ctx context.Context, addr string) *storage.SeedTokenData {
	out := &storage.SeedTokenData{Decimals: unread}

	if raw, err := idx.erc20Call(ctx, addr, selSymbol); err == nil {
		out.Symbol = decodeERC20String(raw)
	}
	if raw, err := idx.erc20Call(ctx, addr, selName); err == nil {
		out.Name = decodeERC20String(raw)
	}
	if raw, err := idx.erc20Call(ctx, addr, selDecimals); err == nil {
		if d, ok := decodeERC20Decimals(raw); ok {
			out.Decimals = d
		}
	}
	// Supply is what turns a price into a valuation. Without it a token page
	// prints a dash where market cap and fully diluted value belong, however much
	// the asset trades. It is read at head, so it follows a mint or a burn.
	if raw, err := idx.erc20Call(ctx, addr, selTotalSupply); err == nil {
		if n, ok := decodeERC20Uint(raw); ok {
			scale := out.Decimals
			if scale == unread {
				scale = defaultDecimals
			}
			out.TotalSupply = wholeUnits(n, scale)
		}
	}
	return out
}

// erc20Call issues `eth_call {to: addr, data: selector}` at the latest block
// and returns the 0x-prefixed hex result. Reuses the indexer's existing
// JSON-RPC client and endpoint — the same transport eth_getLogs rides on.
func (idx *Indexer) erc20Call(ctx context.Context, addr, selector string) (string, error) {
	raw, err := idx.rpcCall(ctx, "eth_call", []interface{}{
		map[string]string{"to": addr, "data": selector},
		"latest",
	})
	if err != nil {
		return "", err
	}
	var hex string
	if err := json.Unmarshal(raw, &hex); err != nil {
		return "", err
	}
	return hex, nil
}

// decodeERC20String decodes an ERC20 string return value. It accepts BOTH
// shapes seen in the wild:
//
//   - Canonical ABI `string`: word0 = data offset (0x20), word1 = byte length,
//     then the UTF-8 bytes right-padded to a 32-byte boundary.
//   - Legacy `bytes32` (e.g. MKR, original DAI): a single 32-byte word holding
//     the symbol/name as right-padded NUL bytes, with NO offset/length header.
//
// Returns "" when the value is empty, all-zero, or malformed so the caller
// keeps its address placeholder.
func decodeERC20String(hexStr string) string {
	b := hexToBytes(hexStr)
	if len(b) == 0 {
		return ""
	}

	// Canonical ABI string: at least offset(32) + length(32); offset must point
	// at 0x20 and the declared length must fit in the payload.
	if len(b) >= 64 {
		offset := beUint(b[:32])
		if offset == 32 && len(b) >= 64 {
			length := beUint(b[32:64])
			if length > 0 && 64+int(length) <= len(b) {
				return sanitizeUTF8(string(b[64 : 64+int(length)]))
			}
			// length == 0 is a legitimately empty ABI string.
			if length == 0 {
				return ""
			}
		}
	}

	// bytes32 fallback: exactly one word, interpreted as right-padded NUL bytes.
	if len(b) == 32 {
		return sanitizeUTF8(strings.TrimRight(string(b), "\x00"))
	}

	return ""
}

// decodeERC20Decimals reads a uint8 decimals() return (a 32-byte word). Returns
// ok=false on an empty/short result so the caller keeps the ERC20 default (18).
func decodeERC20Decimals(hexStr string) (int64, bool) {
	b := hexToBytes(hexStr)
	if len(b) < 32 {
		return 0, false
	}
	// decimals is a uint8; read the low byte of the first word. Guard against an
	// absurd value (a non-compliant contract returning garbage) by rejecting
	// anything that does not fit a uint8 — the upper 31 bytes must be zero.
	for _, x := range b[:31] {
		if x != 0 {
			return 0, false
		}
	}
	return int64(b[31]), true
}

// decodeERC20Uint decodes a uint256 return value. A supply reaches 2^256-1,
// which no float or int64 holds, so it stays a big.Int all the way to the text
// the store keeps.
func decodeERC20Uint(hexStr string) (*big.Int, bool) {
	b := hexToBytes(hexStr)
	if len(b) < 32 {
		return nil, false
	}
	return new(big.Int).SetBytes(b[:32]), true
}

// wholeUnits converts a base-unit amount to whole tokens, exactly.
//
// One unit for one field: a supply means whole tokens everywhere it is read, so
// a price multiplies it directly. A contract reports base units and the native
// coin's genesis figure is already whole, and the conversion belongs here rather
// than in whoever displays it — a client scaling one and not the other is how a
// valuation lands 10^18 out.
//
// Integer division, not float: float64 runs out of mantissa around 2^53 and a
// two-trillion-token supply is far past it. The remainder keeps every digit the
// contract gave.
func wholeUnits(n *big.Int, decimals int64) string {
	if n == nil {
		return ""
	}
	if decimals <= 0 {
		return n.String()
	}
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(decimals), nil)
	q, r := new(big.Int).QuoRem(n, pow, new(big.Int))
	if r.Sign() == 0 {
		return q.String()
	}
	frac := r.String()
	frac = strings.Repeat("0", int(decimals)-len(frac)) + frac
	return q.String() + "." + strings.TrimRight(frac, "0")
}

// --- byte helpers (pure, table-tested) ---

// hexToBytes decodes a 0x-prefixed hex string, tolerating odd length and
// non-hex input by returning nil (callers treat nil as "no data").
func hexToBytes(s string) []byte {
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	if len(s) == 0 || len(s)%2 != 0 {
		return nil
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexNibble(s[i*2])
		lo, ok2 := hexNibble(s[i*2+1])
		if !ok1 || !ok2 {
			return nil
		}
		out[i] = hi<<4 | lo
	}
	return out
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// beUint reads a big-endian unsigned integer from up to 8 trailing bytes of a
// word. ABI offsets/lengths far exceed 2^64 only in adversarial input, which
// the length-fits-payload check above rejects anyway.
func beUint(word []byte) int {
	var n uint64
	start := 0
	if len(word) > 8 {
		start = len(word) - 8 // keep the low 8 bytes; high bytes are zero for sane lengths
	}
	for _, x := range word[start:] {
		n = n<<8 | uint64(x)
	}
	return int(n)
}

// sanitizeUTF8 trims surrounding whitespace/NULs and drops any embedded NULs so
// a malformed bytes32 padding never leaks control bytes into the GraphQL JSON.
func sanitizeUTF8(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}
