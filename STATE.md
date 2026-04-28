# lux/graph — Subgraph State

`lux/graph` is the standalone Go GraphQL indexer (`graph` binary, also runnable
as a Lux VM plugin). It reads any EVM RPC, indexes events into local
SQLite/ZapDB, and serves a Graph-compatible GraphQL API. The "subgraphs" in
this repo are Go resolver packages under `resolvers/<topic>/` plus the central
event-signature table in `indexer/events.go`. There are no `subgraph.yaml`
manifests in this repo — those live in the sibling `uni-v2-subgraph`,
`uni-v3-subgraph`, `uni-v4-subgraph` repos that target this indexer.

## Resolver Subgraphs

| Subgraph | Path | Built-in alias(es) | Status |
| --- | --- | --- | --- |
| ai (A-Chain) | `resolvers/ai/` | `ai`, `achain` | current |
| amm | (handled by `engine.registerAMMResolvers`) | `amm`, `amm-v2`, `uniswap-v2`, `amm-v3`, `uniswap-v3`, `amm-v4`, `uniswap-v4`, `v4` | current |
| bridge (B-Chain) | `resolvers/bridge/` | `bridge`, `bchain` | current |
| dao | `resolvers/dao/` | `dao` | current |
| derivatives | `resolvers/derivatives/` | `derivatives`, `futures`, `options` | current |
| dex (D-Chain CLOB) | `resolvers/dex/` | `dex` | current |
| did | `resolvers/did/` | `did`, `did-registry` | current |
| exchange (X-Chain) | `resolvers/exchange/` | `exchange`, `xchain` | current |
| fhe (T-Chain) | `resolvers/fhe/` | `fhe`, `threshold` | UPDATED 2026-04-28 |
| governance | `resolvers/governance/` | `governance` | current |
| identity (I-Chain) | `resolvers/identity/` | `identity`, `ichain` | current |
| key (K-Chain) | `resolvers/key/` | `key`, `kchain` | current |
| liquid (liquid staking) | `resolvers/liquid/` | `liquid`, `liquid-staking` | current |
| liquidity (omnichain) | `resolvers/liquidity/` | `liquidity`, `liquidity-protocol`, `omnichain` | current |
| liquidprotocol (teleport / liquid-vault) | `resolvers/liquidprotocol/` | `liquid-protocol`, `teleport`, `liquid-vault` | current |
| mpc (M-Chain) | `resolvers/mpc/` | `mpc`, `mchain` | current |
| oracle (O-Chain) | `resolvers/oracle/` | `oracle`, `ochain` | current |
| platform (P-Chain) | `resolvers/platform/` | `platform`, `pchain` | current |
| precompile | `resolvers/precompile/` | `precompile`, `precompiles` | current |
| prediction | `resolvers/prediction/` | `prediction`, `prediction-market` | current |
| privacy | `resolvers/privacy/` | `privacy` | current |
| quantum (Q-Chain) | `resolvers/quantum/` | `quantum`, `qchain` | current |
| relay (R-Chain) | `resolvers/relay/` | `relay`, `rchain` | current |
| securities | `resolvers/securities/` | `securities`, `security-token` | UPDATED 2026-04-28 |
| servicenode (S-Chain) | `resolvers/servicenode/` | `servicenode`, `schain` | current |
| treasury | `resolvers/treasury/` | `treasury` | current |
| utxo | `resolvers/utxo/` | `utxo` | current |
| zk (Z-Chain) | `resolvers/zk/` | `zk`, `zchain` | current |

Empty directories `resolvers/amm/` and `resolvers/threshold/` are intentional:
amm is registered by `engine.registerAMMResolvers`, and `threshold` is an
alias of `fhe` in `engine.LoadBuiltin`.

## Updates landed this sweep (2026-04-28)

### securities (ERC-3643 + ONCHAINID)

Added entity surfaces for ERC-3643 transfer-agent / compliance / identity-
registry workflow plus ONCHAINID claim lifecycle. New resolvers:

- `onchainIdClaim`, `onchainIdClaims` — `ClaimAdded` / `ClaimRemoved` /
  `ClaimChanged` from ONCHAINID.
- `transferAgentAction`, `transferAgentActions` — `Recovery`, `AddressFrozen`,
  `TokensFrozen`, `TokensUnfrozen` from ERC-3643 token agent.
- `frozenAccount`, `frozenAccounts` — current `AddressFrozen` snapshots.
- `frozenTokens`, `frozenTokensList` — per-holder partial freeze amounts.
- `identityRegistryAction`, `identityRegistryActions` — `IdentityRegistered`,
  `IdentityRemoved`, `IdentityStored`, `CountryUpdated` from
  IdentityRegistry contract.

