//go:build !nosqlite

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	// ONE sqlite C library in this binary, under a name nothing else claims.
	// csqlite is the house build of the amalgamation and registers "sqlite3".
	// Upstream mattn compiles a second copy of the same C, so linking both
	// defines every sqlite3_* symbol twice — a link failure on darwin, and on
	// linux survivable only because that linker is laxer about duplicates.
	//
	// The name is the other half. hanzoai/sqlite wraps this same C but takes
	// the name "sqlite", which modernc.org/sqlite also takes — and modernc is
	// in every binary that stores here, by way of hanzoai/replicate next door
	// in this package. Two packages registering one name is a panic in init,
	// before main runs.
	_ "github.com/hanzoai/csqlite"
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
		CREATE TABLE IF NOT EXISTS swaps     (id TEXT PRIMARY KEY, data JSON, timestamp INTEGER, pool TEXT, amountUSD REAL NOT NULL DEFAULT 0);
		CREATE TABLE IF NOT EXISTS entities  (type TEXT, id TEXT, data JSON, PRIMARY KEY(type, id));
		CREATE TABLE IF NOT EXISTS meta      (key TEXT PRIMARY KEY, value TEXT);

		CREATE INDEX IF NOT EXISTS idx_swaps_timestamp ON swaps(timestamp);
		CREATE INDEX IF NOT EXISTS idx_entities_type   ON entities(type);

		-- GetSwaps compares pool COLLATE NOCASE and orders by timestamp, and an
		-- index is used only when its collation matches the comparison. The
		-- earlier index on swaps(pool) was BINARY, so a pool's history was a full
		-- scan of every swap on the chain — seconds per pool, and a token page
		-- asks once per pool it trades in.
		DROP INDEX IF EXISTS idx_swaps_pool;
		CREATE INDEX IF NOT EXISTS idx_swaps_pool_time ON swaps(pool COLLATE NOCASE, timestamp);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// A swap's columns are the queryable projection of its document. Timestamp
	// and pool were promoted so a window and a pool's history could be asked of
	// the database instead of of Go; amountUSD is promoted for the same reason.
	// What the protocol has traded for all time is a SUM over this table, and a
	// figure that exists only inside a JSON document cannot be summed — this
	// driver's amalgamation is built without JSON1, so json_extract is not an
	// option here either (see ValueSwaps).
	//
	// A database written before the column gets it now, filled once from the
	// rows themselves. Every write since keeps the two in step, so the fill runs
	// exactly once per database and the ALTER failing IS how it knows.
	if _, err := s.db.Exec(`ALTER TABLE swaps ADD COLUMN amountUSD REAL NOT NULL DEFAULT 0`); err == nil {
		return s.projectAmounts()
	}
	return nil
}

