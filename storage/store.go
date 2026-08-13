// Package storage provides the data layer for graph.
//
// SQLite WAL for production (default), in-memory maps for nosqlite builds.
// All reads are concurrent-safe. Writes are serialized.
package storage

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Entity types of the day series. The indexer writes them, the day-data
// readers read them, and naming them once keeps the two ends from drifting.
const (
	TokenDay   = "TokenDayData"
	PoolDay    = "PoolDayData"
	FactoryDay = "FactoryDayData"
)

// Seed types for data population.
// Used by the indexer and e2e tests to inject state.

type SeedFactoryData struct {
	PoolCount           int64
	TxCount             int64
	TotalVolumeUSD      string
	TotalValueLockedUSD string
}

type SeedBundleData struct {
	EthPriceUSD string
}

type SeedTokenData struct {
	Symbol              string
	Name                string
	Decimals            int64
	// TotalSupply is the contract's own uint256, as text. It is what turns a
	// price into a fully diluted value; without it a token page can only print
	// a dash where that belongs.
	TotalSupply         string
	VolumeUSD           string
	TotalValueLockedUSD string
	DerivedETH          string
	TxCount             int64
}

type SeedPoolData struct {
	Token0              string
	Token1              string
	FeeTier             int64
	TotalValueLockedUSD string
	VolumeUSD           string
	Token0Price         string
	Token1Price         string
	TxCount             int64
}

// SeedSwapData is one trade.
//
// Timestamp is the UNIX SECOND its block was mined, matching the subgraph wire
// field of the same name — a swap's time is a time, and every consumer (trade
// history, the day series) reads it as one.
//
// Block is that block's number. It is also what tells a row written by an older
// build apart from a current one: that build stored the block NUMBER in
// Timestamp and left Block unwritten, and no swap is ever mined in block 0, so
// Block == 0 identifies exactly the rows whose time still needs reading from
// the chain (see healSwapTimes).
type SeedSwapData struct {
	Timestamp int64
	Block     uint64
	Pool      string
	Amount0   string
	Amount1   string
	AmountUSD string
	Sender    string
}

// Dated reports whether Timestamp is a time. A row without a block was written
// before swaps carried one, so what it holds is a block number and the trade
// belongs to no day until healSwapTimes reads the real one. Its amounts are
// still amounts — an undated trade counts towards volume, just not towards a
// particular day.
func (d *SeedSwapData) Dated() bool { return d.Block != 0 && d.Timestamp > 0 }

// page applies a list query's three arguments — where, order, first — in the
// one order that answers the question asked: match, then sort the matches, then
// cut. Every collection read goes through it so no two of them can disagree
// about what `first: 90, orderBy: date` means.
func page(results []interface{}, limit int, orderBy, orderDirection string, where map[string]interface{}) []interface{} {
	results = FilterResults(results, where)
	sortResults(results, orderBy, orderDirection)
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// sortResults sorts a slice of map[string]interface{} by the named field.
func sortResults(results []interface{}, orderBy, orderDirection string) {
	if orderBy == "" || len(results) == 0 {
		return
	}
	desc := strings.EqualFold(orderDirection, "desc")
	sort.SliceStable(results, func(i, j int) bool {
		mi, _ := results[i].(map[string]interface{})
		mj, _ := results[j].(map[string]interface{})
		if mi == nil || mj == nil {
			return false
		}
		vi := mi[orderBy]
		vj := mj[orderBy]
		cmp := compareValues(vi, vj)
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

// compareValues compares two interface{} values. If both parse as floats,
// compares numerically. Otherwise falls back to string comparison.
func compareValues(a, b interface{}) int {
	sa := fmt.Sprint(a)
	sb := fmt.Sprint(b)
	fa, errA := strconv.ParseFloat(sa, 64)
	fb, errB := strconv.ParseFloat(sb, 64)
	if errA == nil && errB == nil {
		if fa < fb {
			return -1
		}
		if fa > fb {
			return 1
		}
		return 0
	}
	if sa < sb {
		return -1
	}
	if sa > sb {
		return 1
	}
	return 0
}

// FilterResults filters a slice of map results by where conditions.
// Supports exact match and _gte, _lte, _gt, _lt suffixes for numeric comparisons.
//
// The result is always non-nil: filtering everything out yields an empty list,
// never nil. A nil slice encodes as JSON `null`, and a GraphQL list field that
// answers `null` reads as "this collection does not exist" (broken subgraph)
// rather than "nothing matched" — see ListByType.
func FilterResults(results []interface{}, where map[string]interface{}) []interface{} {
	if len(where) == 0 {
		return results
	}
	out := []interface{}{}
	for _, r := range results {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if matchesWhere(m, where) {
			out = append(out, r)
		}
	}
	return out
}

// equalValues reports equality for a `where` exact match. Text compares without
// regard to case: an address is the same address checksummed or lower-cased, and
// both spellings reach us — the store writes the lower-cased form the RPC
// returns, while a client hands back whatever its wallet or URL carried.
func equalValues(a, b interface{}) bool {
	sa, aok := a.(string)
	sb, bok := b.(string)
	if aok && bok {
		if _, err := strconv.ParseFloat(sa, 64); err != nil {
			return strings.EqualFold(sa, sb)
		}
	}
	return compareValues(a, b) == 0
}

func matchesWhere(m map[string]interface{}, where map[string]interface{}) bool {
	for k, v := range where {
		var field, op string
		switch {
		case strings.HasSuffix(k, "_gte"):
			field = k[:len(k)-4]
			op = "gte"
		case strings.HasSuffix(k, "_lte"):
			field = k[:len(k)-4]
			op = "lte"
		case strings.HasSuffix(k, "_gt"):
			field = k[:len(k)-3]
			op = "gt"
		case strings.HasSuffix(k, "_lt"):
			field = k[:len(k)-3]
			op = "lt"
		default:
			field = k
			op = "eq"
		}

		val, exists := m[field]
		if !exists {
			return false
		}
		// A reference field holds the entity it points at, so that a client can
		// select through it (`tokenDayDatas { token { id } }`). The filter names
		// the referenced id — `where: {token: "0x…"}` — so compare against that.
		if ref, ok := val.(map[string]interface{}); ok {
			if id, ok := ref["id"]; ok {
				val = id
			}
		}

		cmp := compareValues(val, v)
		switch op {
		case "eq":
			if !equalValues(val, v) {
				return false
			}
		case "gte":
			if cmp < 0 {
				return false
			}
		case "lte":
			if cmp > 0 {
				return false
			}
		case "gt":
			if cmp <= 0 {
				return false
			}
		case "lt":
			if cmp >= 0 {
				return false
			}
		}
	}
	return true
}