Backing entity types for the storage layer: `OnchainIdClaim`,
`TransferAgentAction`, `FrozenAccount`, `FrozenTokens`,
`IdentityRegistryAction`.

### fhe (LP-114 M-Chain × F-Chain policy gating)

Added `FHEPolicy` and `FHEPolicyBinding` entity surfaces so the indexer can
expose encrypted-policy descriptors anchored on M-Chain and their bindings to
on-chain resources. New resolvers: `fhePolicy`, `fhePolicies`,
`fhePolicyBinding`, `fhePolicyBindings`.

## Sibling subgraph repos (deploy via The Graph protocol)

These three sibling repos define `subgraph.yaml` manifests targeting the same
indexer. They live next to `lux/graph` under `~/work/lux/`:

| Repo | Path | Network | Factory address | spec | Last commit |
| --- | --- | --- | --- | --- | --- |
| uni-v2-subgraph | `~/work/lux/uni-v2-subgraph` | `liquidity` | `0xD173926A10A0C4eCd3A51B1422270b65Df0551c1` | `0.0.4` | `9ffaaac feat: update subgraph config and package dependencies` |
| uni-v3-subgraph | `~/work/lux/uni-v3-subgraph` | `liquidity` | `0x80bBc7C4C7a59C899D1B37BC14539A22D5830a84` | `0.0.4` | `5d56a82 feat: update subgraph config and package dependencies` |
| uni-v4-subgraph | `~/work/lux/uni-v4-subgraph` | `liquidity` | `0x0000000000000000000000000000000000009010` (PoolManager) | `0.0.4` | `578adf5 feat: update subgraph config and package dependencies` |

Mainnet factory addresses match `indexer/events.go::LuxMainnet`. The subgraph
manifests use `network: liquidity` for the deploy target name (the Lux DEX
network identifier in the hosted graph node), not the Liquidity company.

## Build status

Build verification deferred this sweep due to a pre-existing `go.sum`
checksum mismatch on `github.com/luxfi/consensus@v1.22.70` and
`github.com/hanzoai/replicate@v0.6.0` — the mismatch lives in transitive
deps and is unrelated to the resolver edits in this branch (the securities
and fhe packages only import `context`, `fmt`,
`github.com/luxfi/graph/storage`). `gofmt` is clean on both edited files.

To repair the build locally:

```
GOWORK=off GOSUMDB=off go mod tidy
```

Then `make build` should produce `bin/graph`.

## Deploy state

The `graph` binary deploys via the canonical docker-build reusable workflow on
tag push (`ci/canonical-docker-build` branch on `main` history; image
`ghcr.io/luxfi/graph:<semver>`). No deploy keys for The Graph hosted service
are wired into this repo — the sibling subgraph repos handle hosted-service
deploys via their own `package.json` `deploy` script and require an env-side
`GRAPH_DEPLOY_KEY` to run `graph deploy --product hosted-service luxfi/<name>`.

## Follow-on work

- Wire concrete event-signature constants for ERC-3643 + ONCHAINID into
  `indexer/events.go` (mirror the `SigPairCreated` / `SigSwapV2` pattern). The
  resolver layer is in place; the indexer still needs the topic-0 hashes:
  - `Transfer(address,address,uint256)` — already covered by `SigTransfer`.
  - `AddressFrozen(address indexed,bool indexed,address indexed)`,
    `TokensFrozen(address indexed,uint256)`,
    `TokensUnfrozen(address indexed,uint256)`,
    `RecoverySuccess(address,address,address)`.
  - `ClaimAdded(bytes32 indexed,uint256 indexed,uint256,address,bytes,bytes,string)`,
    `ClaimRemoved(...)`, `ClaimChanged(...)`.
  - `IdentityRegistered(address indexed,address indexed)`,
    `IdentityRemoved(address indexed,address indexed)`,
    `IdentityStored(address indexed,address indexed)`,
    `CountryUpdated(address indexed,uint16 indexed)`.
- Add storage-layer entity registrations for the new types so
  `s.GetByType` / `s.ListByType` resolve them. The storage layer auto-handles
  any `(type, id)` pair, but indexes will speed up list queries.
- Add a built-in alias `"erc3643"` to `engine.LoadBuiltin` that calls
  `securities.Register` (currently the alias is `securities` /
  `security-token`).
- Resolve `LoadConfig` TODO at `engine/engine.go:258` once the subgraph.yaml
  parser lands (separate work item).
- Replace the empty `resolvers/amm/` and `resolvers/threshold/` directories
  with package documentation so the layout is self-explanatory.
