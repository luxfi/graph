// Package liquidprotocol provides resolvers for the Liquid Protocol (liquid staking + wrapped assets).
//
// Indexes: fees, validator rewards, slashing, teleporter deposits/yields/withdrawals,
// vault strategies, backing attestations.
//
// Entities: LiquidFee, ValidatorReward, SlashingEvent, DepositMint, YieldMint,
// WithdrawBurn, BackingUpdate, MPCOracle, VaultStrategy, YieldHarvest, LiquidProtocolStats
package liquidprotocol

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["liquidFee"] = resolveSingle("LiquidFee")
	resolvers["liquidFees"] = resolveList("LiquidFee")
	resolvers["validatorReward"] = resolveSingle("ValidatorReward")
	resolvers["validatorRewards"] = resolveList("ValidatorReward")
	resolvers["slashingEvent"] = resolveSingle("SlashingEvent")
	resolvers["slashingEvents"] = resolveList("SlashingEvent")
	resolvers["depositMint"] = resolveSingle("DepositMint")
	resolvers["depositMints"] = resolveList("DepositMint")
	resolvers["yieldMint"] = resolveSingle("YieldMint")
	resolvers["yieldMints"] = resolveList("YieldMint")
	resolvers["withdrawBurn"] = resolveSingle("WithdrawBurn")
	resolvers["withdrawBurns"] = resolveList("WithdrawBurn")
	resolvers["backingUpdate"] = resolveSingle("BackingUpdate")
	resolvers["backingUpdates"] = resolveList("BackingUpdate")
	resolvers["vaultStrategy"] = resolveSingle("VaultStrategy")
	resolvers["vaultStrategies"] = resolveList("VaultStrategy")
	resolvers["yieldHarvest"] = resolveSingle("YieldHarvest")
	resolvers["yieldHarvests"] = resolveList("YieldHarvest")
	resolvers["liquidProtocolStats"] = resolveStats
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
	return s.GetByType("LiquidProtocolStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return limit
}
