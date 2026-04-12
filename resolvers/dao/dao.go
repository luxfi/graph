// Package dao provides resolvers for DAO operations.
//
// Indexes: DAO proposals, members, treasury actions.
//
// Entities: DAOProposal, DAOMember, DAOTreasury, DAOStats
package dao

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["daoProposal"] = resolveDAOProposal
	resolvers["daoProposals"] = resolveDAOProposals
	resolvers["daoMember"] = resolveDAOMember
	resolvers["daoMembers"] = resolveDAOMembers
	resolvers["daoTreasury"] = resolveDAOTreasury
	resolvers["daoTreasuries"] = resolveDAOTreasuries
	resolvers["daoStats"] = resolveDAOStats
}

func resolveDAOProposal(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DAOProposal", fmt.Sprint(id)) }
	return nil, fmt.Errorf("daoProposal requires id")
}
func resolveDAOProposals(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DAOProposal", pl(args))
}
func resolveDAOMember(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DAOMember", fmt.Sprint(id)) }
	return nil, fmt.Errorf("daoMember requires id")
}
func resolveDAOMembers(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DAOMember", pl(args))
}
func resolveDAOTreasury(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DAOTreasury", fmt.Sprint(id)) }
	return nil, fmt.Errorf("daoTreasury requires id")
}
func resolveDAOTreasuries(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DAOTreasury", pl(args))
}
func resolveDAOStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("DAOStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
