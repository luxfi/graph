// Package fhe provides resolvers for the T-Chain (ThresholdVM / FHE).
//
// Indexes: DKG ceremonies, threshold decryption requests, encrypted compute
// jobs, FHE ciphertext operations, key shares, FHE policy bindings
// (M-Chain × F-Chain policy gating per LP-114).
//
// Entities: DKGCeremony, DecryptionRequest, ComputeJob, Ciphertext, KeyShare,
//
//	FHEPolicy, FHEPolicyBinding.
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
	resolvers["fhePolicy"] = resolveFHEPolicy
	resolvers["fhePolicies"] = resolveFHEPolicies
	resolvers["fhePolicyBinding"] = resolveFHEPolicyBinding
	resolvers["fhePolicyBindings"] = resolveFHEPolicyBindings
}

func resolveDKGCeremony(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("DKGCeremony", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("dkgCeremony requires id")
}
func resolveDKGCeremonies(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DKGCeremony", parseLimit(args))
}
func resolveDecryptionRequest(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("DecryptionRequest", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("decryptionRequest requires id")
}
func resolveDecryptionRequests(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DecryptionRequest", parseLimit(args))
}
func resolveComputeJob(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("ComputeJob", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("computeJob requires id")
}
func resolveComputeJobs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ComputeJob", parseLimit(args))
}
func resolveCiphertext(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("Ciphertext", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("ciphertext requires id")
}
func resolveCiphertexts(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Ciphertext", parseLimit(args))
}
func resolveKeyShare(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("KeyShare", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("keyShare requires id")
}
func resolveKeyShares(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("KeyShare", parseLimit(args))
}
func resolveFHEStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("FHEStats", "1")
}

// FHEPolicy: an encrypted-policy descriptor anchored on M-Chain
// (subjectId, policyHash, threshold, expiresAt).
func resolveFHEPolicy(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("FHEPolicy", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("fhePolicy requires id")
}
func resolveFHEPolicies(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("FHEPolicy", parseLimit(args))
}

// FHEPolicyBinding: binding of an FHE policy to an on-chain resource
// (token, vault, identity) — per LP-114 M-Chain × F-Chain integration.
func resolveFHEPolicyBinding(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("FHEPolicyBinding", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("fhePolicyBinding requires id")
}
func resolveFHEPolicyBindings(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("FHEPolicyBinding", parseLimit(args))
}

func parseLimit(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return limit
}
