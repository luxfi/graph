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
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/graph/storage"
)

// Status reports indexer progress.
type Status struct {
	LatestBlock   uint64 `json:"latestBlock"`
	IndexedEvents uint64 `json:"indexedEvents"`
}

// Config tunes an Indexer. PoolManager defaults to the 0x9999 settlement
// address, which every chain shares, and StartBlock to 0 (index from genesis).
// FactoryV2, FactoryV3 and Native have no defaults: they differ per chain, and
// a guess would be a claim that one chain's pools belong to another. Left
// empty, the matching surface stays empty.
type Config struct {
	RPC string
	// PoolManager is the DEX settlement precompile address (0x9999). Logs from
	// this address are additionally derived into the DEX (CLOB) graph schema as
	// Fill/Market entities. Lower-cased on construction for case-insensitive
	// compare against eth_getLogs `address` fields.
	PoolManager string
	// FactoryV2 is the canonical Uniswap-v2 factory. ONLY its PairCreated is
	// authoritative; pairs it creates are the only valid V2 Swap emitters. A
	// PairCreated from any other contract is rejected (otherwise anyone could
	// seed phantom pairs and inflate the public Factory/TVL/volume aggregates).
	// Lower-cased on construction.
	FactoryV2 string
	// FactoryV3 is the canonical Uniswap-v3 factory — same trust-root role as
	// FactoryV2 for PoolCreated and V3 Swaps. Lower-cased on construction.
	FactoryV3 string
	// Native is the wrapped native token (WLUX on Lux, WZOO on Zoo). The wire
	// format quotes every token against it — `token.derivedETH` is a price in
	// native units and `bundle.ethPriceUSD` is what one native unit is worth —
	// so this address is what makes a price expressible at all. Lower-cased on
	// construction. Empty => no native anchor, so no token quotes a price.
	Native string
	// StartBlock is the genesis-relative block to (re)index from. On a clean
	// chain relaunch the indexer rewinds to this value (see poll's reorg guard).
	StartBlock uint64
	// Label names this indexer in its log lines — e.g. "cchain/amm". A process
	// that runs one indexer per (chain, subgraph) is otherwise unable to say
	// WHICH indexer emitted a line, and an indexer that never logs is then
	// indistinguishable from one that is wedged. Empty => unprefixed.
	Label string
	// DexRPC, when non-empty, is the native D-Chain (dexvm) CLOB read-RPC root —
	// e.g. http://node:9650/v1/bc/D/dex. Setting it runs an ADDITIONAL, orthogonal
	// CLOBSource (clob.go) that polls dex_get_{markets,trades,orders} and writes
	// the same Market/Fill/Order entities the EVM 0x9999 path writes. This is the
	// native trading source: a D-Chain trade is a consensus state transition, not an
	// EVM log, so it never appears on the EVM RPC. Empty => EVM-only (unchanged).
	DexRPC string
}

// reorgDepth is how far the chain head may legitimately move backwards (deep
// reorg) before we treat a lower head as a chain RESET (re-genesis) rather than
// a reorg. A re-genesised chain reports a head far below our persisted cursor;
// a normal reorg is shallow. This avoids wiping progress on a transient blip
// while still self-healing across a clean relaunch.
const reorgDepth = 128

// regenesisConfirmations is how many CONSECUTIVE polls must observe a head far
// below our cursor before we even SPEND an RPC to confirm a re-genesis. A single
// low reading is NOT enough: an idle chain ("Building block", head=0x0) or a briefly
// lagging RPC backend can momentarily report a head below our cursor, and probing on
// every blip would be wasteful. A genuine relaunch keeps the head low for many polls
// as the fresh chain rebuilds; a transient dip recovers within one or two. The streak
// is a CHEAP PRE-FILTER only — the rewind itself is gated on a deterministic signal
// (a changed genesis hash), so even a head held low for the full window by a deeply
// lagging load-balanced backend never wipes a still-canonical chain.
const regenesisConfirmations = 3

// genesisBlockTag is the block selector for the genesis (block 0) header. The
// genesis hash is the chain-identity fingerprint we compare to decide a re-genesis:
// every node of a chain has block 0 regardless of how far behind its head is, so an
// equal genesis hash deterministically rules out a re-genesis even on a deeply-stale
// backend — unlike a by-number/by-hash probe of the CURSOR block, which a backend
// whose head is below the cursor cannot serve (it would null-out and false-trip).
const genesisBlockTag = "0x0"

