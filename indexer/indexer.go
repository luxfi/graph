// Package indexer subscribes to EVM events and writes to storage.
//
// Connects to any EVM JSON-RPC, polls for new blocks, decodes logs,
// and writes structured data (swaps, mints, burns, transfers) to storage.
package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/luxfi/graph/storage"
)

// Status reports indexer progress.
type Status struct {
	LatestBlock   uint64 `json:"latestBlock"`
	IndexedEvents uint64 `json:"indexedEvents"`
}

// Config tunes an Indexer. The zero value is valid: PoolManager defaults to the
// canonical 0x9999 settlement address and StartBlock to 0 (index from genesis).
type Config struct {
	RPC string
	// PoolManager is the DEX settlement precompile address (0x9999). Logs from
	// this address are additionally derived into the DEX (CLOB) graph schema as
	// Fill/Market entities. Lower-cased on construction for case-insensitive
	// compare against eth_getLogs `address` fields.
	PoolManager string
	// StartBlock is the genesis-relative block to (re)index from. On a clean
	// chain relaunch the indexer rewinds to this value (see poll's reorg guard).
	StartBlock uint64
}

// reorgDepth is how far the chain head may legitimately move backwards (deep
// reorg) before we treat a lower head as a chain RESET (re-genesis) rather than
// a reorg. A re-genesised chain reports a head far below our persisted cursor;
// a normal reorg is shallow. This avoids wiping progress on a transient blip
// while still self-healing across a clean relaunch.
const reorgDepth = 128

// Indexer watches an EVM RPC and writes events to storage.
type Indexer struct {
	rpc         string
	poolManager string // lower-cased 0x9999 settlement address
	startBlock  uint64
	store       *storage.Store
	client      *http.Client

	lastBlock uint64
	status    Status
}

// New creates an indexer connected to the given RPC endpoint with default
// settlement address and a genesis (block 0) start.
func New(rpc string, store *storage.Store) *Indexer {
	return NewWithConfig(Config{RPC: rpc}, store)
}

// NewWithConfig creates an indexer from an explicit Config.
func NewWithConfig(cfg Config, store *storage.Store) *Indexer {
	pm := cfg.PoolManager
	if pm == "" {
		pm = LXSettleAddress
	}
	idx := &Indexer{
		rpc:         cfg.RPC,
		poolManager: strings.ToLower(pm),
		startBlock:  cfg.StartBlock,
		store:       store,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
	idx.lastBlock = idx.store.GetLastBlock()
	if idx.lastBlock < cfg.StartBlock {
		idx.lastBlock = cfg.StartBlock
	}
	return idx
}

// Status returns current indexer progress.
func (idx *Indexer) Status() Status {
	return idx.status
}

// maybeResetForRegenesis rewinds the cursor to StartBlock when the observed
// chain head is far enough below our persisted cursor to indicate a clean
// relaunch (fresh genesis) rather than a shallow reorg. A re-genesised chain
// reports a head near zero while our store still holds the old chain's height;
// without this rewind, `latest <= lastBlock` would be true forever and the new
// chain would never be indexed. A normal reorg is shallow (< reorgDepth) and is
// ignored here so transient head dips don't wipe progress.
func (idx *Indexer) maybeResetForRegenesis(latest uint64) {
	if idx.lastBlock > idx.startBlock+reorgDepth && latest+reorgDepth < idx.lastBlock {
		log.Printf("[indexer] head %d << cursor %d (>%d behind) — chain reset detected, rewinding to %d",
			latest, idx.lastBlock, reorgDepth, idx.startBlock)
		idx.lastBlock = idx.startBlock
		idx.store.SetLastBlock(idx.startBlock)
	}
}

// Run starts the indexer loop. Blocks until ctx is cancelled.
func (idx *Indexer) Run(ctx context.Context) error {
	log.Printf("[indexer] starting — rpc=%s lastBlock=%d", idx.rpc, idx.lastBlock)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := idx.poll(ctx); err != nil {
				log.Printf("[indexer] poll error: %v", err)
			}
		}
	}
}

