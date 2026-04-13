// Package precompile provides resolvers for Lux EVM precompiled contract calls.
//
// Precompiles are native EVM operations at fixed addresses — callable from
// Solidity, indexed by the graph engine when they emit logs or are called
// via traces.
//
// Address ranges:
//
//	0x02xx — Warp (cross-chain messaging)
//	0x03xx — AI Mining (proof-of-work for AI compute)
//	0x05xx — Crypto primitives (Poseidon2, Pedersen, Ed25519, ECIES)
//	0x07xx — FHE (fully homomorphic encryption + ACL + Gateway)
//	0x09xx — ZK proofs (Groth16, Plonk, Fflonk, Halo2, KZG, IPA, privacy pool)
//	0x0Axx — Graph (on-chain GraphQL schema registry)
//	0x92xx — DEX (native orderbook: limit, market, cancel)
//	0x9003 — Blake3 hash
//	0xB002 — Ring signatures (Ringtail)
//
// Entities: PrecompileCall, AIWorkProof, FHEOperation, ZKVerification,
//
//	CryptoOp, WarpCall, DEXOrder, RingSignature, GraphSchemaUpdate
package precompile

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	// Generic precompile call tracking
	resolvers["precompileCall"] = resolvePrecompileCall
	resolvers["precompileCalls"] = resolvePrecompileCalls

	// AI Mining (0x03xx)
	resolvers["aiWorkProof"] = resolveByType("AIWorkProof")
	resolvers["aiWorkProofs"] = resolveListType("AIWorkProof")

	// FHE (0x07xx)
	resolvers["fheOperation"] = resolveByType("FHEOperation")
	resolvers["fheOperations"] = resolveListType("FHEOperation")
	resolvers["fheACLGrant"] = resolveByType("FHEACLGrant")
	resolvers["fheACLGrants"] = resolveListType("FHEACLGrant")
	resolvers["fheGatewayRequest"] = resolveByType("FHEGatewayRequest")
	resolvers["fheGatewayRequests"] = resolveListType("FHEGatewayRequest")

	// Crypto primitives (0x05xx)
	resolvers["cryptoOp"] = resolveByType("CryptoOp")
	resolvers["cryptoOps"] = resolveListType("CryptoOp")

	// ZK proofs (0x09xx)
	resolvers["zkVerification"] = resolveByType("ZKVerification")
	resolvers["zkVerifications"] = resolveListType("ZKVerification")
	resolvers["privacyPoolOp"] = resolveByType("PrivacyPoolOp")
	resolvers["privacyPoolOps"] = resolveListType("PrivacyPoolOp")
	resolvers["rollupVerification"] = resolveByType("RollupVerification")
	resolvers["rollupVerifications"] = resolveListType("RollupVerification")

	// Warp (0x02xx)
	resolvers["warpCall"] = resolveByType("WarpCall")
	resolvers["warpCalls"] = resolveListType("WarpCall")

	// DEX (0x92xx)
	resolvers["dexPrecompileOrder"] = resolveByType("DEXPrecompileOrder")
	resolvers["dexPrecompileOrders"] = resolveListType("DEXPrecompileOrder")

	// Ring signatures (0xB002)
	resolvers["ringSignature"] = resolveByType("RingSignature")
	resolvers["ringSignatures"] = resolveListType("RingSignature")

	// Graph schema (0x0Axx)
	resolvers["graphSchemaUpdate"] = resolveByType("GraphSchemaUpdate")
	resolvers["graphSchemaUpdates"] = resolveListType("GraphSchemaUpdate")

	// PQ crypto (ML-DSA, ML-KEM, SLH-DSA)
	resolvers["pqCryptoOp"] = resolveByType("PQCryptoOp")
	resolvers["pqCryptoOps"] = resolveListType("PQCryptoOp")

	// Threshold (CGGMP21, FROST)
	resolvers["thresholdOp"] = resolveByType("ThresholdOp")
	resolvers["thresholdOps"] = resolveListType("ThresholdOp")

	// Stats
	resolvers["precompileStats"] = resolvePrecompileStats
}

// Known precompile address ranges for reference.
var Addresses = map[string]string{
	"warp":            "0x0200000000000000000000000000000000000007",
	"aiMining":        "0x0300000000000000000000000000000000000000",
	"poseidon2":       "0x0500000000000000000000000000000000000001",
	"pedersen":        "0x0500000000000000000000000000000000000003",
	"ed25519":         "0x0500000000000000000000000000000000000004",
	"fhe":             "0x0700000000000000000000000000000000000000",
	"fheACL":          "0x0700000000000000000000000000000000000001",
	"fheGateway":      "0x0700000000000000000000000000000000000003",
	"zkVerify":        "0x0900000000000000000000000000000000000000",
	"groth16":         "0x0900000000000000000000000000000000000001",
	"plonk":           "0x0900000000000000000000000000000000000002",
	"fflonk":          "0x0900000000000000000000000000000000000003",
	"halo2":           "0x0900000000000000000000000000000000000004",
	"kzg":             "0x0900000000000000000000000000000000000010",
	"ipa":             "0x0900000000000000000000000000000000000012",
	"privacyPool":     "0x0900000000000000000000000000000000000020",
	"nullifier":       "0x0900000000000000000000000000000000000021",
	"commitment":      "0x0900000000000000000000000000000000000022",
	"rangeProof":      "0x0900000000000000000000000000000000000023",
	"rollupVerify":    "0x0900000000000000000000000000000000000030",
	"stateRoot":       "0x0900000000000000000000000000000000000031",
	"batchProof":      "0x0900000000000000000000000000000000000032",
	"graph":           "0x0A00000000000000000000000000000000000001",
	"blake3":          "0x9003",
	"dexLimit":        "0x9200",
	"dexMarket":       "0x9201",
	"dexCancel":       "0x9202",
	"ringtail":        "0xB002",
}

func resolvePrecompileCall(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("PrecompileCall", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("precompileCall requires id")
}

func resolvePrecompileCalls(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("PrecompileCall", pl(args))
}

func resolvePrecompileStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("PrecompileStats", "1")
}

// Factory helpers for repetitive resolver patterns.
func resolveByType(entityType string) ResolverFunc {
	return func(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
		if id, ok := args["id"]; ok {
			return s.GetByType(entityType, fmt.Sprint(id))
		}
		return nil, fmt.Errorf("%s requires id", entityType)
	}
}

func resolveListType(entityType string) ResolverFunc {
	return func(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
		return s.ListByType(entityType, pl(args))
	}
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return min(limit, 1000)
}
