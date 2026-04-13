// Package servicenode provides resolvers for the S-Chain (ServiceNodeVM).
//
// Indexes: service nodes, registrations, SLA records, uptime proofs, endpoints.
//
// Entities: ServiceNode, ServiceRegistration, SLARecord, UptimeProof, ServiceEndpoint
package servicenode

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["serviceNode"] = resolveServiceNode
	resolvers["serviceNodes"] = resolveServiceNodes
	resolvers["serviceRegistration"] = resolveServiceRegistration
	resolvers["serviceRegistrations"] = resolveServiceRegistrations
	resolvers["slaRecord"] = resolveSLARecord
	resolvers["slaRecords"] = resolveSLARecords
	resolvers["uptimeProof"] = resolveUptimeProof
	resolvers["uptimeProofs"] = resolveUptimeProofs
	resolvers["serviceEndpoint"] = resolveServiceEndpoint
	resolvers["serviceEndpoints"] = resolveServiceEndpoints
	resolvers["serviceStats"] = resolveServiceStats
}

func resolveServiceNode(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ServiceNode", fmt.Sprint(id)) }
	return nil, fmt.Errorf("serviceNode requires id")
}
func resolveServiceNodes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ServiceNode", pl(args))
}
func resolveServiceRegistration(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ServiceRegistration", fmt.Sprint(id)) }
	return nil, fmt.Errorf("serviceRegistration requires id")
}
func resolveServiceRegistrations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ServiceRegistration", pl(args))
}
func resolveSLARecord(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("SLARecord", fmt.Sprint(id)) }
	return nil, fmt.Errorf("slaRecord requires id")
}
func resolveSLARecords(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("SLARecord", pl(args))
}
func resolveUptimeProof(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("UptimeProof", fmt.Sprint(id)) }
	return nil, fmt.Errorf("uptimeProof requires id")
}
func resolveUptimeProofs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("UptimeProof", pl(args))
}
func resolveServiceEndpoint(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ServiceEndpoint", fmt.Sprint(id)) }
	return nil, fmt.Errorf("serviceEndpoint requires id")
}
func resolveServiceEndpoints(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ServiceEndpoint", pl(args))
}
func resolveServiceStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("ServiceStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return min(limit, 1000)
}
