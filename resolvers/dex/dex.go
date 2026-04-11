// Package dex provides resolvers for the native Lux DEX (D-Chain).
//
// The native DEX is a CLOB (central limit order book) with perpetuals,
// NOT an AMM. This indexes DexVM state via RPC.
//
// Entities: Order, Fill, Market, Position, FundingRate, Liquidation
package dex

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

// ResolverFunc matches engine.ResolverFunc.
type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["order"] = resolveOrder
	resolvers["orders"] = resolveOrders
	resolvers["fill"] = resolveFill
	resolvers["fills"] = resolveFills
	resolvers["market"] = resolveMarket
	resolvers["markets"] = resolveMarkets
	resolvers["perpPosition"] = resolvePerpPosition
	resolvers["perpPositions"] = resolvePerpPositions
	resolvers["fundingRate"] = resolveFundingRate
	resolvers["fundingRates"] = resolveFundingRates
	resolvers["liquidation"] = resolveLiquidation
	resolvers["liquidations"] = resolveLiquidations
	resolvers["orderbook"] = resolveOrderbook
	resolvers["marketDayDatas"] = resolveMarketDayDatas
}

func resolveOrder(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Order", fmt.Sprint(id)) }
	return nil, fmt.Errorf("order requires id")
}
func resolveOrders(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Order", parseLimit(args))
}
func resolveFill(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Fill", fmt.Sprint(id)) }
	return nil, fmt.Errorf("fill requires id")
}
func resolveFills(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Fill", parseLimit(args))
}
func resolveMarket(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Market", fmt.Sprint(id)) }
	return nil, fmt.Errorf("market requires id")
}
func resolveMarkets(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Market", parseLimit(args))
}
func resolvePerpPosition(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("PerpPosition", fmt.Sprint(id)) }
	return nil, fmt.Errorf("perpPosition requires id")
}
func resolvePerpPositions(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("PerpPosition", parseLimit(args))
}
func resolveFundingRate(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("FundingRate", fmt.Sprint(id)) }
	return nil, fmt.Errorf("fundingRate requires id")
}
func resolveFundingRates(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("FundingRate", parseLimit(args))
}
func resolveLiquidation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Liquidation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("liquidation requires id")
}
func resolveLiquidations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Liquidation", parseLimit(args))
}
func resolveOrderbook(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Orderbook", fmt.Sprint(id)) }
	return nil, fmt.Errorf("orderbook requires market id")
}
func resolveMarketDayDatas(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("MarketDayData", parseLimit(args))
}

func parseLimit(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