// Indexer watches an EVM RPC and writes events to storage.
type Indexer struct {
	rpc         string
	poolManager string // lower-cased 0x9999 settlement address
	factoryV2   string // lower-cased canonical V2 factory (PairCreated trust root)
	factoryV3   string // lower-cased canonical V3 factory (PoolCreated trust root)
	native      string // lower-cased wrapped native token — the unit `derivedETH` is denominated in
	label       string // log prefix identifying this indexer (see Config.Label)
	startBlock  uint64
	store       *storage.Store
	client      *http.Client

	// clob is the optional native D-Chain CLOB source (clob.go). Non-nil only when
	// Config.DexRPC is set; Run then drives it in parallel with the EVM poller.
	clob *CLOBSource

	lastBlock     uint64
	lowHeadStreak int    // consecutive polls observing a head far below the cursor (re-genesis pre-filter)
	genesisHash   string // chain-identity fingerprint; a change ⇒ true re-genesis (lazily captured)
	status        Status

	// tokenCache memoises ERC20 metadata (symbol/name/decimals) per token
	// address for the process lifetime so each token is read from the chain at
	// most once — never per-block. Keyed lower-cased address → *SeedTokenData.
	// See erc20.go (tokenMeta/seedToken).
	tokenCache sync.Map

	// written remembers each day cell as it was last persisted, so a pass that
	// recomputes the whole history writes only the days that actually moved.
	// Owned by the valuation goroutine (series.go).
	written map[string]cell

	// genesisHashFn fetches the chain's genesis (block 0) hash. It is a field so
	// tests can drive the canonicality decision deterministically without a live
	// RPC; in production it is fetchGenesisHash (eth_getBlockByNumber 0x0). Honors
	// the no-eth_getCode rule — only a block header is read.
	genesisHashFn func(ctx context.Context) (string, error)
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
		factoryV2:   strings.ToLower(cfg.FactoryV2),
		factoryV3:   strings.ToLower(cfg.FactoryV3),
		native:      strings.ToLower(cfg.Native),
		label:       cfg.Label,
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
	idx.genesisHashFn = idx.fetchGenesisHash
	idx.lastBlock = idx.store.GetLastBlock()
	if idx.lastBlock < cfg.StartBlock {
		idx.lastBlock = cfg.StartBlock
	}
	if cfg.DexRPC != "" {
		idx.clob = NewCLOBSource(cfg.DexRPC, store)
	}
	return idx
}

// fetchGenesisHash reads the chain's genesis (block 0) header hash via
// eth_getBlockByNumber. Only the header is fetched (txs=false); this honors the
// no-eth_getCode rule and is cheap. The genesis hash is the chain-identity
// fingerprint used to distinguish a true re-genesis (hash changed) from a merely
// lagging/idle backend on the same chain (hash unchanged).
func (idx *Indexer) fetchGenesisHash(ctx context.Context) (string, error) {
	raw, err := idx.rpcCall(ctx, "eth_getBlockByNumber", []interface{}{genesisBlockTag, false})
	if err != nil {
		return "", err
	}
	var head struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return "", fmt.Errorf("parse genesis header: %w", err)
	}
	if head.Hash == "" {
		return "", fmt.Errorf("genesis header missing hash")
	}
	return strings.ToLower(head.Hash), nil
}

// ensureGenesisHash lazily captures the current chain's genesis hash as the
// baseline the first time it succeeds. Idempotent and a no-op once set (a string
// check), so it costs one RPC only on the first successful poll. Capturing the
// baseline before any reset decision guarantees maybeResetForRegenesis always has
// something to compare against.
func (idx *Indexer) ensureGenesisHash(ctx context.Context) {
	if idx.genesisHash != "" {
		return
	}
	h, err := idx.genesisHashFn(ctx)
	if err != nil {
		idx.logf("[indexer] genesis hash baseline unavailable (will retry): %v", err)
		return
	}
	idx.genesisHash = h
}

// Status returns current indexer progress.
func (idx *Indexer) Status() Status {
	return idx.status
}

