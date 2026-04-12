// Package treasury provides resolvers for on-chain treasury management.
//
// Indexes: deposits, withdrawals, allocations, budgets.
//
// Entities: TreasuryDeposit, TreasuryWithdrawal, TreasuryAllocation, TreasuryBudget, TreasuryStats
package treasury

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["treasuryDeposit"] = resolveTreasuryDeposit
	resolvers["treasuryDeposits"] = resolveTreasuryDeposits
	resolvers["treasuryWithdrawal"] = resolveTreasuryWithdrawal
	resolvers["treasuryWithdrawals"] = resolveTreasuryWithdrawals
	resolvers["treasuryAllocation"] = resolveTreasuryAllocation
	resolvers["treasuryAllocations"] = resolveTreasuryAllocations
	resolvers["treasuryBudget"] = resolveTreasuryBudget
	resolvers["treasuryBudgets"] = resolveTreasuryBudgets
	resolvers["treasuryStats"] = resolveTreasuryStats
}

func resolveTreasuryDeposit(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("TreasuryDeposit", fmt.Sprint(id)) }
	return nil, fmt.Errorf("treasuryDeposit requires id")
}
func resolveTreasuryDeposits(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("TreasuryDeposit", pl(args))
}
func resolveTreasuryWithdrawal(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("TreasuryWithdrawal", fmt.Sprint(id)) }
	return nil, fmt.Errorf("treasuryWithdrawal requires id")
}
func resolveTreasuryWithdrawals(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("TreasuryWithdrawal", pl(args))
}
func resolveTreasuryAllocation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("TreasuryAllocation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("treasuryAllocation requires id")
}
func resolveTreasuryAllocations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("TreasuryAllocation", pl(args))
}
func resolveTreasuryBudget(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("TreasuryBudget", fmt.Sprint(id)) }
	return nil, fmt.Errorf("treasuryBudget requires id")
}
func resolveTreasuryBudgets(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("TreasuryBudget", pl(args))
}
func resolveTreasuryStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("TreasuryStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
