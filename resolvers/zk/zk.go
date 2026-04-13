// Package zk provides resolvers for the Z-Chain (ZKVM).
//
// Indexes: ZK proofs (Groth16, PLONK, Halo2), verifier registrations,
// circuit hashes, proof batches.
//
// Entities: ZKProof, Verifier, CircuitHash, ProofBatch, ZKSession
package zk

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["zkProof"] = resolveZKProof
	resolvers["zkProofs"] = resolveZKProofs
	resolvers["zkVerifier"] = resolveVerifier
	resolvers["zkVerifiers"] = resolveVerifiers
	resolvers["circuitHash"] = resolveCircuitHash
	resolvers["circuitHashes"] = resolveCircuitHashes
	resolvers["proofBatch"] = resolveProofBatch
	resolvers["proofBatches"] = resolveProofBatches
	resolvers["zkSession"] = resolveZKSession
	resolvers["zkStats"] = resolveZKStats
}

func resolveZKProof(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("ZKProof", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("zkProof requires id")
}
func resolveZKProofs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ZKProof", pl(args))
}
func resolveVerifier(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("Verifier", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("zkVerifier requires id")
}
func resolveVerifiers(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Verifier", pl(args))
}
func resolveCircuitHash(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("CircuitHash", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("circuitHash requires id")
}
func resolveCircuitHashes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("CircuitHash", pl(args))
}
func resolveProofBatch(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("ProofBatch", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("proofBatch requires id")
}
func resolveProofBatches(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ProofBatch", pl(args))
}
func resolveZKSession(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("ZKSession", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("zkSession requires id")
}
func resolveZKStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("ZKStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return limit
}
