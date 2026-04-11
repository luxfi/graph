// Package quantum provides resolvers for the Q-Chain (QuantumVM).
//
// Indexes: post-quantum signatures, proofs, key pairs, finality, attestations.
//
// Entities: RingtailSignature, QuantumProof, PQKeyPair, QuantumFinality, QuantumAttestation
package quantum

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["ringtailSignature"] = resolveRingtailSignature
	resolvers["ringtailSignatures"] = resolveRingtailSignatures
	resolvers["quantumProof"] = resolveQuantumProof
	resolvers["quantumProofs"] = resolveQuantumProofs
	resolvers["pqKeyPair"] = resolvePQKeyPair
	resolvers["pqKeyPairs"] = resolvePQKeyPairs
	resolvers["quantumFinality"] = resolveQuantumFinality
	resolvers["quantumFinalitys"] = resolveQuantumFinalitys
	resolvers["quantumAttestation"] = resolveQuantumAttestation
	resolvers["quantumAttestations"] = resolveQuantumAttestations
	resolvers["quantumStats"] = resolveQuantumStats
}

func resolveRingtailSignature(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("RingtailSignature", fmt.Sprint(id)) }
	return nil, fmt.Errorf("ringtailSignature requires id")
}
func resolveRingtailSignatures(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("RingtailSignature", pl(args))
}
func resolveQuantumProof(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("QuantumProof", fmt.Sprint(id)) }
	return nil, fmt.Errorf("quantumProof requires id")
}
func resolveQuantumProofs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("QuantumProof", pl(args))
}
func resolvePQKeyPair(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("PQKeyPair", fmt.Sprint(id)) }
	return nil, fmt.Errorf("pqKeyPair requires id")
}
func resolvePQKeyPairs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("PQKeyPair", pl(args))
}
func resolveQuantumFinality(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("QuantumFinality", fmt.Sprint(id)) }
	return nil, fmt.Errorf("quantumFinality requires id")
}
func resolveQuantumFinalitys(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("QuantumFinality", pl(args))
}
func resolveQuantumAttestation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("QuantumAttestation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("quantumAttestation requires id")
}
func resolveQuantumAttestations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("QuantumAttestation", pl(args))
}
func resolveQuantumStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("QuantumStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
