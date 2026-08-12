//go:build nosqlite

package storage

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// Store is the unified storage backend (in-memory fallback for nosqlite builds).
type Store struct {
	dataDir string
	mu      sync.RWMutex

	factories map[string]*SeedFactoryData
	bundles   map[string]*SeedBundleData
	tokens    map[string]*SeedTokenData
	pools     map[string]*SeedPoolData
	swaps     map[string]*SeedSwapData
	mints     map[string]interface{}
	burns     map[string]interface{}
	ticks     map[string]interface{}
	positions map[string]interface{}
	generic   map[string]interface{}

	lastBlock uint64
}

// New creates an in-memory store (nosqlite build).
func New(dataDir string) (*Store, error) {
	return &Store{
		dataDir:   dataDir,
		factories: make(map[string]*SeedFactoryData),
		bundles:   make(map[string]*SeedBundleData),
		tokens:    make(map[string]*SeedTokenData),
		pools:     make(map[string]*SeedPoolData),
		swaps:     make(map[string]*SeedSwapData),
		mints:     make(map[string]interface{}),
		burns:     make(map[string]interface{}),
		ticks:     make(map[string]interface{}),
		positions: make(map[string]interface{}),
		generic:   make(map[string]interface{}),
	}, nil
}

func (s *Store) Init(_ context.Context) error { return nil }
func (s *Store) Close() error                 { return nil }
func (s *Store) DataDir() string              { return s.dataDir }

func (s *Store) SetLastBlock(block uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastBlock = block
}

func (s *Store) GetLastBlock() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastBlock
}

// --- Seed methods ---

func (s *Store) SeedFactory(id string, d *SeedFactoryData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.factories[id] = d
}

func (s *Store) SeedBundle(id string, d *SeedBundleData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bundles[id] = d
}

func (s *Store) SeedToken(id string, d *SeedTokenData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[id] = d
}

func (s *Store) SeedPool(id string, d *SeedPoolData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pools[id] = d
}

func (s *Store) SeedSwap(id string, d *SeedSwapData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.swaps[id] = d
}

// ValueSwaps writes what each trade was worth onto the stored swap. Callers
// pass values already formatted, so there is exactly one place that decides how
// a dollar figure is written.
func (s *Store) ValueSwaps(usd map[string]string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows int64
	for id, v := range usd {
		if sw, ok := s.swaps[id]; ok {
			sw.AmountUSD = v
			rows++
		}
	}
	return rows, nil
}

// --- Generic entity storage ---

// SetEntity stores an entity. The value is round-tripped through JSON so that
// what comes back out is what the sqlite backend would hand back — a map keyed
// by the json tags — whichever backend a build links. A caller that wrote a
// struct here and read a map there would work against one and not the other.
func (s *Store) SetEntity(entityType, id string, data interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v interface{}
	if j, err := json.Marshal(data); err == nil {
		json.Unmarshal(j, &v)
	}
	s.generic[entityType+":"+id] = v
}

func (s *Store) GetByType(entityType, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := entityType + ":" + id
	if v, ok := s.generic[key]; ok {
		return v, nil
	}
	return nil, nil
}

