// Package utxo provides resolvers for the X-Chain (XVM).
//
// Indexes: UTXO set, assets, asset creation transactions, address balances.
//
// Entities: UTXO, Asset, AssetCreateTx, UTXOBalance
package utxo

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["utxo"] = resolveUTXO
	resolvers["utxos"] = resolveUTXOs
	resolvers["asset"] = resolveAsset
	resolvers["assets"] = resolveAssets
	resolvers["assetCreateTx"] = resolveAssetCreateTx
	resolvers["assetCreateTxs"] = resolveAssetCreateTxs
	resolvers["utxoBalance"] = resolveUTXOBalance
	resolvers["utxoStats"] = resolveUTXOStats
}

func resolveUTXO(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("UTXO", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("utxo requires id")
}
func resolveUTXOs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("UTXO", pl(args))
}
func resolveAsset(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("Asset", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("asset requires id")
}
func resolveAssets(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Asset", pl(args))
}
func resolveAssetCreateTx(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("AssetCreateTx", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("assetCreateTx requires id")
}
func resolveAssetCreateTxs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("AssetCreateTx", pl(args))
}
func resolveUTXOBalance(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if addr, ok := args["address"]; ok {
		return s.GetByType("UTXOBalance", fmt.Sprint(addr))
	}
	return nil, fmt.Errorf("utxoBalance requires address")
}
func resolveUTXOStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("UTXOStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return limit
}
