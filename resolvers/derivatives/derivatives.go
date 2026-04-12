// Package derivatives provides resolvers for futures and options.
//
// Indexes: derivative contracts, settlements, option exercises.
//
// Entities: DerivativeContract, DerivativeSettlement, DerivativeExercise, DerivativeStats
package derivatives

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["derivativeContract"] = resolveDerivativeContract
	resolvers["derivativeContracts"] = resolveDerivativeContracts
	resolvers["derivativeSettlement"] = resolveDerivativeSettlement
	resolvers["derivativeSettlements"] = resolveDerivativeSettlements
	resolvers["derivativeExercise"] = resolveDerivativeExercise
	resolvers["derivativeExercises"] = resolveDerivativeExercises
	resolvers["derivativeStats"] = resolveDerivativeStats
}

func resolveDerivativeContract(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DerivativeContract", fmt.Sprint(id)) }
	return nil, fmt.Errorf("derivativeContract requires id")
}
func resolveDerivativeContracts(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DerivativeContract", pl(args))
}
func resolveDerivativeSettlement(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DerivativeSettlement", fmt.Sprint(id)) }
	return nil, fmt.Errorf("derivativeSettlement requires id")
}
func resolveDerivativeSettlements(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DerivativeSettlement", pl(args))
}
func resolveDerivativeExercise(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DerivativeExercise", fmt.Sprint(id)) }
	return nil, fmt.Errorf("derivativeExercise requires id")
}
func resolveDerivativeExercises(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DerivativeExercise", pl(args))
}
func resolveDerivativeStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("DerivativeStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
