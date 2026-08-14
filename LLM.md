# graph

**Org:** luxfi  ·  **Ecosystem:** lux  ·  **Path:** `/Users/a/work/lux/luxfi/graph`
**Origin:** https://github.com/luxfi/graph.git

## Discovery

This file (`CLAUDE.md`) is the canonical agent-facing readme; `LLM.md` is a symlink to it. Update either name and both stay in sync.

## Where to look first

- `README.md` — human-facing overview (if present)
- `package.json` / `Cargo.toml` / `pyproject.toml` / `go.mod` — language & deps
- `.github/workflows/` — CI surface
- `docs/` — extended docs (if present)

## A token's `totalSupply` is WHOLE TOKENS, and a named row is not a finished one

Two things about `Token` that a reader has to know before touching `erc20.go`:

- **One unit.** `SeedTokenData.TotalSupply` (and `Staked`) is whole tokens as an
  exact decimal string, never base units. The contract reports base units and
  `publishNativeSupply` reports a genesis figure already whole; both land in the
  same field, and a client that scales one and not the other is a valuation
  10^18 out. `wholeUnits` does the conversion, with integer division and the
  remainder — float64 gives up around 2^53, four orders of magnitude below a
  two-trillion-token supply. The exchange multiplies this by a price directly.
- **One predicate.** `settled(row, addr)` — a real symbol AND a supply — decides
  whether a row still has anything to ask the chain. `token()` is the only
  resolver: store first, contract for the rest, memoised per address for the
  process, persisted once. `BackfillTokens` asks the same predicate at every
  start, so a row is completed by starting rather than by remembering a flag.
  Keying "finished" on the symbol alone is what left every token indexed before
  v1.2.25 supply-less forever: the row had a name, so nothing asked again, and
  market cap and fully diluted value printed a dash however much it traded.

`resolve` folds a read over the stored row and never erases: a reverting call
must not downgrade a real symbol, and `SeedToken` is INSERT OR REPLACE, so a
write built only from the contract read zeroes the value locked, volume, price
and trade count the valuation pass put on that same row.

🪤 **The sqlite driver is `hanzoai/csqlite`, opened as `"sqlite3"`.** Not upstream
mattn (a second copy of the same amalgamation — every `sqlite3_*` symbol twice,
a link failure on darwin), and not the `hanzoai/sqlite` wrapper, which registers
the name `"sqlite"` — as does `modernc.org/sqlite`, which arrives here with
`hanzoai/replicate` next door in `storage`. Two packages under one name is a
panic in `database/sql` init, before main runs: v1.2.28 through v1.2.30 could
not start at all.

## A swap's `timestamp` is a TIME, and the poll is what makes it one

`eth_getLogs` says which block a log was in and never when it was mined. Until
v1.2.17 the swap handlers wrote the block NUMBER into `timestamp`, so trade
history dated every fill to 1970 and no day could be computed from a swap at
all. `stampTimes` (blocks.go) now reads one batch of headers per poll and hangs
the time on the `logEntry`, and `poll` returns an error rather than advancing
past a block whose header did not answer — **a block is indexed when it has been
read whole**.

`SeedSwapData.Block` is what tells an old row from a new one: no swap is mined
in block 0, so `Block == 0` means "the number in Timestamp is a block number",
which is exactly what `healSwapTimes` needs to look the real time up. That is
`Dated()`, and both the heal and the day series ask it. The heal runs once at
`Run` and stops when every row has a time.

## A swap's columns are its queryable projection, and all-time volume is a SUM

`swaps` is `(id, data JSON, timestamp, pool, amountUSD)`. The three scalars are
copies of fields inside `data`, promoted so the database can answer questions Go
would otherwise answer by reading the table into memory: a window (`timestamp`),
a pool's own history (`pool`), and what the protocol has traded (`amountUSD`).
`SeedSwap` and `ValueSwaps` write both homes; `Store.Traded()` is the only reader
of the sum. 🪤 There is no `json_extract` escape hatch — this driver's
amalgamation is built without JSON1, which is why the column had to exist at all.