// maybeResetForRegenesis rewinds the cursor to StartBlock ONLY on a confirmed
// re-genesis: the chain's genesis hash has changed. A re-genesised chain reports a
// head near zero while our store still holds the old chain's height; without this
// rewind `latest <= lastBlock` would hold forever and the new chain would never index.
//
// Two gates, in order:
//
//  1. CHEAP PRE-FILTER (hysteresis): the head must stay far below our cursor for
//     regenesisConfirmations consecutive polls. A shallow reorg, an idle chain
//     ("Building block", head=0x0), or a transient lag clears the streak before we
//     spend any extra RPC. This keeps the common case free of probes.
//
//  2. DETERMINISTIC SIGNAL: only after the streak confirms do we fetch the current
//     genesis hash and compare it to our captured baseline. A CHANGED hash ⇒ a
//     different chain ⇒ true re-genesis ⇒ rewind. An UNCHANGED hash ⇒ the same chain
//     behind a deeply-stale/idle/load-balanced backend ⇒ we must NOT wipe; we clear
//     the streak (it is proven not a reset) and keep the cursor. Because every node
//     of a chain serves block 0 regardless of how far behind its head is, this signal
//     cannot be false-tripped by lag — unlike the poll count alone, which a backend
//     held >reorgDepth behind for the full window would satisfy on a still-canonical
//     chain.
//
// If the baseline genesis hash is not yet captured (first run, or the probe failed),
// we capture the current one and decline to wipe this round — a reset cannot be
// proven without a baseline to compare against.
func (idx *Indexer) maybeResetForRegenesis(ctx context.Context, latest uint64) {
	farBelow := idx.lastBlock > idx.startBlock+reorgDepth && latest+reorgDepth < idx.lastBlock
	if !farBelow {
		idx.lowHeadStreak = 0 // head recovered (or normal) — abandon any suspicion.
		return
	}
	idx.lowHeadStreak++
	if idx.lowHeadStreak < regenesisConfirmations {
		idx.logf("[indexer] head %d << cursor %d (>%d behind) — suspected chain reset, awaiting confirmation %d/%d",
			latest, idx.lastBlock, reorgDepth, idx.lowHeadStreak, regenesisConfirmations)
		return
	}

	// Streak confirmed — spend one RPC for the deterministic check.
	current, err := idx.genesisHashFn(ctx)
	if err != nil {
		idx.logf("[indexer] head %d << cursor %d for %d polls but genesis-hash probe failed (%v) — "+
			"NOT wiping; cannot confirm a reset without a canonical signal", latest, idx.lastBlock, idx.lowHeadStreak, err)
		return
	}
	if idx.genesisHash == "" {
		// No baseline yet — adopt the current hash and decline to wipe this round.
		idx.genesisHash = current
		idx.logf("[indexer] head %d << cursor %d but no genesis baseline yet — captured %s, NOT wiping",
			latest, idx.lastBlock, current)
		return
	}
	if current == idx.genesisHash {
		// Same chain, just deeply behind/idle. The poll-count gate alone would have
		// wiped here; the genesis hash proves it canonical, so we hold the cursor.
		idx.logf("[indexer] head %d << cursor %d for %d polls but genesis hash unchanged (%s) — "+
			"canonical chain behind a stale backend, NOT wiping", latest, idx.lastBlock, idx.lowHeadStreak, current)
		idx.lowHeadStreak = 0
		return
	}

	idx.logf("[indexer] genesis hash changed %s -> %s (head %d << cursor %d) — re-genesis confirmed, rewinding to %d",
		idx.genesisHash, current, latest, idx.lastBlock, idx.startBlock)
	idx.lastBlock = idx.startBlock
	idx.store.SetLastBlock(idx.startBlock)
	idx.genesisHash = current
	idx.lowHeadStreak = 0
}

// Run starts the indexer loop. Blocks until ctx is cancelled. When a native
// D-Chain CLOB source is configured (Config.DexRPC), its poll loop runs in a
// sibling goroutine bound to the same ctx — orthogonal to the EVM poller, sharing
// only the store. A native-DEX chain has its trading state on the D-Chain, not on
// the EVM RPC, so both sources run to populate the AMM (EVM) and DEX (CLOB) views.
func (idx *Indexer) Run(ctx context.Context) error {
	idx.logf("[indexer] starting — rpc=%s lastBlock=%d", idx.rpc, idx.lastBlock)

	if idx.clob != nil {
		go func() {
			if err := idx.clob.Run(ctx); err != nil && ctx.Err() == nil {
				idx.logf("[indexer] clob source: %v", err)
			}
		}()
	}

	// Swaps an older build stored with a block number where their time belongs
	// are dated from their block header. It is the whole reason a day series can
	// cover the history already indexed rather than starting today, and it stops
	// as soon as every row has a time.
	go idx.keepSwapTimes(ctx)

	// The valuation pass (valuation.go) derives the USD aggregates from current
	// on-chain balances. It runs beside the log poller, not inside it: reserves
	// are chain STATE, events are chain HISTORY, and an idle chain still has
	// state to report. Sharing only the store keeps the two concerns apart.
	go idx.runValuation(ctx)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := idx.pollGuarded(ctx); err != nil {
				idx.logf("[indexer] poll error: %v", err)
			}
		}
	}
}

