package engine

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

// AMM resolvers — Uniswap v2/v3 subgraph compatible.
// These match The Graph's uniswap-v2 and uniswap-v3 subgraph schemas
// so existing frontend queries work without modification.
//
// "AMM" = automated market maker (v2 constant product, v3 concentrated liquidity).
// "DEX" in this codebase refers to the native ~/work/lux/dex orderbook.

func (e *Engine) registerAMMResolvers() {
	// v2 + v3 unified
	e.resolvers["factory"] = e.resolveFactory
	e.resolvers["factories"] = e.resolveFactories
	e.resolvers["bundle"] = e.resolveBundle
	e.resolvers["bundles"] = e.resolveBundles
	e.resolvers["pool"] = e.resolvePool
	e.resolvers["pools"] = e.resolvePools
	e.resolvers["pair"] = e.resolvePair       // v2
	e.resolvers["pairs"] = e.resolvePairs     // v2
	e.resolvers["swap"] = e.resolveSwap
	e.resolvers["swaps"] = e.resolveSwaps
	e.resolvers["mint"] = e.resolveMint
	e.resolvers["mints"] = e.resolveMints
	e.resolvers["burn"] = e.resolveBurn
	e.resolvers["burns"] = e.resolveBurns
	e.resolvers["tick"] = e.resolveTick       // v3
	e.resolvers["ticks"] = e.resolveTicks     // v3
	e.resolvers["position"] = e.resolvePosition     // v3
	e.resolvers["positions"] = e.resolvePositions   // v3

	// Collect / Flash (V3)
	e.resolvers["collect"] = e.resolveCollect
	e.resolvers["collects"] = e.resolveCollects
	e.resolvers["flash"] = e.resolveFlash
	e.resolvers["flashes"] = e.resolveFlashes

	// Time series
	e.resolvers["tokenDayDatas"] = e.resolveTokenDayDatas
	e.resolvers["pairDayDatas"] = e.resolvePairDayDatas
	e.resolvers["poolDayDatas"] = e.resolvePoolDayDatas
	e.resolvers["pairHourDatas"] = e.resolvePairHourDatas
	e.resolvers["uniswapDayDatas"] = e.resolveFactoryDayDatas // v2 compat name
}

func (e *Engine) registerERC20Resolvers() {
	e.resolvers["token"] = e.resolveToken
	e.resolvers["tokens"] = e.resolveTokens
	e.resolvers["transfer"] = e.resolveTransfer
	e.resolvers["transfers"] = e.resolveTransfers
}

func (e *Engine) registerERC721Resolvers() {
	e.resolvers["nft"] = e.resolveNFT
	e.resolvers["nfts"] = e.resolveNFTs
}

// Factory / Pool / Pair resolvers

func (e *Engine) resolveFactory(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	id := "1" // default factory
	if v, ok := args["id"]; ok {
		id = fmt.Sprint(v)
	}
	return s.GetFactory(ctx, id)
}

func (e *Engine) resolveFactories(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.GetFactories(ctx)
}

func (e *Engine) resolveBundle(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.GetBundle(ctx, "1")
}

func (e *Engine) resolveBundles(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	b, err := s.GetBundle(ctx, "1")
	if err != nil {
		return nil, err
	}
	return []interface{}{b}, nil
}

func (e *Engine) resolvePool(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetPool(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("pool requires id")
}

func (e *Engine) resolvePools(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetPools(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolvePair(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetPair(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("pair requires id")
}

func (e *Engine) resolvePairs(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetPairs(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolveSwap(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetSwap(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("swap requires id")
}

func (e *Engine) resolveSwaps(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetSwaps(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolveMint(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetMint(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("mint requires id")
}

func (e *Engine) resolveMints(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetMints(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolveBurn(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetBurn(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("burn requires id")
}

func (e *Engine) resolveBurns(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetBurns(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolveTick(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetTick(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("tick requires id")
}

func (e *Engine) resolveTicks(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetTicks(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolvePosition(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetPosition(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("position requires id")
}

func (e *Engine) resolvePositions(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetPositions(ctx, limit, orderBy, orderDir, where)
}

// Time series

func (e *Engine) resolveTokenDayDatas(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 30)
	return s.GetTokenDayDatas(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolvePairDayDatas(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 30)
	return s.GetPairDayDatas(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolvePoolDayDatas(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 30)
	return s.GetPoolDayDatas(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolveFactoryDayDatas(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 30)
	return s.GetFactoryDayDatas(ctx, limit, orderBy, orderDir, where)
}

// ERC transfers / NFTs

func (e *Engine) resolveTransfer(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetTransfer(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("transfer requires id")
}

func (e *Engine) resolveTransfers(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetTransfers(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolveNFT(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetNFT(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("nft requires id")
}

func (e *Engine) resolveNFTs(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetNFTs(ctx, limit, orderBy, orderDir, where)
}

// Collect / Flash resolvers

func (e *Engine) resolveCollect(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetCollect(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("collect requires id")
}

func (e *Engine) resolveCollects(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetCollects(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolveFlash(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetFlash(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("flash requires id")
}

func (e *Engine) resolveFlashes(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetFlashes(ctx, limit, orderBy, orderDir, where)
}

func (e *Engine) resolvePairHourDatas(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 24)
	return s.GetPairHourDatas(ctx, limit, orderBy, orderDir, where)
}

// parseListArgs extracts first/orderBy/orderDirection/where from GraphQL args.
func parseListArgs(args map[string]interface{}, defaultLimit int) (int, string, string, map[string]interface{}) {
	limit := defaultLimit
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	orderBy := ""
	if o, ok := args["orderBy"]; ok {
		orderBy = fmt.Sprint(o)
	}
	orderDir := ""
	if d, ok := args["orderDirection"]; ok {
		orderDir = fmt.Sprint(d)
	}
	var where map[string]interface{}
	if w, ok := args["where"]; ok {
		if wm, ok := w.(map[string]interface{}); ok {
			where = wm
		}
	}
	return limit, orderBy, orderDir, where
}
