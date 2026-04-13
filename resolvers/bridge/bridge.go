// Package bridge provides resolvers for the B-Chain (BridgeVM).
//
// Indexes: cross-chain transfers, MPC signatures, bridge requests, wrapped assets.
//
// Entities: BridgeTransfer, MPCSignature, WrappedAsset, BridgeRequest
package bridge

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["bridgeTransfer"] = resolveBridgeTransfer
	resolvers["bridgeTransfers"] = resolveBridgeTransfers
	resolvers["mpcSignature"] = resolveMPCSignature
	resolvers["mpcSignatures"] = resolveMPCSignatures
	resolvers["wrappedAsset"] = resolveWrappedAsset
	resolvers["wrappedAssets"] = resolveWrappedAssets
	resolvers["bridgeRequest"] = resolveBridgeRequest
	resolvers["bridgeRequests"] = resolveBridgeRequests
	resolvers["bridgeStats"] = resolveBridgeStats
}

func resolveBridgeTransfer(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("BridgeTransfer", fmt.Sprint(id)) }
	return nil, fmt.Errorf("bridgeTransfer requires id")
}
func resolveBridgeTransfers(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("BridgeTransfer", pl(args))
}
func resolveMPCSignature(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("MPCSignature", fmt.Sprint(id)) }
	return nil, fmt.Errorf("mpcSignature requires id")
}
func resolveMPCSignatures(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("MPCSignature", pl(args))
}
func resolveWrappedAsset(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("WrappedAsset", fmt.Sprint(id)) }
	return nil, fmt.Errorf("wrappedAsset requires id")
}
func resolveWrappedAssets(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("WrappedAsset", pl(args))
}
func resolveBridgeRequest(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("BridgeRequest", fmt.Sprint(id)) }
	return nil, fmt.Errorf("bridgeRequest requires id")
}
func resolveBridgeRequests(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("BridgeRequest", pl(args))
}
func resolveBridgeStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("BridgeStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return min(limit, 1000)
}
