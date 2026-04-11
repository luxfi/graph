package engine

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

// Core blockchain resolvers — always registered.

func (e *Engine) resolveBlock(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetBlock(ctx, fmt.Sprint(id))
	}
	if num, ok := args["number"]; ok {
		return s.GetBlockByNumber(ctx, fmt.Sprint(num))
	}
	return s.GetLatestBlock(ctx)
}

func (e *Engine) resolveBlocks(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit := 25
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return s.GetBlocks(ctx, limit)
}

func (e *Engine) resolveTransaction(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if hash, ok := args["id"]; ok {
		return s.GetTransaction(ctx, fmt.Sprint(hash))
	}
	return nil, fmt.Errorf("transaction requires id argument")
}

func (e *Engine) resolveTransactions(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit := 25
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return s.GetTransactions(ctx, limit)
}

func (e *Engine) resolveToken(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if addr, ok := args["id"]; ok {
		return s.GetToken(ctx, fmt.Sprint(addr))
	}
	return nil, fmt.Errorf("token requires id argument")
}

func (e *Engine) resolveTokens(ctx context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	limit, orderBy, orderDir, where := parseListArgs(args, 100)
	return s.GetTokens(ctx, limit, orderBy, orderDir, where)
}
