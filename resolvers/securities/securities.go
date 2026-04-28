// Package securities provides resolvers for security tokens.
//
// Indexes ERC-3643 + ONCHAINID surfaces:
//   - issuances, transfers, dividends, compliance records (core token)
//   - identity registry actions (IdentityRegistry contract)
//   - claim lifecycle (ONCHAINID ClaimAdded/Removed/Changed)
//   - transfer-agent actions (Recovery, AddressFrozen, TokensFrozen)
//
// Entities: SecurityIssuance, SecurityTransfer, SecurityDividend,
//
//	SecurityCompliance, SecurityStats, OnchainIdClaim, TransferAgentAction,
//	FrozenAccount, FrozenTokens, IdentityRegistryAction.
package securities

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	// Core token surfaces.
	resolvers["securityIssuance"] = resolveSecurityIssuance
	resolvers["securityIssuances"] = resolveSecurityIssuances
	resolvers["securityTransfer"] = resolveSecurityTransfer
	resolvers["securityTransfers"] = resolveSecurityTransfers
	resolvers["securityDividend"] = resolveSecurityDividend
	resolvers["securityDividends"] = resolveSecurityDividends
	resolvers["securityCompliance"] = resolveSecurityCompliance
	resolvers["securityCompliances"] = resolveSecurityCompliances
	resolvers["securityStats"] = resolveSecurityStats

	// ERC-3643 + ONCHAINID surfaces.
	resolvers["onchainIdClaim"] = resolveOnchainIdClaim
	resolvers["onchainIdClaims"] = resolveOnchainIdClaims
	resolvers["transferAgentAction"] = resolveTransferAgentAction
	resolvers["transferAgentActions"] = resolveTransferAgentActions
	resolvers["frozenAccount"] = resolveFrozenAccount
	resolvers["frozenAccounts"] = resolveFrozenAccounts
	resolvers["frozenTokens"] = resolveFrozenTokensRec
	resolvers["frozenTokensList"] = resolveFrozenTokensList
	resolvers["identityRegistryAction"] = resolveIdentityRegistryAction
	resolvers["identityRegistryActions"] = resolveIdentityRegistryActions
}

func resolveSecurityIssuance(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("SecurityIssuance", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("securityIssuance requires id")
}
func resolveSecurityIssuances(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SecurityIssuance", pl(args))
}
func resolveSecurityTransfer(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("SecurityTransfer", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("securityTransfer requires id")
}
func resolveSecurityTransfers(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SecurityTransfer", pl(args))
}
func resolveSecurityDividend(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("SecurityDividend", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("securityDividend requires id")
}
func resolveSecurityDividends(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SecurityDividend", pl(args))
}
func resolveSecurityCompliance(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("SecurityCompliance", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("securityCompliance requires id")
}
func resolveSecurityCompliances(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SecurityCompliance", pl(args))
}
func resolveSecurityStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("SecurityStats", "1")
}

// ONCHAINID claim resolvers (ClaimAdded / ClaimRemoved / ClaimChanged).
func resolveOnchainIdClaim(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("OnchainIdClaim", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("onchainIdClaim requires id")
}
func resolveOnchainIdClaims(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("OnchainIdClaim", pl(args))
}

// Transfer-agent action resolvers (Recovery / AddressFrozen / TokensFrozen).
func resolveTransferAgentAction(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("TransferAgentAction", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("transferAgentAction requires id")
}
func resolveTransferAgentActions(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("TransferAgentAction", pl(args))
}

// Frozen-account snapshot (current state of AddressFrozen on a holder).
func resolveFrozenAccount(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("FrozenAccount", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("frozenAccount requires id")
}
func resolveFrozenAccounts(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("FrozenAccount", pl(args))
}

// Frozen-tokens snapshot (per-holder partial freeze amount).
func resolveFrozenTokensRec(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("FrozenTokens", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("frozenTokens requires id")
}
func resolveFrozenTokensList(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("FrozenTokens", pl(args))
}

// IdentityRegistry actions (IdentityRegistered / IdentityRemoved / IdentityStored / CountryUpdated).
func resolveIdentityRegistryAction(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok {
		return s.GetByType("IdentityRegistryAction", fmt.Sprint(id))
	}
	return nil, fmt.Errorf("identityRegistryAction requires id")
}
func resolveIdentityRegistryActions(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("IdentityRegistryAction", pl(args))
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok {
		fmt.Sscanf(fmt.Sprint(l), "%d", &limit)
	}
	return min(limit, 1000)
}