// pollGuarded runs one poll, converting any panic (e.g. a malformed RPC log that
// slips a length check) into an error so a single bad log cannot crash the whole
// graphd process — the loop logs it and continues on the next tick. This is the
// process-survival backstop; individual handlers still length-check their inputs.
func (idx *Indexer) pollGuarded(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("poll panic recovered: %v", r)
		}
	}()
	return idx.poll(ctx)
}

// rpcCall makes a JSON-RPC POST and returns the result field.
func (idx *Indexer) rpcCall(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	return idx.rpcCallTo(ctx, idx.rpc, method, params)
}

// rpcCallTo is rpcCall against a named endpoint. The chain's other chains — the
// P-Chain, for what is staked — sit beside the EVM one under the same node, and
// asking them is the same JSON-RPC with a different path.
func (idx *Indexer) rpcCallTo(ctx context.Context, url, method string, params interface{}) (json.RawMessage, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
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
//
// time is when the block was mined, in unix seconds. It is not part of the RPC
// result — a log says only which block it was in — so the poll reads it from
// the block header and stamps it here (see stampTimes). Zero means the header
// was not read, and a handler that needs a time drops the event rather than
// dating it to 1970.
type logEntry struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	LogIndex         string   `json:"logIndex"`
	TransactionIndex string   `json:"transactionIndex"`

	time int64
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

	// Capture the genesis-hash baseline (once) before any reset decision, then
	// run the re-genesis self-heal (see maybeResetForRegenesis).
	idx.ensureGenesisHash(ctx)
	idx.maybeResetForRegenesis(ctx, latest)

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

	// 3. Date the logs, then process them. One header read per block in the
	// batch, before any handler runs: a swap's time is part of the swap, not
	// something to be reconstructed afterwards.
	if err := idx.stampTimes(ctx, logs); err != nil {
		return fmt.Errorf("block times: %w", err)
	}
	for i := range logs {
		idx.processLog(ctx, &logs[i])
		idx.status.IndexedEvents++
	}

	// 4. Update progress
	idx.lastBlock = toBlock
	idx.status.LatestBlock = toBlock
	idx.store.SetLastBlock(toBlock)

	if len(logs) > 0 {
		idx.logf("[indexer] blocks %d..%d — %d events", fromBlock, toBlock, len(logs))
	}
	return nil
}

// processLog matches a log entry's topic0 and writes to storage. ctx is
// threaded through so the token-seeding handlers can read ERC20 metadata via
// eth_call on the same RPC the poll loop already holds a context for.
func (idx *Indexer) processLog(ctx context.Context, l *logEntry) {
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
		idx.handlePairCreated(ctx, l)
	case SigPoolCreated:
		idx.handlePoolCreated(ctx, l)
	case SigTransfer:
		idx.handleTransfer(ctx, l, txHash, logIdx)
	case SigInitializeV4:
		idx.handleInitializeV4(ctx, l)
	case SigSwapV4:
		idx.handleSwapV4(l, blockNum, txHash, logIdx)
	case SigDEXFill:
		idx.handleDEXFill(l, blockNum, txHash, logIdx)
	case SigMintV2, SigMintV3, SigBurnV2, SigBurnV3, SigSync,
		SigCollect, SigFlash, SigInitialize, SigModifyLiquidity:
		// Deliberately inert. Every one of these events only restates what a
		// pool HOLDS, and that is derived state the valuation pass already owns
		// (valuation.go) — from balanceOf at head, which covers V2, V3 and V4
		// alike and needs no history replay. Decoding them here would be a
		// second, V2-only writer for the same field.

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
// Malformed hex (a non-hex digit in the word) yields 0 rather than a silently
// half-parsed value: SetString reports ok=false and leaves n unmodified, so we
// reset to a clean zero. A bad word must not masquerade as a real (partial) amount.
func decodeUint256(data string, wordIndex int) *big.Int {
	data = strings.TrimPrefix(data, "0x")
	start := wordIndex * 64
	if start+64 > len(data) {
		return new(big.Int)
	}
	n := new(big.Int)
	if _, ok := n.SetString(data[start:start+64], 16); !ok {
		return new(big.Int)
	}
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
	// A V2 Swap is emitted by the pair contract, NOT the factory — so the gate is
	// "is the emitter a pair the canonical V2 factory created?". handlePairCreated
	// (gated to factoryV2) is the ONLY path that registers a pair, so a Swap from a
	// rogue pair was never registered and is dropped here. This closes the parallel
	// bypass into the same SeedSwap/factory volume aggregates the V4 path now gates.
	if !idx.isKnownPool(l.Address) {
		return
	}
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
		Timestamp: l.time,
		Block:     blockNum,
		Pool:      l.Address,
		Amount0:   amount0.String(),
		Amount1:   amount1.String(),
		AmountUSD: "0",
		Sender:    sender,
	})
	idx.bumpSwapCounts(l.Address)
}

