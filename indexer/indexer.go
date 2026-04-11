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

// Indexer watches an EVM RPC and writes events to storage.
type Indexer struct {
	rpc    string
	store  *storage.Store
	client *http.Client

	lastBlock uint64
	status    Status
}

// New creates an indexer connected to the given RPC endpoint.
func New(rpc string, store *storage.Store) *Indexer {
	idx := &Indexer{
		rpc:   rpc,
		store: store,
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
	return idx
}

// Status returns current indexer progress.
func (idx *Indexer) Status() Status {
	return idx.status
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
	return []string{
		SigPairCreated, SigSwapV2, SigMintV2, SigBurnV2, SigSync, SigTransfer,
		SigPoolCreated, SigInitialize, SigSwapV3, SigMintV3, SigBurnV3,
		SigCollect, SigFlash,
		SigInitializeV4, SigModifyLiquidity, SigSwapV4,
	}
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
	case SigMintV2, SigMintV3, SigBurnV2, SigBurnV3, SigSync,
		SigCollect, SigFlash, SigInitialize,
		SigInitializeV4, SigModifyLiquidity, SigSwapV4:
		// Recognized but storage for these types not yet wired
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
}

func (idx *Indexer) handleTransfer(l *logEntry, txHash, logIdx string) {
	// ERC20 Transfer — just record the token if we see it
	if len(l.Topics) >= 3 {
		idx.store.SeedToken(l.Address, &storage.SeedTokenData{
			Symbol: l.Address[:8], Name: l.Address, Decimals: 18,
		})
	}
}