A database written before the column gets it in `Init`, filled once from the rows
themselves; the ALTER failing is how a later start knows not to do it again.
Arriving empty would have zeroed every trade older than the current window — the
fix erasing the number it exists to correct.

**Total volume is not the pass's total.** `maxValuedSwaps` bounds what one
valuation pass values, because the swap table grows forever. Summing that window
answers for the newest trades only. Summing the day series does not escape it
either: those rows are folded from the same window, so their sum IS the window —
a fix that looked right and changed nothing. `Factory.txCount` is a separate
question (every interaction, mints and burns included) and stays with the
handlers that see them.

**A pass reads BOTH ENDS of what it has not seen.** The head
(`RecentSwapsRaw`) keeps arriving trades priced. The tail (`SwapsAfter`) walks
forward from `PricedThrough`, a mark in `meta`, so a table longer than any window
is covered a batch at a time and then never asked about again. It is a mark and
not a "which rows are still zero" query on purpose: a trade whose tokens reach no
stablecoin can NEVER be priced, and that query would hand back the same
unpriceable trades every pass forever. Having looked is monotone; having valued
is not. The mark moves only after `ValueSwaps` returns without error.

`wholeDays` drops the day each read's own cut falls in, because a day row is
REPLACED by the pass that writes it and half a day would land where a whole one
was. A read that came back short of its limit was not cut and keeps everything —
that condition is the whole test; without it the pass discards its oldest real
day on every chain small enough to fit in one window.

## The day series (series.go) is a SECOND FOLD over the valuation pass's trades

`tokenDayDatas` / `poolDayDatas` / `uniswapDayDatas` are rolled out of the swaps
already on disk, which is why history appears the moment a build ships instead
of starting that day. `valuedTrades` reads and values the swap window ONCE;
`valueSwaps` folds it into running totals and `writeSeries` folds the same
stream into days. Do not add a second read or a second pricing path —
valuation.go owns pricing (see its header).

- **What is exact.** A pool's candle is its own execution ratio `amount0/amount1`.
  A token's is that ratio carried to USD through the OTHER leg, so it is exact
  against a stablecoin and exact in shape against a volatile one. Pricing a
  token off its OWN leg yields a flat line at today's price — that mutation is
  covered by a test.
- **What is not.** Value locked is not in the trades. Each day records it while
  it is today; a day that passed before the series existed reports `""`, never
  `"0.00"` (`fmtHeld`) — zero would draw a cliff to no liquidity.
- **Write only what moved.** A pass recomputes the whole history every 30s;
  `idx.written` remembers each cell as persisted so the steady state is one
  write per subject that traded. Rewriting everything is the O(rows) storm that
  once stopped this pass from finishing.
- Rows are entities (`storage.TokenDay` / `PoolDay` / `FactoryDay`) built from
  the `engine` structs, so the json tags ARE the wire. `Store.Entities` reads a
  whole type then filters/orders/limits — a SQL LIMIT before the filter would
  answer with the first N rows rather than the first N matches.
- **The client draws nothing under three points**; fewer renders "Missing chart
  data", the same as an error. `pairDayDatas` and the hour series stay stubs —
  no client asks for them.

`where` learned two things a chart needs: a filter naming a reference
(`{token: "0x…"}`) compares against the referenced entity's `id`, and an exact
text match ignores case, because an address arrives checksummed as often as
lower-cased.

## DEX indexing has TWO orthogonal sources (EVM 0x9999 logs + native D-Chain CLOB)

The `dex` subgraph (markets/fills/orders/orderbook) can be fed from either or both of:

1. **EVM 0x9999 settlement logs** — the cross-chain SETTLEMENT view. `handleDEXFill`/
   `handleInitializeV4` derive Market/Fill from `DEXFill`/`Initialize` logs emitted by
   the 0x9999 PoolManager on an EVM chain (C-Chain). Source = `indexer.go` eth_getLogs.