// projectAmounts copies each stored swap's dollar value out of its document and
// into the column beside it.
func (s *Store) projectAmounts() error {
	rows, err := s.db.Query(`SELECT id, data FROM swaps`)
	if err != nil {
		return fmt.Errorf("storage: read swaps to project: %w", err)
	}
	type valued struct {
		id  string
		usd float64
	}
	// Drained whole before the write opens, because a read held open across a
	// write on the same file is how SQLite is asked to deadlock with itself.
	var priced []valued
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var d SeedSwapData
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		if usd := dollars(d.AmountUSD); usd != 0 {
			priced = append(priced, valued{id, usd})
		}
	}
	rows.Close()
	if len(priced) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("storage: project amounts: %w", err)
	}
	stmt, err := tx.Prepare(`UPDATE swaps SET amountUSD = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("storage: project amounts: %w", err)
	}
	defer stmt.Close()
	for _, v := range priced {
		if _, err := stmt.Exec(v.usd, v.id); err != nil {
			tx.Rollback()
			return fmt.Errorf("storage: project %s: %w", v.id, err)
		}
	}
	return tx.Commit()
}

// dollars reads a formatted dollar figure back as a number. Anything unreadable
// is worth nothing, which is what an unpriced swap already says.
func dollars(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// SwapsAfter returns up to limit swaps stored later than ts, oldest first.
//
// The mirror of RecentSwapsRaw: one reads the head of the table, this reads
// forward from wherever a caller last got to, so a history longer than any
// window can be walked a batch at a time and finish.
func (s *Store) SwapsAfter(ts int64, limit int) map[string]*SeedSwapData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]*SeedSwapData{}
	rows, err := s.db.Query(
		"SELECT id, data FROM swaps WHERE timestamp > ? ORDER BY timestamp ASC LIMIT ?", ts, limit)
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
		if json.Unmarshal([]byte(raw), &sw) != nil {
			continue
		}
		out[id] = &sw
	}
	return out
}

// PricedThrough is the newest trade time a valuation pass has reached walking
// forward from the start of the table, or 0 for a store that has never walked.
//
// A mark, not a test of the rows: a trade whose tokens reach no stablecoin
// cannot be priced at all, and asking "which rows still have no value" would
// hand back the same unpriceable trades every pass forever. Having LOOKED is
// monotone where having VALUED is not, so the mark is what is remembered.
func (s *Store) PricedThrough() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var v string
	if err := s.db.QueryRow("SELECT value FROM meta WHERE key='pricedThrough'").Scan(&v); err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

// SetPricedThrough records how far forward the pass has now valued.
func (s *Store) SetPricedThrough(ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec("INSERT OR REPLACE INTO meta(key, value) VALUES('pricedThrough', ?)",
		strconv.FormatInt(ts, 10))
}

// Traded is a count of trades and what they were worth.
type Traded struct {
	Trades    int64
	VolumeUSD float64
}

// TradedByPool reports what each pool has traded, over the whole table.
//
// One question sliced one way, and the protocol's own total is the fold of it —
// asked separately they drifted apart by fifty times: the protocol summed every
// trade ever indexed while each pool summed the window one pass had just read,
// so a landing page claimed $206,718 above a list of pools claiming $4,159.
//
// Over the whole table, not over a window: a pass values a bounded slice because
// the table grows forever, and that bound is right for the pass and wrong for a
// figure the wire calls all-time. Grouping costs one scan and there are as many
// rows out as there are pools.
func (s *Store) TradedByPool() (map[string]Traded, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT pool, COUNT(*), COALESCE(SUM(amountUSD), 0) FROM swaps GROUP BY pool COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("storage: traded by pool: %w", err)
	}
	defer rows.Close()
	out := map[string]Traded{}
	for rows.Next() {
		var pool string
		var t Traded
		if err := rows.Scan(&pool, &t.Trades, &t.VolumeUSD); err != nil {
			continue
		}
		out[strings.ToLower(pool)] = t
	}
	return out, rows.Err()
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

// SetSource records which addresses the rows below the cursor were built from.
// Called once the indexer has committed to a set, so a later start can tell
// whether the cursor it inherits describes the same book.
func (s *Store) SetSource(fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec("INSERT OR REPLACE INTO meta(key, value) VALUES('source', ?)", fingerprint)
}

// Source returns the fingerprint the rows were built from, or "" for a store
// written before this was recorded — which is indistinguishable from a store
// built by another set, and is treated as one.
func (s *Store) Source() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var v string
	if err := s.db.QueryRow("SELECT value FROM meta WHERE key='source'").Scan(&v); err != nil {
		return ""
	}
	return v
}

// DropDerived removes everything read off the chain, leaving the file and its
// schema. Used when the addresses changed under a store: those rows describe a
// different book, and mixing them with the new one gives an answer that was
// never true anywhere.
func (s *Store) DropDerived() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range []string{"factories", "bundles", "tokens", "pools", "swaps", "entities"} {
		if _, err := s.db.Exec("DELETE FROM " + t); err != nil {
			return err
		}
	}
	_, err := s.db.Exec("DELETE FROM meta WHERE key='lastBlock'")
	return err
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
	s.db.Exec("INSERT OR REPLACE INTO swaps(id, data, timestamp, pool, amountUSD) VALUES(?, ?, ?, ?, ?)",
		id, string(data), d.Timestamp, d.Pool, dollars(d.AmountUSD))
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
	write, err := tx.Prepare(`UPDATE swaps SET data = ?, amountUSD = ? WHERE id = ?`)
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
		res, err := write.Exec(string(next), dollars(v), id)
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

	// A filter on the pool goes into the SQL, because the LIMIT is applied by
	// the database and a limit that runs before the filter answers with the
	// first N rows rather than the first N matches.
	//
	// One pair does most of the trading on a live chain, so it fills any recent
	// window by itself: asking for one quiet pool's last 1000 trades read the
	// 1000 newest chain-wide, kept the handful that matched, and usually
	// answered with none at all. The pool column is indexed for exactly this.
	args := []interface{}{}
	conds := []string{}
	if pool, ok := whereString(where, "pool"); ok {
		conds = append(conds, "pool = ? COLLATE NOCASE")
		args = append(args, pool)
	}
	// A page of history is asked for as "the N before this time", and that bound
	// has to sit beside the LIMIT for the same reason the pool does: applied
	// after it, the query returns the newest N and then throws them all away.
	if before, ok := whereNumber(where, "timestamp_lt"); ok {
		conds = append(conds, "timestamp < ?")
		args = append(args, before)
	}
	query := "SELECT id, data FROM swaps"
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	if orderBy == "timestamp" {
		if strings.EqualFold(orderDirection, "desc") {
			query += " ORDER BY timestamp DESC"
		} else {
			query += " ORDER BY timestamp ASC"
		}
	}
	query += " LIMIT ?"
	args = append(args, limit)

	// Load all swaps first, close rows, then resolve pool/token refs.
	type idSwap struct {
		id string
		sw SeedSwapData
	}
	rows, err := s.db.Query(query, args...)
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

// whereString reads a plain string equality out of a `where` map — the shape a
// filter naming one pool arrives in. Anything richer is left to FilterResults,
// which still runs over whatever the query returned.
func whereString(where map[string]interface{}, key string) (string, bool) {
	v, ok := where[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

// whereNumber reads a numeric bound out of a `where` map, however the client
// wrote it — GraphQL variables arrive as float64, literals may arrive as strings.
func whereNumber(where map[string]interface{}, key string) (float64, bool) {
	v, ok := where[key]
	if !ok || v == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	if err != nil {
		return 0, false
	}
	return f, true
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

// UndatedSwapsRaw returns up to `limit` swaps that still hold a block number
// where a time belongs, keyed by id.
//
// It asks for them by the property that identifies them — no swap is mined in
// block 0, so an unset block is exactly the older build's row — rather than by
// taking a window of the newest. The newest is the one window they are never
// in: an undated row holds a block number, block numbers are small beside unix
// seconds, so ordering by timestamp descending sorts every row needing repair
// to the far end of the table, behind every row already correct.
func (s *Store) UndatedSwapsRaw(limit int) map[string]*SeedSwapData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]*SeedSwapData{}
	// A row's block lives in the JSON document; only id, timestamp and pool are
	// columns. Ascending timestamp puts the undated rows first for the same
	// reason descending puts them last, so the scan reaches them immediately and
	// the Dated test below decides.
	rows, err := s.db.Query("SELECT id, data FROM swaps ORDER BY timestamp ASC LIMIT ?", limit)
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
		if json.Unmarshal([]byte(raw), &sw) == nil && !sw.Dated() && sw.Timestamp > 0 {
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
