package indexer

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/luxfi/graph/storage"
)

// ERC20 metadata selectors (first 4 bytes of keccak256 of the signature).
//
//	symbol()   => 0x95d89b41
//	name()     => 0x06fdde03
//	decimals() => 0x313ce567
//
// AMM Token entities are token *contracts* (an ERC20 underlying, or the
// pair/LP token itself, which is also an ERC20 — e.g. "UNI-V2"). The pool
// creation logs only carry the address; the human symbol/name/decimals live
// on the contract and must be read with eth_call. (This is distinct from the
// no-eth_getCode re-genesis rule, which forbids eth_getCode of the *cursor*
// block for chain-identity probing — an eth_call of a token contract at head
// is a normal, cheap read and is cached one-per-token below.)
const (
	selSymbol   = "0x95d89b41"
	selName     = "0x06fdde03"
	selDecimals = "0x313ce567"
)

// defaultDecimals is the ERC20 default when decimals() is absent or reverts.
const defaultDecimals = 18

// tokenMeta resolves (and memoises) the ERC20 metadata for a token address.
// It is the single source of truth for symbol/name/decimals enrichment: every
// pool/pair/transfer handler routes its token writes through seedToken, which
// calls this once per address for the lifetime of the process.
//
// Caching is mandatory, not an optimisation: Transfer fires on every transfer
// and PairCreated/PoolCreated re-reference the same tokens, so an un-cached
// read would issue three eth_calls per token *per log* — a per-block RPC storm
// against the chain. The cache collapses that to three eth_calls the first
// time a token is seen and zero thereafter; reverting/non-compliant tokens are
// cached as a placeholder so they are likewise probed only once.
func (idx *Indexer) tokenMeta(ctx context.Context, addr string) *storage.SeedTokenData {
	key := strings.ToLower(addr)
	if v, ok := idx.tokenCache.Load(key); ok {
		return v.(*storage.SeedTokenData)
	}

	meta := idx.readERC20(ctx, addr)
	// LoadOrStore so two goroutines racing the same fresh token converge on one
	// cached value (handlers run on a single poll goroutine today, but the cache
	// must not depend on that).
	actual, _ := idx.tokenCache.LoadOrStore(key, meta)
	return actual.(*storage.SeedTokenData)
}

// readERC20 performs the three metadata eth_calls and decodes them, applying
// graceful fallbacks so a non-compliant or reverting token still yields a sane
// Token entity (placeholder symbol/name = address, decimals = 18) rather than
// dropping the token or failing the poll.
func (idx *Indexer) readERC20(ctx context.Context, addr string) *storage.SeedTokenData {
	out := &storage.SeedTokenData{
		Symbol:   shortAddr(addr), // fallbacks, overwritten on a successful read
		Name:     addr,
		Decimals: defaultDecimals,
	}

	if raw, err := idx.erc20Call(ctx, addr, selSymbol); err == nil {
		if s := decodeERC20String(raw); s != "" {
			out.Symbol = s
		}
	}
	if raw, err := idx.erc20Call(ctx, addr, selName); err == nil {
		if s := decodeERC20String(raw); s != "" {
			out.Name = s
		}
	}
	if raw, err := idx.erc20Call(ctx, addr, selDecimals); err == nil {
		if d, ok := decodeERC20Decimals(raw); ok {
			out.Decimals = d
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