// rpcCall makes a JSON-RPC POST and returns the result field.
func (idx *Indexer) rpcCall(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	type rpcReq struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params"`
		ID      int         `json:"id"`
	}
	type rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	body, err := json.Marshal(rpcReq{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", idx.rpc, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := idx.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}

	var rr rpcResp
	if err := json.Unmarshal(respBody, &rr); err != nil {
		return nil, fmt.Errorf("rpc decode: %w", err)
	}
	if rr.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rr.Error.Code, rr.Error.Message)
	}
	return rr.Result, nil
}

// parseHexUint64 parses a 0x-prefixed hex string to uint64.
func parseHexUint64(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	return strconv.ParseUint(s, 16, 64)
}

// knownTopics returns all event topic0s we want to filter for.
func knownTopics() []string {
	topics := []string{
		// AMM (V2/V3/V4)
		SigPairCreated, SigSwapV2, SigMintV2, SigBurnV2, SigSync, SigTransfer,
		SigPoolCreated, SigInitialize, SigSwapV3, SigMintV3, SigBurnV3,
		SigCollect, SigFlash,
		SigInitializeV4, SigModifyLiquidity, SigSwapV4,
		// Native DEX (CLOB) settlement at 0x9999.
		SigDEXFill,
	}
	// Securities — full ERC-3643 + ONCHAINID surface.
	return append(topics, SecuritiesTopics()...)
}

// logEntry is a decoded eth_getLogs result entry.
type logEntry struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	LogIndex         string   `json:"logIndex"`
	TransactionIndex string   `json:"transactionIndex"`
}

func (idx *Indexer) poll(ctx context.Context) error {
	// 1. Get latest block number
	raw, err := idx.rpcCall(ctx, "eth_blockNumber", []interface{}{})
	if err != nil {
		return fmt.Errorf("eth_blockNumber: %w", err)
	}

	var hexBlock string
	if err := json.Unmarshal(raw, &hexBlock); err != nil {
		return fmt.Errorf("parse blockNumber: %w", err)
	}
	latest, err := parseHexUint64(hexBlock)
	if err != nil {
		return fmt.Errorf("parse hex block: %w", err)
	}

	// Re-genesis self-heal (see maybeResetForRegenesis).
	idx.maybeResetForRegenesis(latest)

	// Nothing new
	if latest <= idx.lastBlock {
		return nil
	}

	fromBlock := idx.lastBlock + 1
	toBlock := latest
	// Cap batch size to 2000 blocks
	if toBlock-fromBlock > 2000 {
		toBlock = fromBlock + 2000
	}

	// 2. Get logs for known event signatures
	filter := map[string]interface{}{
		"fromBlock": fmt.Sprintf("0x%x", fromBlock),
		"toBlock":   fmt.Sprintf("0x%x", toBlock),
		"topics":    []interface{}{knownTopics()},
	}

	raw, err = idx.rpcCall(ctx, "eth_getLogs", []interface{}{filter})
	if err != nil {
		return fmt.Errorf("eth_getLogs: %w", err)
	}

	var logs []logEntry
	if err := json.Unmarshal(raw, &logs); err != nil {
		return fmt.Errorf("parse logs: %w", err)
	}

	// 3. Process each log
	for i := range logs {
		idx.processLog(&logs[i])
		idx.status.IndexedEvents++
	}

	// 4. Update progress
	idx.lastBlock = toBlock
	idx.status.LatestBlock = toBlock
	idx.store.SetLastBlock(toBlock)

	if len(logs) > 0 {
		log.Printf("[indexer] blocks %d..%d — %d events", fromBlock, toBlock, len(logs))
	}
	return nil
}

