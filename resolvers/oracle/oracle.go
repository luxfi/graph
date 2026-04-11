// Package oracle provides resolvers for the O-Chain (OracleVM).
//
// Indexes: price feeds, data requests, oracle reports, node registrations.
//
// Entities: PriceFeed, DataRequest, OracleReport, OracleNode, DataAttestation
package oracle

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["priceFeed"] = resolvePriceFeed
	resolvers["priceFeeds"] = resolvePriceFeeds
	resolvers["dataRequest"] = resolveDataRequest
	resolvers["dataRequests"] = resolveDataRequests
	resolvers["oracleReport"] = resolveOracleReport
	resolvers["oracleReports"] = resolveOracleReports
	resolvers["oracleNode"] = resolveOracleNode
	resolvers["oracleNodes"] = resolveOracleNodes
	resolvers["dataAttestation"] = resolveDataAttestation
	resolvers["dataAttestations"] = resolveDataAttestations
	resolvers["oracleStats"] = resolveOracleStats
}

func resolvePriceFeed(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("PriceFeed", fmt.Sprint(id)) }
	return nil, fmt.Errorf("priceFeed requires id")
}
func resolvePriceFeeds(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("PriceFeed", pl(args))
}
func resolveDataRequest(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DataRequest", fmt.Sprint(id)) }
	return nil, fmt.Errorf("dataRequest requires id")
}
func resolveDataRequests(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DataRequest", pl(args))
}
func resolveOracleReport(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("OracleReport", fmt.Sprint(id)) }
	return nil, fmt.Errorf("oracleReport requires id")
}
func resolveOracleReports(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("OracleReport", pl(args))
}
func resolveOracleNode(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("OracleNode", fmt.Sprint(id)) }
	return nil, fmt.Errorf("oracleNode requires id")
}
func resolveOracleNodes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("OracleNode", pl(args))
}
func resolveDataAttestation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DataAttestation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("dataAttestation requires id")
}
func resolveDataAttestations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DataAttestation", pl(args))
}
func resolveOracleStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("OracleStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
