// Package relay provides resolvers for the R-Chain (RelayVM).
//
// Indexes: warp messages, relay requests, proofs, routes, receipts.
//
// Entities: WarpMessage, RelayRequest, RelayProof, RelayRoute, MessageReceipt
package relay

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["warpMessage"] = resolveWarpMessage
	resolvers["warpMessages"] = resolveWarpMessages
	resolvers["relayRequest"] = resolveRelayRequest
	resolvers["relayRequests"] = resolveRelayRequests
	resolvers["relayProof"] = resolveRelayProof
	resolvers["relayProofs"] = resolveRelayProofs
	resolvers["relayRoute"] = resolveRelayRoute
	resolvers["relayRoutes"] = resolveRelayRoutes
	resolvers["messageReceipt"] = resolveMessageReceipt
	resolvers["messageReceipts"] = resolveMessageReceipts
	resolvers["relayStats"] = resolveRelayStats
}

func resolveWarpMessage(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("WarpMessage", fmt.Sprint(id)) }
	return nil, fmt.Errorf("warpMessage requires id")
}
func resolveWarpMessages(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("WarpMessage", pl(args))
}
func resolveRelayRequest(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("RelayRequest", fmt.Sprint(id)) }
	return nil, fmt.Errorf("relayRequest requires id")
}
func resolveRelayRequests(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("RelayRequest", pl(args))
}
func resolveRelayProof(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("RelayProof", fmt.Sprint(id)) }
	return nil, fmt.Errorf("relayProof requires id")
}
func resolveRelayProofs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("RelayProof", pl(args))
}
func resolveRelayRoute(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("RelayRoute", fmt.Sprint(id)) }
	return nil, fmt.Errorf("relayRoute requires id")
}
func resolveRelayRoutes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("RelayRoute", pl(args))
}
func resolveMessageReceipt(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("MessageReceipt", fmt.Sprint(id)) }
	return nil, fmt.Errorf("messageReceipt requires id")
}
func resolveMessageReceipts(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("MessageReceipt", pl(args))
}
func resolveRelayStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("RelayStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return min(limit, 1000)
}
