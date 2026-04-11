// Package fhe provides resolvers for the T-Chain (ThresholdVM / FHE).
//
// Indexes: DKG ceremonies, threshold decryption requests, encrypted compute
// jobs, FHE ciphertext operations, key shares.
//
// Entities: DKGCeremony, DecryptionRequest, ComputeJob, Ciphertext, KeyShare
package fhe

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["dkgCeremony"] = resolveDKGCeremony
	resolvers["dkgCeremonies"] = resolveDKGCeremonies
	resolvers["decryptionRequest"] = resolveDecryptionRequest
	resolvers["decryptionRequests"] = resolveDecryptionRequests
	resolvers["computeJob"] = resolveComputeJob
	resolvers["computeJobs"] = resolveComputeJobs
	resolvers["ciphertext"] = resolveCiphertext
	resolvers["ciphertexts"] = resolveCiphertexts
	resolvers["keyShare"] = resolveKeyShare
	resolvers["keyShares"] = resolveKeyShares
	resolvers["fheStats"] = resolveFHEStats
}

func resolveDKGCeremony(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DKGCeremony", fmt.Sprint(id)) }
	return nil, fmt.Errorf("dkgCeremony requires id")
}
func resolveDKGCeremonies(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DKGCeremony", parseLimit(args))
}
func resolveDecryptionRequest(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DecryptionRequest", fmt.Sprint(id)) }
	return nil, fmt.Errorf("decryptionRequest requires id")
}
func resolveDecryptionRequests(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DecryptionRequest", parseLimit(args))
}
func resolveComputeJob(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ComputeJob", fmt.Sprint(id)) }
	return nil, fmt.Errorf("computeJob requires id")
}
func resolveComputeJobs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ComputeJob", parseLimit(args))
}
func resolveCiphertext(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Ciphertext", fmt.Sprint(id)) }
	return nil, fmt.Errorf("ciphertext requires id")
}
func resolveCiphertexts(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Ciphertext", parseLimit(args))
}
func resolveKeyShare(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("KeyShare", fmt.Sprint(id)) }
	return nil, fmt.Errorf("keyShare requires id")
}
func resolveKeyShares(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("KeyShare", parseLimit(args))
}
func resolveFHEStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("FHEStats", "1")
}

func parseLimit(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
