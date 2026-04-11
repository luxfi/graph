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

type SeedSwapData struct {
	Timestamp int64
	Pool      string
	Amount0   string
	Amount1   string
	AmountUSD string
	Sender    string
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
func FilterResults(results []interface{}, where map[string]interface{}) []interface{} {
	if len(where) == 0 {
		return results
	}
	var out []interface{}
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

		cmp := compareValues(val, v)
		switch op {
		case "eq":
			if cmp != 0 {
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
