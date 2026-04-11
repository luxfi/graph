// Package platform provides resolvers for the P-Chain (PlatformVM).
//
// Indexes: validators, delegators, staking, subnets, L1 creation, rewards.
//
// Entities: Validator, Delegator, Subnet, Blockchain, StakingReward, StakingPeriod
package platform

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["validator"] = resolveValidator
	resolvers["validators"] = resolveValidators
	resolvers["delegator"] = resolveDelegator
	resolvers["delegators"] = resolveDelegators
	resolvers["subnet"] = resolveSubnet
	resolvers["subnets"] = resolveSubnets
	resolvers["blockchain"] = resolveBlockchain
	resolvers["blockchains"] = resolveBlockchains
	resolvers["stakingReward"] = resolveStakingReward
	resolvers["stakingRewards"] = resolveStakingRewards
	resolvers["networkStats"] = resolveNetworkStats
}

func resolveValidator(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Validator", fmt.Sprint(id)) }
	return nil, fmt.Errorf("validator requires id (NodeID)")
}
func resolveValidators(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Validator", parseLimit(args))
}
func resolveDelegator(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Delegator", fmt.Sprint(id)) }
	return nil, fmt.Errorf("delegator requires id")
}
func resolveDelegators(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Delegator", parseLimit(args))
}
func resolveSubnet(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Subnet", fmt.Sprint(id)) }
	return nil, fmt.Errorf("subnet requires id")
}
func resolveSubnets(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Subnet", parseLimit(args))
}
func resolveBlockchain(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Blockchain", fmt.Sprint(id)) }
	return nil, fmt.Errorf("blockchain requires id")
}
func resolveBlockchains(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Blockchain", parseLimit(args))
}
func resolveStakingReward(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("StakingReward", fmt.Sprint(id)) }
	return nil, fmt.Errorf("stakingReward requires id")
}
func resolveStakingRewards(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("StakingReward", parseLimit(args))
}
func resolveNetworkStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("NetworkStats", "1")
}

func parseLimit(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