func (idx *Indexer) handleSwapV3(l *logEntry, blockNum uint64, txHash, logIdx string) {
	// Same trust model as handleSwapV2: a V3 Swap is emitted by the pool, which is
	// only registered via handlePoolCreated (gated to factoryV3). A Swap from an
	// unregistered (rogue) pool is dropped.
	if !idx.isKnownPool(l.Address) {
		return
	}
	id := fmt.Sprintf("%s#%s", txHash, logIdx)
	sender := ""
	if len(l.Topics) > 1 {
		sender = topicAddr(l.Topics[1])
	}
	// A V3 Swap emits `int256 amount0, int256 amount1` — one leg of every swap
	// is NEGATIVE (the token leaving the pool). Reading them unsigned turned
	// that leg into ~2^256, which is invisible until something actually adds the
	// amounts up: the volume rollup then reported astronomical figures.
	amount0 := decodeInt256(l.Data, 0)
	amount1 := decodeInt256(l.Data, 1)

	idx.store.SeedSwap(id, &storage.SeedSwapData{
		Timestamp: l.time,
		Block:     blockNum,
		Pool:      l.Address,
		Amount0:   amount0.String(),
		Amount1:   amount1.String(),
		AmountUSD: "0",
		Sender:    sender,
	})
	idx.bumpSwapCounts(l.Address)
}

func (idx *Indexer) handlePairCreated(ctx context.Context, l *logEntry) {
	// Scope to the canonical V2 factory (the trust root), exactly as the V4
	// handlers scope to 0x9999. A PairCreated from any other contract is rejected:
	// honoring it would let anyone seed phantom pairs/tokens and inflate the
	// explorer's public Factory/TVL/volume aggregates — and would register a rogue
	// pair address whose Swaps would then be accepted (the parallel bypass).
	if len(l.Topics) < 3 || !strings.EqualFold(l.Address, idx.factoryV2) {
		return
	}
	data := strings.TrimPrefix(l.Data, "0x")
	if len(data) < 64 {
		return
	}
	token0 := topicAddr(l.Topics[1])
	token1 := topicAddr(l.Topics[2])
	pair := "0x" + data[:64]
	// Lower-case the pair key so isKnownPool's Swap-side lookup matches regardless
	// of how the RPC backend cased the emitter address (the V2 Swap's l.Address).
	pair = strings.ToLower(topicAddr("0x" + pair[len(pair)-40:]))

	idx.store.SeedPool(pair, &storage.SeedPoolData{
		Token0:  token0,
		Token1:  token1,
		FeeTier: 3000,
	})
	idx.seedToken(ctx, token0)
	idx.seedToken(ctx, token1)
	idx.bumpFactory()
}

