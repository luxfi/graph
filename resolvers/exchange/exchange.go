// Package exchange provides resolvers for the X-Chain (ExchangeVM).
//
// Indexes: UTXO assets, transfers, NFTs, asset creation, cross-chain imports/exports.
//
// Entities: Asset, UTXO, Transfer, AssetCreation, ImportTx, ExportTx
package exchange

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["asset"] = resolveAsset
	resolvers["assets"] = resolveAssets
	resolvers["utxo"] = resolveUTXO
	resolvers["utxos"] = resolveUTXOs
	resolvers["xTransfer"] = resolveXTransfer
	resolvers["xTransfers"] = resolveXTransfers
	resolvers["assetCreation"] = resolveAssetCreation
	resolvers["assetCreations"] = resolveAssetCreations
	resolvers["importTx"] = resolveImportTx
	resolvers["importTxs"] = resolveImportTxs
	resolvers["exportTx"] = resolveExportTx
	resolvers["exportTxs"] = resolveExportTxs
}

func resolveAsset(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Asset", fmt.Sprint(id)) }
	return nil, fmt.Errorf("asset requires id")
}
func resolveAssets(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Asset", pl(args))
}
func resolveUTXO(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("UTXO", fmt.Sprint(id)) }
	return nil, fmt.Errorf("utxo requires id")
}
func resolveUTXOs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("UTXO", pl(args))
}
func resolveXTransfer(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("XTransfer", fmt.Sprint(id)) }
	return nil, fmt.Errorf("xTransfer requires id")
}
func resolveXTransfers(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("XTransfer", pl(args))
}
func resolveAssetCreation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("AssetCreation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("assetCreation requires id")
}
func resolveAssetCreations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("AssetCreation", pl(args))
}
func resolveImportTx(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ImportTx", fmt.Sprint(id)) }
	return nil, fmt.Errorf("importTx requires id")
}
func resolveImportTxs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ImportTx", pl(args))
}
func resolveExportTx(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ExportTx", fmt.Sprint(id)) }
	return nil, fmt.Errorf("exportTx requires id")
}
func resolveExportTxs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ExportTx", pl(args))
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
