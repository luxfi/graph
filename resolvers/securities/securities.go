// Package securities provides resolvers for security tokens.
//
// Indexes: issuances, transfers, dividends, compliance records.
//
// Entities: SecurityIssuance, SecurityTransfer, SecurityDividend, SecurityCompliance, SecurityStats
package securities

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["securityIssuance"] = resolveSecurityIssuance
	resolvers["securityIssuances"] = resolveSecurityIssuances
	resolvers["securityTransfer"] = resolveSecurityTransfer
	resolvers["securityTransfers"] = resolveSecurityTransfers
	resolvers["securityDividend"] = resolveSecurityDividend
	resolvers["securityDividends"] = resolveSecurityDividends
	resolvers["securityCompliance"] = resolveSecurityCompliance
	resolvers["securityCompliances"] = resolveSecurityCompliances
	resolvers["securityStats"] = resolveSecurityStats
}

func resolveSecurityIssuance(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("SecurityIssuance", fmt.Sprint(id)) }
	return nil, fmt.Errorf("securityIssuance requires id")
}
func resolveSecurityIssuances(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SecurityIssuance", pl(args))
}
func resolveSecurityTransfer(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("SecurityTransfer", fmt.Sprint(id)) }
	return nil, fmt.Errorf("securityTransfer requires id")
}
func resolveSecurityTransfers(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SecurityTransfer", pl(args))
}
func resolveSecurityDividend(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("SecurityDividend", fmt.Sprint(id)) }
	return nil, fmt.Errorf("securityDividend requires id")
}
func resolveSecurityDividends(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SecurityDividend", pl(args))
}
func resolveSecurityCompliance(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("SecurityCompliance", fmt.Sprint(id)) }
	return nil, fmt.Errorf("securityCompliance requires id")
}
func resolveSecurityCompliances(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SecurityCompliance", pl(args))
}
func resolveSecurityStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("SecurityStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return limit
}
