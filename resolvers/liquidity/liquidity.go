// Package liquidity provides resolvers for the Liquidity Protocol (omnichain DeFi).
//
// Indexes: cross-chain swaps, limit orders, arbitrage, OMA router, oracle prices,
// strategy orders, market making, bridge operations.
//
// Entities: CrossChainSwap, LimitOrder, Arbitrage, OMAPool, OMASwap, PriceUpdate,
// StrategyOrder, MarketMaker, BridgeTransfer, LiquidityStats
package liquidity

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["crossChainSwap"] = resolveSingle("CrossChainSwap")
	resolvers["crossChainSwaps"] = resolveList("CrossChainSwap")
	resolvers["limitOrder"] = resolveSingle("LimitOrder")
	resolvers["limitOrders"] = resolveList("LimitOrder")
	resolvers["arbitrage"] = resolveSingle("Arbitrage")
	resolvers["arbitrages"] = resolveList("Arbitrage")
	resolvers["omaPool"] = resolveSingle("OMAPool")
	resolvers["omaPools"] = resolveList("OMAPool")
	resolvers["omaSwap"] = resolveSingle("OMASwap")
	resolvers["omaSwaps"] = resolveList("OMASwap")
	resolvers["priceUpdate"] = resolveSingle("PriceUpdate")
	resolvers["priceUpdates"] = resolveList("PriceUpdate")
	resolvers["strategyOrder"] = resolveSingle("StrategyOrder")
	resolvers["strategyOrders"] = resolveList("StrategyOrder")
	resolvers["marketMaker"] = resolveSingle("MarketMaker")
	resolvers["marketMakers"] = resolveList("MarketMaker")
	resolvers["bridgeTransfer"] = resolveSingle("BridgeTransfer")
	resolvers["bridgeTransfers"] = resolveList("BridgeTransfer")
	resolvers["liquidityStats"] = resolveStats
}

func resolveSingle(typeName string) ResolverFunc {
	return func(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
		if id, ok := args["id"]; ok {
			return s.GetByType(typeName, fmt.Sprint(id))
		}
		return nil, fmt.Errorf("%s requires id", typeName)
	}
}

func resolveList(typeName string) ResolverFunc {
	return func(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
		return s.ListByType(typeName, pl(args))
	}
}

func resolveStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("LiquidityStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return min(limit, 1000)
}
