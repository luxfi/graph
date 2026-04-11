// Package privacy provides resolvers for the Z-Chain (ZKVM / Privacy).
//
// Indexes: ZK proofs, shielded transfers, nullifiers, commitment trees.
//
// Entities: ShieldedTransfer, ZKProof, Nullifier, Commitment, PrivacyPool
package privacy

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["shieldedTransfer"] = resolveShieldedTransfer
	resolvers["shieldedTransfers"] = resolveShieldedTransfers
	resolvers["zkProof"] = resolveZKProof
	resolvers["zkProofs"] = resolveZKProofs
	resolvers["nullifier"] = resolveNullifier
	resolvers["nullifiers"] = resolveNullifiers
	resolvers["commitment"] = resolveCommitment
	resolvers["commitments"] = resolveCommitments
	resolvers["privacyPool"] = resolvePrivacyPool
	resolvers["privacyPools"] = resolvePrivacyPools
}

func resolveShieldedTransfer(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ShieldedTransfer", fmt.Sprint(id)) }
	return nil, fmt.Errorf("shieldedTransfer requires id")
}
func resolveShieldedTransfers(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ShieldedTransfer", pl(args))
}
func resolveZKProof(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ZKProof", fmt.Sprint(id)) }
	return nil, fmt.Errorf("zkProof requires id")
}
func resolveZKProofs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ZKProof", pl(args))
}
func resolveNullifier(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Nullifier", fmt.Sprint(id)) }
	return nil, fmt.Errorf("nullifier requires id")
}
func resolveNullifiers(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Nullifier", pl(args))
}
func resolveCommitment(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Commitment", fmt.Sprint(id)) }
	return nil, fmt.Errorf("commitment requires id")
}
func resolveCommitments(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Commitment", pl(args))
}
func resolvePrivacyPool(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("PrivacyPool", fmt.Sprint(id)) }
	return nil, fmt.Errorf("privacyPool requires id")
}
func resolvePrivacyPools(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("PrivacyPool", pl(args))
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
