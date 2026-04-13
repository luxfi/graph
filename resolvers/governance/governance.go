// Package governance provides resolvers for on-chain governance.
//
// Indexes: proposals, votes, delegations, governance parameters.
//
// Entities: Proposal, Vote, Delegation, GovernanceStats
package governance

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["proposal"] = resolveProposal
	resolvers["proposals"] = resolveProposals
	resolvers["vote"] = resolveVote
	resolvers["votes"] = resolveVotes
	resolvers["delegation"] = resolveDelegation
	resolvers["delegations"] = resolveDelegations
	resolvers["governanceStats"] = resolveGovernanceStats
}

func resolveProposal(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Proposal", fmt.Sprint(id)) }
	return nil, fmt.Errorf("proposal requires id")
}
func resolveProposals(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Proposal", pl(args))
}
func resolveVote(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Vote", fmt.Sprint(id)) }
	return nil, fmt.Errorf("vote requires id")
}
func resolveVotes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Vote", pl(args))
}
func resolveDelegation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Delegation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("delegation requires id")
}
func resolveDelegations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Delegation", pl(args))
}
func resolveGovernanceStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("GovernanceStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return min(limit, 1000)
}
