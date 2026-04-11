// Package key provides resolvers for the K-Chain (KeyVM).
//
// Indexes: managed keys, rotations, shares, ceremonies, attestations.
//
// Entities: ManagedKey, KeyRotation, KeyShare, KeyCeremony, KeyAttestation
package key

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["managedKey"] = resolveManagedKey
	resolvers["managedKeys"] = resolveManagedKeys
	resolvers["keyRotation"] = resolveKeyRotation
	resolvers["keyRotations"] = resolveKeyRotations
	resolvers["keyShare"] = resolveKeyShare
	resolvers["keyShares"] = resolveKeyShares
	resolvers["keyCeremony"] = resolveKeyCeremony
	resolvers["keyCeremonies"] = resolveKeyCeremonies
	resolvers["keyAttestation"] = resolveKeyAttestation
	resolvers["keyAttestations"] = resolveKeyAttestations
	resolvers["keyStats"] = resolveKeyStats
}

func resolveManagedKey(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ManagedKey", fmt.Sprint(id)) }
	return nil, fmt.Errorf("managedKey requires id")
}
func resolveManagedKeys(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ManagedKey", pl(args))
}
func resolveKeyRotation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("KeyRotation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("keyRotation requires id")
}
func resolveKeyRotations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("KeyRotation", pl(args))
}
func resolveKeyShare(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("KeyShare", fmt.Sprint(id)) }
	return nil, fmt.Errorf("keyShare requires id")
}
func resolveKeyShares(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("KeyShare", pl(args))
}
func resolveKeyCeremony(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("KeyCeremony", fmt.Sprint(id)) }
	return nil, fmt.Errorf("keyCeremony requires id")
}
func resolveKeyCeremonies(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("KeyCeremony", pl(args))
}
func resolveKeyAttestation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("KeyAttestation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("keyAttestation requires id")
}
func resolveKeyAttestations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("KeyAttestation", pl(args))
}
func resolveKeyStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("KeyStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