// processLog matches a log entry's topic0 and writes to storage.
func (idx *Indexer) processLog(l *logEntry) {
	if len(l.Topics) == 0 {
		return
	}
	topic0 := l.Topics[0]
	blockNum, _ := parseHexUint64(l.BlockNumber)
	txHash := l.TransactionHash
	logIdx := l.LogIndex

	switch topic0 {
	case SigSwapV2:
		idx.handleSwapV2(l, blockNum, txHash, logIdx)
	case SigSwapV3:
		idx.handleSwapV3(l, blockNum, txHash, logIdx)
	case SigPairCreated:
		idx.handlePairCreated(l)
	case SigPoolCreated:
		idx.handlePoolCreated(l)
	case SigTransfer:
		idx.handleTransfer(l, txHash, logIdx)
	case SigInitializeV4:
		idx.handleInitializeV4(l)
	case SigSwapV4:
		idx.handleSwapV4(l, blockNum, txHash, logIdx)
	case SigDEXFill:
		idx.handleDEXFill(l, blockNum, txHash, logIdx)
	case SigMintV2, SigMintV3, SigBurnV2, SigBurnV3, SigSync,
		SigCollect, SigFlash, SigInitialize, SigModifyLiquidity:
		// Recognized but storage for these types not yet wired

	// ── ERC-3643 IToken ───────────────────────────────────────────────
	case SigAddressFrozen:
		idx.handleAddressFrozen(l, blockNum, txHash, logIdx)
	case SigTokensFrozen:
		idx.handleTokensFrozen(l, blockNum, txHash, logIdx, true)
	case SigTokensUnfrozen:
		idx.handleTokensFrozen(l, blockNum, txHash, logIdx, false)
	case SigSecurityPaused:
		idx.handleSecurityPause(l, blockNum, txHash, logIdx, true)
	case SigSecurityUnpaused:
		idx.handleSecurityPause(l, blockNum, txHash, logIdx, false)
	case SigRecoverySuccess:
		idx.handleRecoverySuccess(l, blockNum, txHash, logIdx)
	case SigUpdatedTokenInformation:
		idx.handleSimpleSecuritiesEvent(l, blockNum, txHash, logIdx, "UpdatedTokenInformation")
	case SigIdentityRegistryAdded:
		idx.handleSimpleSecuritiesEvent(l, blockNum, txHash, logIdx, "IdentityRegistryAdded")
	case SigComplianceAdded:
		idx.handleSimpleSecuritiesEvent(l, blockNum, txHash, logIdx, "ComplianceAdded")

	// ── IdentityRegistry / Storage ────────────────────────────────────
	case SigIdentityRegistered:
		idx.handleIdentityRegistryAction(l, blockNum, txHash, logIdx, "IdentityRegistered")
	case SigIdentityRemoved:
		idx.handleIdentityRegistryAction(l, blockNum, txHash, logIdx, "IdentityRemoved")
	case SigIdentityUpdated:
		idx.handleIdentityRegistryAction(l, blockNum, txHash, logIdx, "IdentityUpdated")
	case SigCountryUpdated:
		idx.handleIdentityRegistryAction(l, blockNum, txHash, logIdx, "CountryUpdated")
	case SigIdentityStored:
		idx.handleIdentityRegistryAction(l, blockNum, txHash, logIdx, "IdentityStored")

	// ── ONCHAINID (ERC-734 / ERC-735) ─────────────────────────────────
	case SigClaimAdded:
		idx.handleClaim(l, blockNum, txHash, logIdx, "ClaimAdded")
	case SigClaimRemoved:
		idx.handleClaim(l, blockNum, txHash, logIdx, "ClaimRemoved")
	case SigClaimChanged:
		idx.handleClaim(l, blockNum, txHash, logIdx, "ClaimChanged")
	case SigKeyAdded:
		idx.handleKey(l, blockNum, txHash, logIdx, "KeyAdded")
	case SigKeyRemoved:
		idx.handleKey(l, blockNum, txHash, logIdx, "KeyRemoved")
	case SigOnchainIdApproved:
		idx.handleSimpleSecuritiesEvent(l, blockNum, txHash, logIdx, "OnchainIdApproved")
	case SigOnchainIdExecuted:
		idx.handleSimpleSecuritiesEvent(l, blockNum, txHash, logIdx, "OnchainIdExecuted")

	// ── TrustedIssuersRegistry / ClaimTopicsRegistry ──────────────────
	case SigTrustedIssuerAdded:
		idx.handleTrustedIssuer(l, blockNum, txHash, logIdx, "TrustedIssuerAdded")
	case SigTrustedIssuerRemoved:
		idx.handleTrustedIssuer(l, blockNum, txHash, logIdx, "TrustedIssuerRemoved")
	case SigClaimTopicsUpdated:
		idx.handleTrustedIssuer(l, blockNum, txHash, logIdx, "ClaimTopicsUpdated")
	case SigClaimTopicAdded:
		idx.handleClaimTopic(l, blockNum, txHash, logIdx, "ClaimTopicAdded")
	case SigClaimTopicRemoved:
		idx.handleClaimTopic(l, blockNum, txHash, logIdx, "ClaimTopicRemoved")

	// ── ModularCompliance / IModule ───────────────────────────────────
	case SigModuleAdded, SigModuleRemoved, SigTokenBound, SigTokenUnbound, SigModuleInteraction:
		idx.handleComplianceEvent(l, blockNum, txHash, logIdx, topic0)
	}
}

