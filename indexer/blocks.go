package indexer

// When a swap happened.
//
// eth_getLogs answers WHICH block a log was in and never WHEN it was mined, so
// an indexer that writes only what the log carries has no clock. This one wrote
// the block NUMBER into the swap's `timestamp` — a field the subgraph wire
// defines as a unix second and every consumer reads as one. Trade history dated
// every fill to January 1970, and a day series was not merely empty but
// impossible: there is no day to put a block number in.
//
// So the poll reads the header of each block it saw an event in and carries the
// time onto the log, and healSwapTimes gives the rows an older build already
// wrote the same time from the same source. One notion of when: the chain's.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/luxfi/graph/storage"
)

// blockTimeBatch is how many headers one JSON-RPC batch asks for. Headers are
// small and the node answers them from its index; the bound is here so a first
// run over a long history is many bounded requests rather than one unbounded
// one.
const blockTimeBatch = 256

// healRetry is how long to wait before re-attempting the one-shot heal. It only
// ever runs again if the RPC was unreachable, so this is a reconnect delay, not
// a poll interval.
const healRetry = 30 * time.Second

// blockTimes reads the mining time of each block, in unix seconds.
//
// Blocks the node did not answer for are ABSENT from the result. A missing time
// must not read as zero: zero is 1970, and a swap dated 1970 would silently
// land in a day bucket half a century away from every other one.
func (idx *Indexer) blockTimes(ctx context.Context, blocks []uint64) map[uint64]int64 {
	out := make(map[uint64]int64, len(blocks))
	for start := 0; start < len(blocks); start += blockTimeBatch {
		end := start + blockTimeBatch
		if end > len(blocks) {
			end = len(blocks)
		}
		batch := blocks[start:end]
		reqs := make([]rpcBatchReq, 0, len(batch))
		for i, b := range batch {
			reqs = append(reqs, rpcBatchReq{
				JSONRPC: "2.0", ID: i + 1, Method: "eth_getBlockByNumber",
				Params: []interface{}{fmt.Sprintf("0x%x", b), false},
			})
		}
		res, err := idx.rpcBatch(ctx, reqs)
		if err != nil {
			idx.logf("[indexer] block times %d..%d: %v", batch[0], batch[len(batch)-1], err)
			continue
		}
		for i, b := range batch {
			raw, ok := res[i+1]
			if !ok {
				continue
			}
			var head struct {
				Timestamp string `json:"timestamp"`
			}
			if json.Unmarshal(raw, &head) != nil || head.Timestamp == "" {
				continue
			}
			ts, err := parseHexUint64(head.Timestamp)
			if err != nil {
				continue
			}
			out[b] = int64(ts)
		}
	}
	return out
}

// stampTimes gives each log the time of its block, one batch of headers for the
// whole poll.
//
// A block whose header does not answer is reported as an error and the poll
// does not advance past it, exactly as it already declines to advance past a
// failed eth_getLogs. A block is indexed when it has been read WHOLE; half of
// it is not progress, and the alternative — admitting a trade with no time, or
// with a made-up one — puts a wrong candle on a chart that no later pass would
// know to correct.
func (idx *Indexer) stampTimes(ctx context.Context, logs []logEntry) error {
	var blocks []uint64
	seen := map[uint64]bool{}
	for i := range logs {
		b, err := parseHexUint64(logs[i].BlockNumber)
		if err != nil || seen[b] {
			continue
		}
		seen[b] = true
		blocks = append(blocks, b)
	}
	if len(blocks) == 0 {
		return nil
	}
	times := idx.blockTimes(ctx, blocks)
	for i := range logs {
		b, err := parseHexUint64(logs[i].BlockNumber)
		if err != nil {
			continue // a log we cannot place in a block; processLog reads it as block 0
		}
		ts, ok := times[b]
		if !ok {
			return fmt.Errorf("no header for block %d", b)
		}
		logs[i].time = ts
	}
	return nil
}

// healSwapTimes gives stored swaps the real time of their block, and reports
// how many still lack one.
//
// A row written by an older build holds its block NUMBER in Timestamp and has
// no Block at all. That is not a guess: no swap is mined in block 0, so an
// unset Block identifies exactly those rows, and the number it left behind is
// the very thing needed to look the time up. A row whose supposed block the
// chain does not answer for is left alone — this heals what it can prove.
func (idx *Indexer) healSwapTimes(ctx context.Context) (int, error) {
	swaps := idx.store.RecentSwapsRaw(maxValuedSwaps)
	stale := map[string]*storage.SeedSwapData{}
	var blocks []uint64
	seen := map[uint64]bool{}
	for id, sw := range swaps {
		if sw.Dated() || sw.Timestamp <= 0 {
			continue
		}
		b := uint64(sw.Timestamp)
		stale[id] = sw
		if !seen[b] {
			seen[b] = true
			blocks = append(blocks, b)
		}
	}
	if len(stale) == 0 {
		return 0, nil
	}

	times := idx.blockTimes(ctx, blocks)
	if len(times) == 0 {
		return len(stale), fmt.Errorf("no block header answered for %d blocks", len(blocks))
	}
	healed := 0
	for id, sw := range stale {
		block := uint64(sw.Timestamp)
		ts, ok := times[block]
		if !ok {
			continue
		}
		next := *sw
		next.Block, next.Timestamp = block, ts
		idx.store.SeedSwap(id, &next)
		healed++
	}
	idx.logf("[indexer] swap times healed — %d of %d rows dated from their block header", healed, len(stale))
	return len(stale) - healed, nil
}

// keepSwapTimes runs the heal until every stored swap has a real time, then
// stops. New swaps are dated as they arrive, so the only way this runs twice is
// an RPC that was unreachable the first time.
func (idx *Indexer) keepSwapTimes(ctx context.Context) {
	for {
		stale, err := idx.healSwapTimes(ctx)
		if stale == 0 && err == nil {
			return
		}
		if err != nil {
			idx.logf("[indexer] swap times: %v (%d rows still undated)", err, stale)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(healRetry):
		}
	}
}

// dayStart is the UTC midnight at or before ts, in unix seconds — the `date` of
// the day series, matching the subgraph convention that a day is identified by
// its own first second.
func dayStart(ts int64) int64 { return ts - ts%dayLength }

// dayLength is a day in seconds.
const dayLength = 86400

// dayID is the subgraph's identity for one subject's day: the subject's address
// and the day's index since the epoch.
func dayID(subject string, date int64) string {
	return fmt.Sprintf("%s-%d", strings.ToLower(subject), date/dayLength)
}
