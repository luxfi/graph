//go:build !nosqlite

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// Store is the unified storage backend backed by SQLite WAL.
type Store struct {
	dataDir string
	mu      sync.RWMutex
	db      *sql.DB
}

// New creates a store rooted at dataDir with SQLite WAL.
func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("storage: mkdir %s: %w", dataDir, err)
	}
	dbPath := filepath.Join(dataDir, "graph.db")
	// One SQLite driver, the same one the indexer and the explorer load. It
	// carries a copy of the C amalgamation, so a second package bringing another
	// copy — a dependency two hops away is enough — collides at link time and
	// takes every test in the repo down with it. That is worth knowing before
	// adding a dependency here; it is what an unused ORM did to this module.
	db, err := sql.Open("sqlite3", dbPath+
		"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_cache_size=-64000")
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", dbPath, err)
	}
	// Write-ahead logging is a property of the file, not of a connection, so it
	// is set once here rather than handed to every connection that opens. It is
	// what lets readers keep reading while a pass writes.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("storage: enable WAL on %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(8)
	return &Store{dataDir: dataDir, db: db}, nil
}

// Init creates tables and indexes.
func (s *Store) Init(_ context.Context) error {
	schema := `
		CREATE TABLE IF NOT EXISTS factories (id TEXT PRIMARY KEY, data JSON);
		CREATE TABLE IF NOT EXISTS bundles   (id TEXT PRIMARY KEY, data JSON);
		CREATE TABLE IF NOT EXISTS tokens    (id TEXT PRIMARY KEY, data JSON);
		CREATE TABLE IF NOT EXISTS pools     (id TEXT PRIMARY KEY, data JSON);
		CREATE TABLE IF NOT EXISTS swaps     (id TEXT PRIMARY KEY, data JSON, timestamp INTEGER, pool TEXT);
		CREATE TABLE IF NOT EXISTS entities  (type TEXT, id TEXT, data JSON, PRIMARY KEY(type, id));
		CREATE TABLE IF NOT EXISTS meta      (key TEXT PRIMARY KEY, value TEXT);

		CREATE INDEX IF NOT EXISTS idx_swaps_timestamp ON swaps(timestamp);
		CREATE INDEX IF NOT EXISTS idx_swaps_pool      ON swaps(pool);
		CREATE INDEX IF NOT EXISTS idx_entities_type   ON entities(type);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Close closes the database.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DataDir returns the store's data directory.
func (s *Store) DataDir() string { return s.dataDir }

// SetLastBlock persists the indexer's last processed block.
func (s *Store) SetLastBlock(block uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec("INSERT OR REPLACE INTO meta(key, value) VALUES('lastBlock', ?)", strconv.FormatUint(block, 10))
}

// GetLastBlock returns the indexer's last processed block.
func (s *Store) GetLastBlock() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var v string
	if err := s.db.QueryRow("SELECT value FROM meta WHERE key='lastBlock'").Scan(&v); err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(v, 10, 64)
	return n
}

// --- Seed methods ---

func (s *Store) SeedFactory(id string, d *SeedFactoryData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(d)
	s.db.Exec("INSERT OR REPLACE INTO factories(id, data) VALUES(?, ?)", id, string(data))
}

func (s *Store) SeedBundle(id string, d *SeedBundleData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(d)
	s.db.Exec("INSERT OR REPLACE INTO bundles(id, data) VALUES(?, ?)", id, string(data))
}

func (s *Store) SeedToken(id string, d *SeedTokenData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(d)
	s.db.Exec("INSERT OR REPLACE INTO tokens(id, data) VALUES(?, ?)", id, string(data))
}

func (s *Store) SeedPool(id string, d *SeedPoolData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(d)
	s.db.Exec("INSERT OR REPLACE INTO pools(id, data) VALUES(?, ?)", id, string(data))
}

func (s *Store) SeedSwap(id string, d *SeedSwapData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(d)
	s.db.Exec("INSERT OR REPLACE INTO swaps(id, data, timestamp, pool) VALUES(?, ?, ?, ?)",
		id, string(data), d.Timestamp, d.Pool)
}

// ValueSwaps writes what each trade was worth onto the stored swap.
//
// One transaction and one prepared statement for the whole batch. Sent through
// the plain Exec path instead, every row commits on its own and the interval is
// spent waiting on the disk; a chain with a real trade history never finishes
// the pass. That cost is why the value used to be dropped on the floor and
// every swap read back a dollar amount of zero.
//
// Callers pass values already formatted, so there is exactly one place that
// decides how a dollar figure is written.
// It reports how many rows it actually changed. A write that quietly does
// nothing is the failure this had first: the pass ran, said nothing, and every
// swap kept reading zero under a pool claiming thousands in volume.
func (s *Store) ValueSwaps(usd map[string]string) (int64, error) {
	if len(usd) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	// The field is edited here rather than by SQLite's json_set: that function
	// belongs to the JSON1 extension, which this driver's amalgamation is not
	// always built with. It is present on a developer's machine and absent in
	// the image, so the statement prepared cleanly in a test and failed on every
	// pass in production.
	read, err := tx.Prepare(`SELECT data FROM swaps WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("prepare read: %w", err)
	}
	defer read.Close()
	write, err := tx.Prepare(`UPDATE swaps SET data = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("prepare write: %w", err)
	}
	defer write.Close()

	var rows int64
	for id, v := range usd {
		var raw string
		if err := read.QueryRow(id).Scan(&raw); err != nil {
			if err == sql.ErrNoRows {
				continue // the window moved on; nothing to price
			}
			tx.Rollback()
			return 0, fmt.Errorf("read %s: %w", id, err)
		}
		var d SeedSwapData
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("decode %s: %w", id, err)
		}
		d.AmountUSD = v
		next, err := json.Marshal(&d)
		if err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("encode %s: %w", id, err)
		}
		res, err := write.Exec(string(next), id)
		if err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("write %s: %w", id, err)
		}
		n, _ := res.RowsAffected()
		rows += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return rows, nil
}

// --- Generic entity storage ---

func (s *Store) SetEntity(entityType, id string, data interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, _ := json.Marshal(data)
	s.db.Exec("INSERT OR REPLACE INTO entities(type, id, data) VALUES(?, ?, ?)", entityType, id, string(j))
}

func (s *Store) GetByType(entityType, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var raw string
	if err := s.db.QueryRow("SELECT data FROM entities WHERE type=? AND id=?", entityType, id).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var v interface{}
	json.Unmarshal([]byte(raw), &v)
	return v, nil
}

// ListByType returns every entity of a type, up to limit. The result is always
// a list — an unpopulated type yields an empty one.
//
// It must never be a nil slice: nil encodes as JSON `null`, and a GraphQL list
// field answering `null` is the wire signature of a subgraph that is not
// deployed or is erroring, not of one that has simply seen no events yet. The
// `dex` subgraph answered `{"markets":null}` for exactly this reason while it
// was deployed, subscribed to 0x9999 and fully caught up to the chain head.
func (s *Store) ListByType(entityType string, limit int) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT data FROM entities WHERE type=? LIMIT ?", entityType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []interface{}{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var v interface{}
		json.Unmarshal([]byte(raw), &v)
		result = append(result, v)
	}
	return result, nil
}

// Entities returns the stored entities of a type, filtered, ordered and cut to
// limit.
//
// It reads the whole type where ListByType reads the first `limit` rows: a
// filter and an order are properties of the WHOLE collection, so a SQL LIMIT
// applied before them would answer with the first N rows of the table rather
// than the first N matches — a chart asking for the newest 90 days would get
// the oldest 90 rows of whatever happened to be stored.
func (s *Store) Entities(entityType string, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT data FROM entities WHERE type=?", entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []interface{}{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var v interface{}
		if json.Unmarshal([]byte(raw), &v) == nil {
			result = append(result, v)
		}
	}
	return page(result, limit, orderBy, orderDirection, where), nil
}

// --- Block queries (stubs) ---

func (s *Store) GetBlock(_ context.Context, id string) (interface{}, error)          { return nil, nil }
func (s *Store) GetBlockByNumber(_ context.Context, num string) (interface{}, error) { return nil, nil }
func (s *Store) GetLatestBlock(_ context.Context) (interface{}, error)               { return nil, nil }
func (s *Store) GetBlocks(_ context.Context, limit int) (interface{}, error) {
	return []interface{}{}, nil
}

// --- Transaction queries (stubs) ---

func (s *Store) GetTransaction(_ context.Context, hash string) (interface{}, error) { return nil, nil }
func (s *Store) GetTransactions(_ context.Context, limit int) (interface{}, error) {
	return []interface{}{}, nil
}

// --- Token queries ---

func (s *Store) GetToken(_ context.Context, addr string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, err := s.loadToken(addr)
	if err != nil || t == nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": addr, "symbol": t.Symbol, "name": t.Name, "decimals": t.Decimals,
		"volumeUSD": t.VolumeUSD, "totalValueLockedUSD": t.TotalValueLockedUSD,
		"derivedETH": t.DerivedETH, "txCount": t.TxCount,
		"totalSupply": t.TotalSupply, "staked": t.Staked,
	}, nil
}

func (s *Store) GetTokens(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT id, data FROM tokens")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []interface{}{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var t SeedTokenData
		json.Unmarshal([]byte(raw), &t)
		result = append(result, map[string]interface{}{
			"id": id, "symbol": t.Symbol, "name": t.Name, "decimals": t.Decimals,
			"volumeUSD": t.VolumeUSD, "totalValueLockedUSD": t.TotalValueLockedUSD,
			"derivedETH": t.DerivedETH, "txCount": t.TxCount,
			"totalSupply": t.TotalSupply, "staked": t.Staked,
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
	f, err := s.loadFactory(id)
	if err != nil || f == nil {
		return nil, err
	}
	return map[string]interface{}{
		"id": id, "poolCount": f.PoolCount, "txCount": f.TxCount,
		"totalVolumeUSD": f.TotalVolumeUSD, "totalValueLockedUSD": f.TotalValueLockedUSD,
	}, nil
}

func (s *Store) GetFactories(_ context.Context) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT id, data FROM factories")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []interface{}{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var f SeedFactoryData
		json.Unmarshal([]byte(raw), &f)
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
	var raw string
	if err := s.db.QueryRow("SELECT data FROM bundles WHERE id=?", id).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var b SeedBundleData
	json.Unmarshal([]byte(raw), &b)
	return map[string]interface{}{
		"id": id, "ethPriceUSD": b.EthPriceUSD, "ethPrice": b.EthPriceUSD,
	}, nil
}

func (s *Store) GetPool(_ context.Context, id string) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.loadPool(id)
	if err != nil || p == nil {
		return nil, err
	}
	return s.poolToMap(id, p), nil
}

func (s *Store) GetPools(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Load all pools first, then close rows before resolving tokens (avoids conn deadlock).
	type idPool struct {
		id string
		p  SeedPoolData
	}
	rows, err := s.db.Query("SELECT id, data FROM pools")
	if err != nil {
		return nil, err
	}
	var pools []idPool
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var p SeedPoolData
		json.Unmarshal([]byte(raw), &p)
		pools = append(pools, idPool{id, p})
	}
	rows.Close()

	result := []interface{}{}
	for _, pp := range pools {
		p := pp.p
		result = append(result, s.poolToMap(pp.id, &p))
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
	if tok, _ := s.loadToken(p.Token0); tok != nil {
		t0 = map[string]interface{}{"id": p.Token0, "symbol": tok.Symbol, "name": tok.Name, "decimals": tok.Decimals}
	}
	if tok, _ := s.loadToken(p.Token1); tok != nil {
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
	var raw string
	if err := s.db.QueryRow("SELECT data FROM swaps WHERE id=?", id).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var sw SeedSwapData
	json.Unmarshal([]byte(raw), &sw)
	return s.swapToMap(id, &sw), nil
}

func (s *Store) GetSwaps(_ context.Context, limit int, orderBy, orderDirection string, where map[string]interface{}) (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if orderBy == "" {
		orderBy = "timestamp"
	}
	if orderDirection == "" {
		orderDirection = "desc"
	}

	// Use SQL ordering for timestamp (indexed column)
	query := "SELECT id, data FROM swaps"
	if orderBy == "timestamp" {
		if strings.EqualFold(orderDirection, "desc") {
			query += " ORDER BY timestamp DESC"
		} else {
			query += " ORDER BY timestamp ASC"
		}
	}
	query += " LIMIT ?"

	// Load all swaps first, close rows, then resolve pool/token refs.
	type idSwap struct {
		id string
		sw SeedSwapData
	}
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	var swaps []idSwap
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var sw SeedSwapData
		json.Unmarshal([]byte(raw), &sw)
		swaps = append(swaps, idSwap{id, sw})
	}
	rows.Close()

	result := []interface{}{}
	for _, ss := range swaps {
		sw := ss.sw
		result = append(result, s.swapToMap(ss.id, &sw))
	}
	result = FilterResults(result, where)
	if orderBy != "timestamp" {
		sortResults(result, orderBy, orderDirection)
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Store) swapToMap(id string, sw *SeedSwapData) map[string]interface{} {
	var pool interface{} = sw.Pool
	if p, _ := s.loadPool(sw.Pool); p != nil {
		pool = s.poolToMap(sw.Pool, p)
	}
	return map[string]interface{}{
		"id": id, "timestamp": sw.Timestamp, "pool": pool,
		"amount0": sw.Amount0, "amount1": sw.Amount1, "amountUSD": sw.AmountUSD,
		"sender": sw.Sender,
	}
}

// Stubs for unimplemented entity types
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

// --- Day series ---
//
// The indexer writes one row per (subject, UTC day) as an entity; these read
// them back. Three subjects have readers because three are asked for: a token's
// candles, a pool's candles, and the protocol total behind the explore tiles.
// PairDayData and PairHourData are the v2 spellings and no client asks for
// them.

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

// --- Internal helpers ---

func (s *Store) loadFactory(id string) (*SeedFactoryData, error) {
	var raw string
	if err := s.db.QueryRow("SELECT data FROM factories WHERE id=?", id).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var f SeedFactoryData
	json.Unmarshal([]byte(raw), &f)
	return &f, nil
}

func (s *Store) loadToken(addr string) (*SeedTokenData, error) {
	var raw string
	if err := s.db.QueryRow("SELECT data FROM tokens WHERE id=?", addr).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var t SeedTokenData
	json.Unmarshal([]byte(raw), &t)
	return &t, nil
}

func (s *Store) loadPool(id string) (*SeedPoolData, error) {
	var raw string
	if err := s.db.QueryRow("SELECT data FROM pools WHERE id=?", id).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var p SeedPoolData
	json.Unmarshal([]byte(raw), &p)
	return &p, nil
}

// --- Raw accessors (valuation) ---
//
// The valuation pass (indexer/valuation.go) is the one owner of the derived USD
// aggregates. It must read a pool/swap row, compute, and write the SAME row back
// — and SeedPool/SeedSwap are INSERT OR REPLACE (full overwrite). Round-tripping
// through GetPools/GetSwaps would be lossy: those return a *presentation* map
// (tokens nested and dropped entirely when the token row is missing). These
// accessors hand back the stored value itself so a read-modify-write preserves
// every field the writer does not own.

// PoolsRaw returns every stored pool keyed by id.
func (s *Store) PoolsRaw() map[string]*SeedPoolData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]*SeedPoolData{}
	rows, err := s.db.Query("SELECT id, data FROM pools")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var p SeedPoolData
		if json.Unmarshal([]byte(raw), &p) == nil {
			out[id] = &p
		}
	}
	return out
}

// RecentSwapsRaw returns the `limit` most recent stored swaps keyed by id,
// newest first by the indexed timestamp column.
//
// The bound is not an optimisation. A live chain's swap table grows without
// limit — the Lux C-Chain's already spans a million blocks — so an unbounded
// read would make every caller's cost grow forever with chain age. Callers that
// summarise recent activity take a window; nothing needs the whole table in
// memory at once.
func (s *Store) RecentSwapsRaw(limit int) map[string]*SeedSwapData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]*SeedSwapData{}
	rows, err := s.db.Query("SELECT id, data FROM swaps ORDER BY timestamp DESC LIMIT ?", limit)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var sw SeedSwapData
		if json.Unmarshal([]byte(raw), &sw) == nil {
			out[id] = &sw
		}
	}
	return out
}

// TokensRaw returns every stored token keyed by address.
func (s *Store) TokensRaw() map[string]*SeedTokenData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]*SeedTokenData{}
	rows, err := s.db.Query("SELECT id, data FROM tokens")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var t SeedTokenData
		if json.Unmarshal([]byte(raw), &t) == nil {
			out[id] = &t
		}
	}
	return out
}