// decodeUint256 reads a 32-byte hex word from data at the given word index.
func decodeUint256(data string, wordIndex int) *big.Int {
	data = strings.TrimPrefix(data, "0x")
	start := wordIndex * 64
	if start+64 > len(data) {
		return new(big.Int)
	}
	n := new(big.Int)
	n.SetString(data[start:start+64], 16)
	return n
}

// topicAddr extracts an address from a topic (last 40 hex chars of 66-char topic).
func topicAddr(topic string) string {
	if len(topic) >= 42 {
		return "0x" + topic[len(topic)-40:]
	}
	return topic
}

func (idx *Indexer) handleSwapV2(l *logEntry, blockNum uint64, txHash, logIdx string) {
	id := fmt.Sprintf("%s#%s", txHash, logIdx)
	sender := ""
	if len(l.Topics) > 1 {
		sender = topicAddr(l.Topics[1])
	}
	amount0In := decodeUint256(l.Data, 0)
	amount1In := decodeUint256(l.Data, 1)
	amount0Out := decodeUint256(l.Data, 2)
	amount1Out := decodeUint256(l.Data, 3)

	// Net amounts
	amount0 := new(big.Int).Sub(amount0In, amount0Out)
	amount1 := new(big.Int).Sub(amount1In, amount1Out)

	idx.store.SeedSwap(id, &storage.SeedSwapData{
		Timestamp: int64(blockNum),
		Pool:      l.Address,
		Amount0:   amount0.String(),
		Amount1:   amount1.String(),
		AmountUSD: "0",
		Sender:    sender,
	})
}

func (idx *Indexer) handleSwapV3(l *logEntry, blockNum uint64, txHash, logIdx string) {
	id := fmt.Sprintf("%s#%s", txHash, logIdx)
	sender := ""
	if len(l.Topics) > 1 {
		sender = topicAddr(l.Topics[1])
	}
	amount0 := decodeUint256(l.Data, 0)
	amount1 := decodeUint256(l.Data, 1)

	idx.store.SeedSwap(id, &storage.SeedSwapData{
		Timestamp: int64(blockNum),
		Pool:      l.Address,
		Amount0:   amount0.String(),
		Amount1:   amount1.String(),
		AmountUSD: "0",
		Sender:    sender,
	})
}

func (idx *Indexer) handlePairCreated(l *logEntry) {
	if len(l.Topics) < 3 {
		return
	}
	data := strings.TrimPrefix(l.Data, "0x")
	if len(data) < 64 {
		return
	}
	token0 := topicAddr(l.Topics[1])
	token1 := topicAddr(l.Topics[2])
	pair := "0x" + data[:64]
	pair = topicAddr("0x" + pair[len(pair)-40:])

	idx.store.SeedPool(pair, &storage.SeedPoolData{
		Token0:  token0,
		Token1:  token1,
		FeeTier: 3000,
	})
	idx.store.SeedToken(token0, &storage.SeedTokenData{Symbol: token0[:8], Name: token0, Decimals: 18})
	idx.store.SeedToken(token1, &storage.SeedTokenData{Symbol: token1[:8], Name: token1, Decimals: 18})
	idx.bumpFactory(1)
}