func (idx *Indexer) handlePoolCreated(ctx context.Context, l *logEntry) {
	// Scope to the canonical V3 factory (the trust root), mirroring handlePairCreated
	// and the V4 0x9999 gate. A PoolCreated from a rogue contract is rejected so it
	// can neither inflate the Factory/TVL aggregates nor register a rogue pool whose
	// Swaps would be honored.
	if len(l.Topics) < 4 || !strings.EqualFold(l.Address, idx.factoryV3) {
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

	// Pool address is in data (last 20 bytes of first 32-byte word if padded, or second word).
	// Lower-cased so isKnownPool's V3 Swap-side lookup matches the emitter regardless of RPC casing.
	pool := strings.ToLower(topicAddr("0x" + strings.TrimPrefix(l.Data, "0x")))

	idx.store.SeedPool(pool, &storage.SeedPoolData{
		Token0:  token0,
		Token1:  token1,
		FeeTier: fee,
	})
	idx.seedToken(ctx, token0)
	idx.seedToken(ctx, token1)
	idx.bumpFactory()
}

func (idx *Indexer) handleTransfer(ctx context.Context, l *logEntry, txHash, logIdx string) {
	// ERC20 Transfer — record the token if we see it. seedToken reads the real
	// symbol/name/decimals from the chain once and caches, so the per-transfer
	// firing of this handler does NOT translate into per-block RPC.
	if len(l.Topics) >= 3 {
		idx.seedToken(ctx, l.Address)
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

// bumpFactory increments the singleton factory ("1") transaction count — the
// only field of the factory aggregate that IS a running total of events, and so
// the only one the log handlers own.
//
// poolCount and the USD aggregates are DERIVED, not accumulated: the valuation
// pass reads the true pool count out of the store and recomputes TVL/volume
// from on-chain balances every interval. An accumulator for those drifts on
// every restart, re-index and duplicate create, and there is no reason to count
// something the store can simply be asked for. SeedFactory is INSERT OR REPLACE,
// so this read-modify-write carries the derived fields through untouched.
// bumpSwapCounts records that one swap touched a pool: the pool's own txCount,
// both of its tokens', and the factory's. Every swap handler (V2/V3/V4) calls
// this and nothing else counts swaps, so the four aggregates cannot drift apart.
//
// Without it the swaps were stored but nothing counted them, and every txCount
// the explorer served read 0 — pools, tokens and "all-time trades" alike.
func (idx *Indexer) bumpSwapCounts(poolID string) {
	pools := idx.store.PoolsRaw()
	p, ok := pools[strings.ToLower(poolID)]
	if !ok {
		if p, ok = pools[poolID]; !ok {
			return // a swap from a pool we never registered is already dropped upstream
		}
	}

	next := *p
	next.TxCount = p.TxCount + 1
	idx.store.SeedPool(poolID, &next)

	tokens := idx.store.TokensRaw()
	for _, addr := range []string{next.Token0, next.Token1} {
		if addr == "" {
			continue
		}
		t, ok := tokens[strings.ToLower(addr)]
		if !ok {
			if t, ok = tokens[addr]; !ok {
				continue
			}
		}
		nt := *t
		nt.TxCount = t.TxCount + 1
		idx.store.SeedToken(addr, &nt)
	}

	idx.bumpFactory()
}

func (idx *Indexer) bumpFactory() {
	f, _ := idx.store.GetFactory(nil, "1")
	var pc, tc int64
	var tvl, vol string
	if m, ok := f.(map[string]interface{}); ok {
		pc = asInt64(m["poolCount"])
		tc = asInt64(m["txCount"])
		tvl = asString(m["totalValueLockedUSD"])
		vol = asString(m["totalVolumeUSD"])
	}
	idx.store.SeedFactory("1", &storage.SeedFactoryData{
		PoolCount:           pc,
		TxCount:             tc + 1,
		TotalValueLockedUSD: tvl,
		TotalVolumeUSD:      vol,
	})
}

// asString coerces a JSON-decoded value to its string form, returning "" for nil
// so a missing aggregate carries through a read-modify-write as empty (not "<nil>").
func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
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
func (idx *Indexer) handleInitializeV4(ctx context.Context, l *logEntry) {
	// Scope to the canonical settlement manager (0x9999), exactly like
	// handleDEXFill. Any contract can emit a lookalike Initialize; honoring them
	// would let anyone seed phantom pools/tokens and inflate the explorer's public
	// TVL/volume aggregates. Only the real PoolManager's Initialize is authoritative.
	if len(l.Topics) < 4 || !idx.isPoolManager(l.Address) {
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
	idx.seedToken(ctx, token0)
	idx.seedToken(ctx, token1)
	idx.bumpFactory()

	// A bound market carries a human BASE/QUOTE symbol (not the raw poolId) when
	// BOTH currencies resolve to a clean ERC20 symbol. `assetsBound` marks the
	// market as backed by two identified real assets. This is the ONE place the
	// market's display identity is computed, so every downstream surface — the
	// explorer /dex tab, the exchange-api real-asset gate (dexMarkets.ts requires
	// assetsBound + a clean pair symbol), and the FE — reads one complete value.
	// A token we cannot cleanly name yields no pair → the market stays unbound and
	// the public surface hides it rather than showing a junk hex symbol.
	symbol := poolID
	assetsBound := false
	if s0, s1 := idx.pairSide(ctx, token0), idx.pairSide(ctx, token1); s0 != "" && s1 != "" {
		symbol = s0 + "/" + s1
		assetsBound = true
	}

	// We only reach here for the 0x9999 PoolManager (gated above), so every
	// Initialize is also a DEX (CLOB) Market. MERGE with any stub a prior fill
	// already accumulated (writeFill creates a volume24h/tradeCount stub when a Fill
	// arrives before its Initialize). The Initialize supplies the RICH fields (token
	// pair, fee tier); it must NOT reset the accumulated trade aggregates to zero.
	// Defaults apply only when the field is absent (no prior stub).
	mk := map[string]interface{}{
		"id": poolID, "symbol": symbol, "assetsBound": assetsBound,
		"baseToken": token0, "quoteToken": token1,
		"feeTier": fee, "volume24h": "0", "tradeCount": int64(0), "lastPrice": "0",
	}
	if existing, _ := idx.store.GetByType("Market", poolID); existing != nil {
		if em, ok := existing.(map[string]interface{}); ok {
			for _, k := range []string{"volume24h", "tradeCount", "lastPrice", "lastUpdate"} {
				if v, present := em[k]; present {
					mk[k] = v
				}
			}
		}
	}
	idx.store.SetEntity("Market", poolID, mk)
}

// pairSide returns the clean, uppercase symbol for one side of a market pair, or
// "" if the token has no identifiable ERC20 symbol. A side is clean iff it is a
// real, non-placeholder symbol reduced to [A-Z0-9] of length 2–12 — the exact
// shape the exchange-api gate (dexMarkets.ts PAIR_SYMBOL) admits. A token whose
// symbol() reverts (placeholder = short address) yields "" so its market stays
// unbound and hidden, never surfaced with a junk symbol. The metadata is the same
// read-once-cached value seedToken already fetched, so this adds no RPC.
func (idx *Indexer) pairSide(ctx context.Context, addr string) string {
	meta := idx.tokenMeta(ctx, addr)
	if meta == nil || isPlaceholderSymbol(meta.Symbol, addr) {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToUpper(meta.Symbol) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) < 2 || len(s) > 12 {
		return ""
	}
	return s
}

// handleSwapV4 records a V4 swap (Swap at the 0x9999 PoolManager) as an AMM swap —
// amount0/amount1 are signed int128. This is the AMM-side view only. The DEX
// (CLOB) Fill is NOT derived here: a settled native fill is recorded from its
// own authoritative DEXFill event (handleDEXFill), so the Fill has exactly one
// source. Scoped to the canonical PoolManager (like handleInitializeV4 /
// handleDEXFill): a lookalike Swap from any other contract would otherwise inject
// phantom swaps and inflate the explorer's public volume aggregate.
func (idx *Indexer) handleSwapV4(l *logEntry, blockNum uint64, txHash, logIdx string) {
	if len(l.Topics) < 3 || !idx.isPoolManager(l.Address) {
		return
	}
	id := fmt.Sprintf("%s#%s", txHash, logIdx)
	poolID := l.Topics[1]
	sender := topicAddr(l.Topics[2])
	amount0 := decodeInt256(l.Data, 0)
	amount1 := decodeInt256(l.Data, 1)

	idx.store.SeedSwap(id, &storage.SeedSwapData{
		Timestamp: l.time,
		Block:     blockNum,
		Pool:      poolID,
		Amount0:   amount0.String(),
		Amount1:   amount1.String(),
		AmountUSD: "0",
		Sender:    sender,
	})
	idx.bumpSwapCounts(poolID)
	idx.bumpFactory()
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

// isKnownPool reports whether a log address is a V2 pair / V3 pool that the
// canonical factory created — i.e. one we persisted via handlePairCreated /
// handlePoolCreated (both gated to the canonical factory). It is the Swap-side
// half of the AMM trust root: the factory authorizes a pool, and only that pool's
// Swaps are honored. eth_getLogs returns logs in block+logIndex order, so the
// PairCreated/PoolCreated (in an earlier or the same block) is always processed
// before the pool's first Swap. Pairs are seeded lower-cased (topicAddr), so the
// lookup is case-insensitive.
func (idx *Indexer) isKnownPool(addr string) bool {
	p, _ := idx.store.GetPool(nil, strings.ToLower(addr))
	return p != nil
}

// shortAddr returns a stable short symbol for an unknown token address. It is
// the fallback when the chain has no ERC20 symbol()/name() to offer (a revert,
// a non-compliant contract, or an unreachable RPC) — see readERC20.
func shortAddr(addr string) string {
	if len(addr) >= 8 {
		return addr[:8]
	}
	return addr
}

// seedToken records a Token entity with ERC20 metadata read from the chain.
// It is the ONE place every pool/pair/transfer handler routes token writes
// through; it owns both the read-once cache (tokenMeta) and the persistence,
// so the four handlers stay in their lane and never duplicate enrichment.
//
// Cold-start clobber guard: SeedToken is INSERT OR REPLACE, and the in-memory
// cache is empty after a restart. If a token was already enriched in a prior
// run (its stored symbol differs from the address placeholder), the cheap
// metadata read might transiently fail (RPC blip) and we must NOT overwrite a
// good stored name with the address fallback. So on a cache miss we consult the
// store: an already-enriched row short-circuits the read entirely, and a freshly
// read placeholder never downgrades an existing real symbol.
func (idx *Indexer) seedToken(ctx context.Context, addr string) {
	key := strings.ToLower(addr)
	// Already settled this process (resolved metadata or a cached placeholder):
	// the value is in the store from the first sight, so the hot Transfer path
	// costs zero RPC AND zero DB writes on every subsequent block.
	if _, cached := idx.tokenCache.Load(key); cached {
		return
	}
	// First sight this process. If the store already holds a real (non-placeholder)
	// symbol from a previous run, adopt it as the cached value and skip the RPC —
	// the chain metadata is immutable, so a re-read would only cost RPC and risk a
	// blip-induced downgrade.
	if existing := idx.storedToken(addr); existing != nil && !isPlaceholderSymbol(existing.Symbol, addr) {
		idx.tokenCache.Store(key, existing)
		return
	}
	meta := idx.tokenMeta(ctx, addr)
	idx.store.SeedToken(addr, meta)
}

// BackfillTokens enriches Token rows that were persisted with the address
// placeholder (symbol == shortAddr(addr)) by an earlier build that did not read
// ERC20 metadata. It is the cheap, idempotent alternative to a full re-sync:
// it issues at most three eth_calls per *placeholder* token (already-enriched
// rows are skipped) and rewrites only those rows — no block re-scan, no cursor
// rewind, no effect on swaps/pools/factory aggregates.
//
// Safe to call at startup before Run, or as a one-shot maintenance pass. Returns
// the number of tokens enriched. Honors ctx cancellation between tokens.
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
	enriched := 0
	for _, r := range rows {
		if ctx.Err() != nil {
			return enriched, ctx.Err()
		}
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		addr, _ := m["id"].(string)
		symbol, _ := m["symbol"].(string)
		if addr == "" || !isPlaceholderSymbol(symbol, addr) {
			continue // already has a real symbol — nothing to do
		}
		before := symbol
		idx.seedToken(ctx, addr)
		// Count only rows that actually changed (the read may have reverted and
		// kept the placeholder — that token is now cached and won't be retried).
		if after := idx.storedToken(addr); after != nil && !isPlaceholderSymbol(after.Symbol, addr) && after.Symbol != before {
			enriched++
		}
	}
	idx.logf("[indexer] token backfill complete — %d/%d enriched", enriched, len(rows))
	return enriched, nil
}

// storedToken loads a token's persisted metadata, or nil if absent. Used only
// by the cold-start guard in seedToken.
func (idx *Indexer) storedToken(addr string) *storage.SeedTokenData {
	t, err := idx.store.GetToken(nil, addr)
	if err != nil || t == nil {
		return nil
	}
	m, ok := t.(map[string]interface{})
	if !ok {
		return nil
	}
	out := &storage.SeedTokenData{Decimals: defaultDecimals}
	if s, ok := m["symbol"].(string); ok {
		out.Symbol = s
	}
	if s, ok := m["name"].(string); ok {
		out.Name = s
	}
	out.Decimals = asInt64(m["decimals"])
	return out
}

// isPlaceholderSymbol reports whether a stored symbol is the address-derived
// fallback (shortAddr) rather than a real ERC20 symbol — i.e. a row that still
// needs enriching.
func isPlaceholderSymbol(symbol, addr string) bool {
	return symbol == "" || strings.EqualFold(symbol, shortAddr(addr))
}
