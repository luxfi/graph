// Package identity provides resolvers for the I-Chain (IdentityVM).
//
// Indexes: decentralized identifiers, verifiable credentials, attestations.
//
// Entities: DID, VerifiableCredential, Attestation, IdentityRegistry, CredentialSchema
package identity

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["did"] = resolveDID
	resolvers["dids"] = resolveDIDs
	resolvers["verifiableCredential"] = resolveVerifiableCredential
	resolvers["verifiableCredentials"] = resolveVerifiableCredentials
	resolvers["attestation"] = resolveAttestation
	resolvers["attestations"] = resolveAttestations
	resolvers["identityRegistry"] = resolveIdentityRegistry
	resolvers["identityRegistries"] = resolveIdentityRegistries
	resolvers["credentialSchema"] = resolveCredentialSchema
	resolvers["credentialSchemas"] = resolveCredentialSchemas
	resolvers["identityStats"] = resolveIdentityStats
}

func resolveDID(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DID", fmt.Sprint(id)) }
	return nil, fmt.Errorf("did requires id")
}
func resolveDIDs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DID", pl(args))
}
func resolveVerifiableCredential(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("VerifiableCredential", fmt.Sprint(id)) }
	return nil, fmt.Errorf("verifiableCredential requires id")
}
func resolveVerifiableCredentials(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("VerifiableCredential", pl(args))
}
func resolveAttestation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("Attestation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("attestation requires id")
}
func resolveAttestations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Attestation", pl(args))
}
func resolveIdentityRegistry(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("IdentityRegistry", fmt.Sprint(id)) }
	return nil, fmt.Errorf("identityRegistry requires id")
}
func resolveIdentityRegistries(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("IdentityRegistry", pl(args))
}
func resolveCredentialSchema(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("CredentialSchema", fmt.Sprint(id)) }
	return nil, fmt.Errorf("credentialSchema requires id")
}
func resolveCredentialSchemas(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("CredentialSchema", pl(args))
}
func resolveIdentityStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("IdentityStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