func (idx *Indexer) handlePoolCreated(l *logEntry) {
	if len(l.Topics) < 4 {
		return
	}
	data := strings.TrimPrefix(l.Data, "0x")
	if len(data) < 64 {
		return
	}
	token0 := topicAddr(l.Topics[1])
	token1 := topicAddr(l.Topics[2])
	feeHex := strings.TrimPrefix(l.Topics[3], "0x")
	fee, _ := strconv.ParseInt(feeHex, 16, 64)

	// Pool address is in data (last 20 bytes of first 32-byte word if padded, or second word)
	pool := topicAddr("0x" + strings.TrimPrefix(l.Data, "0x"))

	idx.store.SeedPool(pool, &storage.SeedPoolData{
		Token0:  token0,
		Token1:  token1,
		FeeTier: fee,
	})
	idx.store.SeedToken(token0, &storage.SeedTokenData{Symbol: token0[:8], Name: token0, Decimals: 18})
	idx.store.SeedToken(token1, &storage.SeedTokenData{Symbol: token1[:8], Name: token1, Decimals: 18})
	idx.bumpFactory(1)
}

func (idx *Indexer) handleTransfer(l *logEntry, txHash, logIdx string) {
	// ERC20 Transfer — just record the token if we see it
	if len(l.Topics) >= 3 {
		idx.store.SeedToken(l.Address, &storage.SeedTokenData{
			Symbol: l.Address[:8], Name: l.Address, Decimals: 18,
		})
	}
}

// decodeInt256 reads a 32-byte word as a signed two's-complement big.Int. V4
// emits amount0/amount1 as int128 (sign-extended into a 32-byte word), so a
// straight unsigned read would misreport negative legs as astronomically large.
func decodeInt256(data string, wordIndex int) *big.Int {
	n := decodeUint256(data, wordIndex)
	// Words with the high bit set are negative: subtract 2^256.
	if n.Bit(255) == 1 {
		twoExp256 := new(big.Int).Lsh(big.NewInt(1), 256)
		n.Sub(n, twoExp256)
	}
	return n
}

// bumpFactory increments the singleton factory ("1") aggregate so the
// dashboard `factory`/`factories`/`uniswapFactories` query returns non-zero
// pool/tx counts. One place, called from every pool/pair creation handler —
// the factory aggregate was previously only ever seeded by tests, leaving the
// exchange landing page's TVL/volume header empty even with live pools.
func (idx *Indexer) bumpFactory(deltaPools int64) {
	f, _ := idx.store.GetFactory(nil, "1")
	var pc, tc int64
	if m, ok := f.(map[string]interface{}); ok {
		pc = asInt64(m["poolCount"])
		tc = asInt64(m["txCount"])
	}
	idx.store.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount: pc + deltaPools,
		TxCount:   tc + 1,
	})
}

// asInt64 best-effort coerces a JSON-decoded numeric (float64/json.Number/int)
// to int64 for aggregate accumulation.
func asInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		var out int64
		fmt.Sscanf(fmt.Sprint(v), "%d", &out)
		return out
	}
}

// handleInitializeV4 records a V4 pool (Initialize at the PoolManager). The
// pool id is the indexed bytes32 `id`; token0/token1 are the indexed
// currency0/currency1. Fee is the first data word. Pools created by the 0x9999
// settlement manager are ALSO surfaced as DEX (CLOB) Market entities.
func (idx *Indexer) handleInitializeV4(l *logEntry) {
	if len(l.Topics) < 4 {
		return
	}
	poolID := l.Topics[1] // full 32-byte id
	token0 := topicAddr(l.Topics[2])
	token1 := topicAddr(l.Topics[3])
	fee := decodeUint256(l.Data, 0).Int64()

	idx.store.SeedPool(poolID, &storage.SeedPoolData{
		Token0:  token0,
		Token1:  token1,
		FeeTier: fee,
	})
	idx.store.SeedToken(token0, &storage.SeedTokenData{Symbol: shortAddr(token0), Name: token0, Decimals: 18})
	idx.store.SeedToken(token1, &storage.SeedTokenData{Symbol: shortAddr(token1), Name: token1, Decimals: 18})
	idx.bumpFactory(1)

	if idx.isPoolManager(l.Address) {
		idx.store.SetEntity("Market", poolID, map[string]interface{}{
			"id": poolID, "symbol": poolID, "baseToken": token0, "quoteToken": token1,
			"feeTier": fee, "volume24h": "0", "tradeCount": 0, "lastPrice": "0",
		})
	}
}

