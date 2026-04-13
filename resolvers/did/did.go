// Package did provides resolvers for the DID registry.
//
// Indexes: decentralized identifiers, delegates, attributes.
//
// Entities: DIDDocument, DIDDelegate, DIDAttribute, DIDStats
package did

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["didDocument"] = resolveDIDDocument
	resolvers["didDocuments"] = resolveDIDDocuments
	resolvers["didDelegate"] = resolveDIDDelegate
	resolvers["didDelegates"] = resolveDIDDelegates
	resolvers["didAttribute"] = resolveDIDAttribute
	resolvers["didAttributes"] = resolveDIDAttributes
	resolvers["didStats"] = resolveDIDStats
}

func resolveDIDDocument(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DIDDocument", fmt.Sprint(id)) }
	return nil, fmt.Errorf("didDocument requires id")
}
func resolveDIDDocuments(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DIDDocument", pl(args))
}
func resolveDIDDelegate(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DIDDelegate", fmt.Sprint(id)) }
	return nil, fmt.Errorf("didDelegate requires id")
}
func resolveDIDDelegates(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DIDDelegate", pl(args))
}
func resolveDIDAttribute(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("DIDAttribute", fmt.Sprint(id)) }
	return nil, fmt.Errorf("didAttribute requires id")
}
func resolveDIDAttributes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("DIDAttribute", pl(args))
}
func resolveDIDStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("DIDStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return min(limit, 1000)
}
