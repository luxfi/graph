package engine

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

// V4 resolvers — PoolManager singleton, ModifyLiquidity, Positions, Subscriptions.

func (e *Engine) registerV4Resolvers() {
	// PoolManager (V4 singleton — also aliased as "factory" for V3 compat)
	e.resolvers["poolManager"] = e.resolvePoolManager
	e.resolvers["poolManagers"] = e.resolvePoolManagers

	// ModifyLiquidity (replaces separate mint/burn in V4)
	e.resolvers["modifyLiquidity"] = e.resolveModifyLiquidity
	e.resolvers["modifyLiquiditys"] = e.resolveModifyLiquiditys // Graph uses this plural

	// Positions (V4 ERC721)
	e.resolvers["position"] = e.resolvePosition
	e.resolvers["positions"] = e.resolvePositions

	// Subscriptions (V4 hooks)
	e.resolvers["subscribe"] = e.resolveSubscribe
	e.resolvers["subscribes"] = e.resolveSubscribes
	e.resolvers["unsubscribe"] = e.resolveUnsubscribe
	e.resolvers["unsubscribes"] = e.resolveUnsubscribes

	// Hourly data
	e.resolvers["poolHourDatas"] = e.resolvePoolHourDatas
	e.resolvers["tokenHourDatas"] = e.resolveTokenHourDatas
	e.resolvers["uniswapDayDatas"] = e.resolveUniswapDayDatas
}

func (e *Engine) resolvePoolManager(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	id := "1"
	if v, ok := args["id"]; ok {
		id = fmt.Sprint(v)
	}
	return s.GetFactory(ctx, id) // PoolManager shares factory storage
}

func (e *Engine) resolvePoolManagers(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.GetFactories(ctx)
}

func (e *Engine) resolveModifyLiquidity(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetModifyLiquidity(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("modifyLiquidity requires id")
}

func (e *Engine) resolveModifyLiquiditys(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, _ := parseListArgs(args, 100)
	return s.GetModifyLiquiditys(ctx, limit, orderBy, orderDir)
}

func (e *Engine) resolveSubscribe(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetSubscribe(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("subscribe requires id")
}

func (e *Engine) resolveSubscribes(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, _ := parseListArgs(args, 100)
	return s.GetSubscribes(ctx, limit, orderBy, orderDir)
}

func (e *Engine) resolveUnsubscribe(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetUnsubscribe(ctx, fmt.Sprint(id))
	}
	return nil, fmt.Errorf("unsubscribe requires id")
}

func (e *Engine) resolveUnsubscribes(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, _ := parseListArgs(args, 100)
	return s.GetUnsubscribes(ctx, limit, orderBy, orderDir)
}

func (e *Engine) resolvePoolHourDatas(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, _ := parseListArgs(args, 24)
	return s.GetPoolHourDatas(ctx, limit, orderBy, orderDir)
}

func (e *Engine) resolveTokenHourDatas(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, _ := parseListArgs(args, 24)
	return s.GetTokenHourDatas(ctx, limit, orderBy, orderDir)
}

func (e *Engine) resolveUniswapDayDatas(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 30)
	return s.GetFactoryDayDatas(ctx, limit, orderBy, orderDir, where)
}
