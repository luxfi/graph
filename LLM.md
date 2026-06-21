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

## Native DEX indexing (0x9999)

`graphd` indexes the native CLOB settlement precompile at `0x...9999` into a `dex`
subgraph (markets/fills/orders/orderbook) alongside the `amm` subgraph (Uniswap
v2/v3-shaped pools/pairs/swaps/tokens/factories). The two subgraphs serve on the same
host under distinct slugs (`.../cchain/amm/graphql`, `.../cchain/dex/graphql`).

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