// handleSwapV4 records a V4 swap (Swap at any V4 PoolManager) as an AMM swap —
// amount0/amount1 are signed int128. This is the AMM-side view only. The DEX
// (CLOB) Fill is NOT derived here: a settled native fill is recorded from its
// own authoritative DEXFill event (handleDEXFill), so the Fill has exactly one
// source. A vanilla (non-0x9999) V4 pool emits only this AMM swap, never a fill.
func (idx *Indexer) handleSwapV4(l *logEntry, blockNum uint64, txHash, logIdx string) {
	if len(l.Topics) < 3 {
		return
	}
	id := fmt.Sprintf("%s#%s", txHash, logIdx)
	poolID := l.Topics[1]
	sender := topicAddr(l.Topics[2])
	amount0 := decodeInt256(l.Data, 0)
	amount1 := decodeInt256(l.Data, 1)

	idx.store.SeedSwap(id, &storage.SeedSwapData{
		Timestamp: int64(blockNum),
		Pool:      poolID,
		Amount0:   amount0.String(),
		Amount1:   amount1.String(),
		AmountUSD: "0",
		Sender:    sender,
	})
	idx.bumpFactory(0)
}

// handleDEXFill records a native-CLOB settlement (DEXFill at 0x9999). This is
// the authoritative settled-fill signal: poolId + taker indexed, amountOut +
// blockNumber in data. Only logs from the configured PoolManager are honored —
// a spoofed DEXFill from another address is ignored.
func (idx *Indexer) handleDEXFill(l *logEntry, blockNum uint64, txHash, logIdx string) {
	if len(l.Topics) < 3 || !idx.isPoolManager(l.Address) {
		return
	}
	id := fmt.Sprintf("%s#%s", txHash, logIdx)
	poolID := l.Topics[1]
	taker := topicAddr(l.Topics[2])
	amountOut := decodeUint256(l.Data, 0)
	idx.writeFill(id, poolID, taker, amountOut, int64(blockNum), txHash)
}

// writeFill persists a DEX (CLOB) Fill entity and rolls its volume into the
// market aggregate. Shared by the V4-swap-at-0x9999 derivation and the explicit
// DEXFill handler so there is one fill-shape and one market-rollup path.
func (idx *Indexer) writeFill(id, poolID, taker string, amountOut *big.Int, ts int64, txHash string) {
	idx.store.SetEntity("Fill", id, map[string]interface{}{
		"id": id, "market": poolID, "taker": taker,
		"amountOut": amountOut.String(), "timestamp": ts, "txHash": txHash,
	})

	// Market rollup — accumulate volume + trade count, keyed by pool id.
	var vol = new(big.Int)
	var tc int64
	if m, _ := idx.store.GetByType("Market", poolID); m != nil {
		if mm, ok := m.(map[string]interface{}); ok {
			vol.SetString(fmt.Sprint(mm["volume24h"]), 10)
			tc = asInt64(mm["tradeCount"])
			vol.Add(vol, amountOut)
			mm["volume24h"] = vol.String()
			mm["tradeCount"] = tc + 1
			mm["lastUpdate"] = ts
			idx.store.SetEntity("Market", poolID, mm)
			return
		}
	}
	// First fill for a market we never saw an Initialize for — create a stub.
	idx.store.SetEntity("Market", poolID, map[string]interface{}{
		"id": poolID, "symbol": poolID, "volume24h": amountOut.String(),
		"tradeCount": int64(1), "lastUpdate": ts,
	})
}

// isPoolManager reports whether a log address is the configured 0x9999
// settlement precompile (case-insensitive).
func (idx *Indexer) isPoolManager(addr string) bool {
	return strings.EqualFold(addr, idx.poolManager)
}

// shortAddr returns a stable short symbol for an unknown token address.
func shortAddr(addr string) string {
	if len(addr) >= 8 {
		return addr[:8]
	}
	return addr
}