2. **Native D-Chain CLOB read RPC** — the TRADING view. On a native-DEX deployment the
   trade engine IS the D-Chain (dexvm): a trade is a consensus state transition at
   `Block.Verify`, NOT an EVM event, so it NEVER appears on the EVM RPC. `CLOBSource`
   (`indexer/clob.go`) polls the committed-state read RPC
   `…/v1/bc/<D>/dex/clob_get_{markets,trades,orders}` (GET/JSON, READ-ONLY, zero
   consensus impact — see `luxfi/dex pkg/dchain/read.go`) and writes the SAME
   Market/Fill/Order entities the resolvers serve. Enabled by `Config.DexRPC` (graph)
   / `dex_rpc:` per-chain (explorer `chains.yaml`); empty = source 1 only.

The two sources are ORTHOGONAL — distinct loop, distinct protocol, distinct endpoint;
they share only the store (the sink) and the entity shapes (the contract). Running the
CLOB source NEVER regresses AMM/EVM indexing (the `amm` subgraph never gets `dex_rpc`).
Both Markets key by `poolID`, so when both run they converge on one Market per pool:
`writeMarket` refreshes the live CLOB book summary and MERGES through any EVM-accrued
`volume24h`/`tradeCount`/`lastPrice` it does not own.

Markets serve on the same host under distinct slugs (`.../cchain/amm/graphql`,
`.../cchain/dex/graphql`).

### Native CLOB source notes (`clob.go`)
- **Fills are a GLOBAL feed.** The 80-byte on-chain trade row carries no poolID (frozen
  GPU ABI), so `clob_get_trades` is a global height-ordered log. `writeTrade` records
  every fill as a `Fill` (the trade-history view) keyed `height:seq` (deterministic →
  idempotent re-read). Per-market figures (open orders, depth, best bid/ask, remaining)
  come from `clob_get_markets`' authoritative per-market book summary — we do NOT
  fabricate a per-market volume by mis-attributing the global log.
- **Incremental.** `drainTrades` pages by `since=lastHeight+1`; a steady chain costs one
  small query per tick. **Symbol decode:** a bound market's hex symbol (`4c55…` =
  ASCII `LUX/LUSD`) → human pair; an unbound poolID stays hex.

## Native DEX indexing (0x9999) — the EVM-settlement source detail

- **Single Fill source.** A settled fill is recorded ONLY from the authoritative
  `DEXFill` event in `handleDEXFill`. `handleSwapV4` is the AMM-side view and does NOT
  write a Fill. Do not add a second fill source.
- **Emitter-scoped.** `handleDEXFill` drops any `DEXFill` not emitted by the configured
  `PoolManager` (0x9999) — `!isPoolManager(l.Address)` returns early. This is the
  anti-spoof control; keep it.
- **Topic0 hashes** (`indexer/events.go`) are keccak256 of the EXACT signatures the
  `luxfi/precompile` emits (see `~/work/lux/precompile/dex/events.go`). The Lux `Swap`
  carries a trailing `uint24 fee` — NOT the upstream Uniswap-v4 signature. `DEXFill` =
  `keccak256("DEXFill(bytes32,address,uint256,uint256)")`. If you change a signature in
  the precompile, recompute the hash here or it matches nothing.
- **Re-genesis self-heal** rewinds the cursor only on a confirmed re-genesis (changed
  genesis-block hash) after a hysteresis streak — never on a transient low-head blip.

## Deployment topology — TWO exchange-api images (do not conflate)

- **`ghcr.io/luxfi/graph`** (this repo, `Dockerfile` → `graphd`) backs the explorer.
  Built by `.github/workflows/docker.yml` on `v*` tags, self-hosted ARC pool.
- **`ghcr.io/luxfi/exchange-api`** has TWO independent source/version lines:
  - **`v2.2.x`** ← repo `luxfi/exchange-api` (TypeScript, `/v1/graphql` with amm/dex
    routing + `DEX_SUBGRAPH_URL`). Deployed in **lux-explorer**. This is the markets API.
  - **`v5.x`** ← repo `luxfi/exchange` `Dockerfile.api` (`server.js`, REST
    `/v1/tokens|pools|trades|price` + `/subgraph/*`). Deployed in **lux-ns**.
  These are NOT the same codebase. A change to one does not reach the other's image.

## Sibling repos

See the org-level `LLM.md` at `/Users/a/work/lux/luxfi/LLM.md` for the full inventory of sibling repos and inter-repo dependencies.
