// Package prediction provides resolvers for prediction markets.
//
// Indexes: markets, positions, resolutions.
//
// Entities: PredictionMarket, PredictionPosition, PredictionResolution, PredictionStats
package prediction

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["predictionMarket"] = resolvePredictionMarket
	resolvers["predictionMarkets"] = resolvePredictionMarkets
	resolvers["predictionPosition"] = resolvePredictionPosition
	resolvers["predictionPositions"] = resolvePredictionPositions
	resolvers["predictionResolution"] = resolvePredictionResolution
	resolvers["predictionResolutions"] = resolvePredictionResolutions
	resolvers["predictionStats"] = resolvePredictionStats
}

func resolvePredictionMarket(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("PredictionMarket", fmt.Sprint(id)) }
	return nil, fmt.Errorf("predictionMarket requires id")
}
func resolvePredictionMarkets(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("PredictionMarket", pl(args))
}
func resolvePredictionPosition(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("PredictionPosition", fmt.Sprint(id)) }
	return nil, fmt.Errorf("predictionPosition requires id")
}
func resolvePredictionPositions(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("PredictionPosition", pl(args))
}
func resolvePredictionResolution(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("PredictionResolution", fmt.Sprint(id)) }
	return nil, fmt.Errorf("predictionResolution requires id")
}
func resolvePredictionResolutions(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("PredictionResolution", pl(args))
}
func resolvePredictionStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("PredictionStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
