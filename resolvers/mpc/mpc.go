// Package mpc provides resolvers for the M-Chain (MPC coordination VM).
//
// Indexes: threshold signing sessions (CGGMP21 ECDSA, FROST EdDSA),
// keygen sessions, signer sets, presignatures, signing requests.
//
// Entities: SigningSession, KeygenSession, SignerSet, Presignature, SignRequest
package mpc

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["signingSession"] = resolveSigningSession
	resolvers["signingSessions"] = resolveSigningSessions
	resolvers["keygenSession"] = resolveKeygenSession
	resolvers["keygenSessions"] = resolveKeygenSessions
	resolvers["signerSet"] = resolveSignerSet
	resolvers["signerSets"] = resolveSignerSets
	resolvers["presignature"] = resolvePresignature
	resolvers["presignatures"] = resolvePresignatures
	resolvers["signRequest"] = resolveSignRequest
	resolvers["signRequests"] = resolveSignRequests
	resolvers["mpcStats"] = resolveMPCStats
}

func resolveSigningSession(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("SigningSession", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("signingSession requires id")
}
func resolveSigningSessions(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SigningSession", pl(args))
}
func resolveKeygenSession(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("KeygenSession", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("keygenSession requires id")
}
func resolveKeygenSessions(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("KeygenSession", pl(args))
}
func resolveSignerSet(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("SignerSet", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("signerSet requires id")
}
func resolveSignerSets(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SignerSet", pl(args))
}
func resolvePresignature(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("Presignature", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("presignature requires id")
}
func resolvePresignatures(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("Presignature", pl(args))
}
func resolveSignRequest(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("SignRequest", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("signRequest requires id")
}
func resolveSignRequests(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SignRequest", pl(args))
}
func resolveMPCStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("MPCStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return limit
}