// ListByType returns every entity of a type, up to limit. The result is always
// a list — an unpopulated type yields an empty one, never a nil slice (which
// would encode as JSON `null`; see the sqlite backend for why that matters).
func (s *Store) ListByType(entityType string, limit int) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := entityType + ":"
	result := []interface{}{}
	for k, v := range s.generic {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			result = append(result, v)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// Entities returns the stored entities of a type, filtered, ordered and cut to
// limit. See the sqlite backend for why the whole type is read.
func (s *Store) Entities(entityType string, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := entityType + ":"
	result := []interface{}{}
	for k, v := range s.generic {
		if strings.HasPrefix(k, prefix) {
			result = append(result, v)
		}
	}
	return page(result, limit, orderBy, orderDirection, where), nil
}

// --- Block queries ---

func (s *Store) GetBlock(_ context.Context, id string) (interface{}, error)          { return nil, nil }
func (s *Store) GetBlockByNumber(_ context.Context, num string) (interface{}, error) { return nil, nil }
func (s *Store) GetLatestBlock(_ context.Context) (interface{}, error)               { return nil, nil }
func (s *Store) GetBlocks(_ context.Context, limit int) (interface{}, error) {
	return []interface{}{}, nil
}

// --- Transaction queries ---

func (s *Store) GetTransaction(_ context.Context, hash string) (interface{}, error) { return nil, nil }
func (s *Store) GetTransactions(_ context.Context, limit int) (interface{}, error) {
	return []interface{}{}, nil
}

// --- Token queries ---

func (s *Store) GetToken(_ context.Context, addr string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t, ok := s.tokens[addr]; ok {
		return map[string]interface{}{
			"id": addr, "symbol": t.Symbol, "name": t.Name, "decimals": t.Decimals,
			"volumeUSD": t.VolumeUSD, "totalValueLockedUSD": t.TotalValueLockedUSD,
			"derivedETH": t.DerivedETH, "txCount": t.TxCount,
		}, nil
	}
	return nil, nil
}

func (s *Store) GetTokens(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []interface{}{}
	for addr, t := range s.tokens {
		result = append(result, map[string]interface{}{
			"id": addr, "symbol": t.Symbol, "name": t.Name, "decimals": t.Decimals,
			"volumeUSD": t.VolumeUSD, "totalValueLockedUSD": t.TotalValueLockedUSD,
			"derivedETH": t.DerivedETH, "txCount": t.TxCount,
		})
	}
	result = FilterResults(result, where)
	sortResults(result, orderBy, orderDirection)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// --- DEX queries ---

func (s *Store) GetFactory(_ context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if f, ok := s.factories[id]; ok {
		return map[string]interface{}{
			"id": id, "poolCount": f.PoolCount, "txCount": f.TxCount,
			"totalVolumeUSD": f.TotalVolumeUSD, "totalValueLockedUSD": f.TotalValueLockedUSD,
		}, nil
	}
	return nil, nil
}

func (s *Store) GetFactories(_ context.Context) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []interface{}{}
	for id, f := range s.factories {
		result = append(result, map[string]interface{}{
			"id": id, "poolCount": f.PoolCount, "txCount": f.TxCount,
			"totalVolumeUSD": f.TotalVolumeUSD, "totalValueLockedUSD": f.TotalValueLockedUSD,
		})
	}
	return result, nil
}

func (s *Store) GetBundle(_ context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if b, ok := s.bundles[id]; ok {
		return map[string]interface{}{
			"id": id, "ethPriceUSD": b.EthPriceUSD, "ethPrice": b.EthPriceUSD,
		}, nil
	}
	return nil, nil
}

func (s *Store) GetPool(_ context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.pools[id]; ok {
		return s.poolToMap(id, p), nil
	}
	return nil, nil
}

func (s *Store) GetPools(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []interface{}{}
	for id, p := range s.pools {
		result = append(result, s.poolToMap(id, p))
	}
	result = FilterResults(result, where)
	sortResults(result, orderBy, orderDirection)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Store) poolToMap(id string, p *SeedPoolData) map[string]interface{} {
	var t0, t1 interface{}
	if tok, ok := s.tokens[p.Token0]; ok {
		t0 = map[string]interface{}{"id": p.Token0, "symbol": tok.Symbol, "name": tok.Name, "decimals": tok.Decimals}
	}
	if tok, ok := s.tokens[p.Token1]; ok {
		t1 = map[string]interface{}{"id": p.Token1, "symbol": tok.Symbol, "name": tok.Name, "decimals": tok.Decimals}
	}
	return map[string]interface{}{
		"id": id, "token0": t0, "token1": t1, "feeTier": p.FeeTier,
		"totalValueLockedUSD": p.TotalValueLockedUSD, "volumeUSD": p.VolumeUSD,
		"token0Price": p.Token0Price, "token1Price": p.Token1Price, "txCount": p.TxCount,
	}
}

func (s *Store) GetPair(_ context.Context, id string) (interface{}, error) { return s.GetPool(nil, id) }
func (s *Store) GetPairs(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return s.GetPools(nil, limit, orderBy, orderDirection, where)
}

func (s *Store) GetSwap(_ context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sw, ok := s.swaps[id]; ok {
		return s.swapToMap(id, sw), nil
	}
	return nil, nil
}

func (s *Store) GetSwaps(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []interface{}{}
	for id, sw := range s.swaps {
		result = append(result, s.swapToMap(id, sw))
	}
	result = FilterResults(result, where)
	if orderBy == "" {
		orderBy = "timestamp"
	}
	if orderDirection == "" {
		orderDirection = "desc"
	}
	sortResults(result, orderBy, orderDirection)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Store) swapToMap(id string, sw *SeedSwapData) map[string]interface{} {
	var pool interface{} = sw.Pool
	if p, ok := s.pools[sw.Pool]; ok {
		pool = s.poolToMap(sw.Pool, p)
	}
	return map[string]interface{}{
		"id": id, "timestamp": sw.Timestamp, "pool": pool,
		"amount0": sw.Amount0, "amount1": sw.Amount1, "amountUSD": sw.AmountUSD,
		"sender": sw.Sender,
	}
}

// Stubs
func (s *Store) GetMint(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetMints(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetBurn(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetBurns(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetTick(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetTicks(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetPosition(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetPositions(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}

func (s *Store) GetCollect(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetCollects(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetFlash(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetFlashes(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}

// --- Day series --- (see the sqlite backend)

func (s *Store) GetTokenDayDatas(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return s.Entities(TokenDay, limit, orderBy, orderDirection, where)
}
func (s *Store) GetPoolDayDatas(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return s.Entities(PoolDay, limit, orderBy, orderDirection, where)
}
func (s *Store) GetFactoryDayDatas(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return s.Entities(FactoryDay, limit, orderBy, orderDirection, where)
}
func (s *Store) GetPairDayDatas(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetPairHourDatas(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}

func (s *Store) GetTransfer(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetTransfers(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetNFT(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetNFTs(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	return []interface{}{}, nil
}

// --- V4 storage methods ---

func (s *Store) GetModifyLiquidity(_ context.Context, id string) (interface{}, error) {
	return nil, nil
}
func (s *Store) GetModifyLiquiditys(_ context.Context, limit int, orderBy, orderDirection string) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetSubscribe(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetSubscribes(_ context.Context, limit int, orderBy, orderDirection string) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetUnsubscribe(_ context.Context, id string) (interface{}, error) { return nil, nil }
func (s *Store) GetUnsubscribes(_ context.Context, limit int, orderBy, orderDirection string) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetPoolHourDatas(_ context.Context, limit int, orderBy, orderDirection string) (interface{}, error) {
	return []interface{}{}, nil
}
func (s *Store) GetTokenHourDatas(_ context.Context, limit int, orderBy, orderDirection string) (interface{}, error) {
	return []interface{}{}, nil
}

// --- Raw accessors (valuation) ---
// See the sqlite implementation for why the valuation pass needs the stored
// value rather than the presentation map.

// PoolsRaw returns every stored pool keyed by id.
func (s *Store) PoolsRaw() map[string]*SeedPoolData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*SeedPoolData, len(s.pools))
	for id, p := range s.pools {
		c := *p
		out[id] = &c
	}
	return out
}

// RecentSwapsRaw returns the `limit` most recent stored swaps keyed by id.
// See the sqlite implementation for why the bound exists.
func (s *Store) RecentSwapsRaw(limit int) map[string]*SeedSwapData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.swaps))
	for id := range s.swaps {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return s.swaps[ids[i]].Timestamp > s.swaps[ids[j]].Timestamp })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make(map[string]*SeedSwapData, len(ids))
	for _, id := range ids {
		c := *s.swaps[id]
		out[id] = &c
	}
	return out
}

// TokensRaw returns every stored token keyed by address.
func (s *Store) TokensRaw() map[string]*SeedTokenData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*SeedTokenData, len(s.tokens))
	for id, t := range s.tokens {
		c := *t
		out[id] = &c
	}
	return out
}
