// Package liquid provides resolvers for liquid staking.
//
// Indexes: stakes, unstakes, rewards, validators.
//
// Entities: LiquidStake, LiquidUnstake, LiquidReward, LiquidValidator, LiquidStats
package liquid

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["liquidStake"] = resolveLiquidStake
	resolvers["liquidStakes"] = resolveLiquidStakes
	resolvers["liquidUnstake"] = resolveLiquidUnstake
	resolvers["liquidUnstakes"] = resolveLiquidUnstakes
	resolvers["liquidReward"] = resolveLiquidReward
	resolvers["liquidRewards"] = resolveLiquidRewards
	resolvers["liquidValidator"] = resolveLiquidValidator
	resolvers["liquidValidators"] = resolveLiquidValidators
	resolvers["liquidStats"] = resolveLiquidStats
}

func resolveLiquidStake(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("LiquidStake", fmt.Sprint(id)) }
	return nil, fmt.Errorf("liquidStake requires id")
}
func resolveLiquidStakes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("LiquidStake", pl(args))
}
func resolveLiquidUnstake(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("LiquidUnstake", fmt.Sprint(id)) }
	return nil, fmt.Errorf("liquidUnstake requires id")
}
func resolveLiquidUnstakes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("LiquidUnstake", pl(args))
}
func resolveLiquidReward(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("LiquidReward", fmt.Sprint(id)) }
	return nil, fmt.Errorf("liquidReward requires id")
}
func resolveLiquidRewards(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("LiquidReward", pl(args))
}
func resolveLiquidValidator(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("LiquidValidator", fmt.Sprint(id)) }
	return nil, fmt.Errorf("liquidValidator requires id")
}
func resolveLiquidValidators(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("LiquidValidator", pl(args))
}
func resolveLiquidStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("LiquidStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return min(limit, 1000)
}
