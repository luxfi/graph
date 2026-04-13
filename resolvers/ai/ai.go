// Package ai provides resolvers for the A-Chain (AIVM).
//
// Indexes: inference proofs, model hashes, compute attestations, jobs, training runs.
//
// Entities: InferenceProof, ModelHash, ComputeAttestation, AIJob, TrainingRun
package ai

import (
	"context"
	"fmt"

	"github.com/luxfi/graph/storage"
)

type ResolverFunc = func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error)

func Register(resolvers map[string]ResolverFunc) {
	resolvers["inferenceProof"] = resolveInferenceProof
	resolvers["inferenceProofs"] = resolveInferenceProofs
	resolvers["modelHash"] = resolveModelHash
	resolvers["modelHashes"] = resolveModelHashes
	resolvers["computeAttestation"] = resolveComputeAttestation
	resolvers["computeAttestations"] = resolveComputeAttestations
	resolvers["aiJob"] = resolveAIJob
	resolvers["aiJobs"] = resolveAIJobs
	resolvers["trainingRun"] = resolveTrainingRun
	resolvers["trainingRuns"] = resolveTrainingRuns
	resolvers["aiStats"] = resolveAIStats
}

func resolveInferenceProof(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("InferenceProof", fmt.Sprint(id)) }
	return nil, fmt.Errorf("inferenceProof requires id")
}
func resolveInferenceProofs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("InferenceProof", pl(args))
}
func resolveModelHash(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ModelHash", fmt.Sprint(id)) }
	return nil, fmt.Errorf("modelHash requires id")
}
func resolveModelHashes(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ModelHash", pl(args))
}
func resolveComputeAttestation(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("ComputeAttestation", fmt.Sprint(id)) }
	return nil, fmt.Errorf("computeAttestation requires id")
}
func resolveComputeAttestations(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("ComputeAttestation", pl(args))
}
func resolveAIJob(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("AIJob", fmt.Sprint(id)) }
	return nil, fmt.Errorf("aiJob requires id")
}
func resolveAIJobs(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("AIJob", pl(args))
}
func resolveTrainingRun(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	if id, ok := args["id"]; ok { return s.GetByType("TrainingRun", fmt.Sprint(id)) }
	return nil, fmt.Errorf("trainingRun requires id")
}
func resolveTrainingRuns(_ context.Context, s *storage.Store, args map[string]interface{}) (interface{}, error) {
	return s.ListByType("TrainingRun", pl(args))
}
func resolveAIStats(_ context.Context, s *storage.Store, _ map[string]interface{}) (interface{}, error) {
	return s.GetByType("AIStats", "1")
}

func pl(args map[string]interface{}) int {
	limit := 100
	if l, ok := args["first"]; ok { fmt.Sscanf(fmt.Sprint(l), "%d", &limit) }
	return min(limit, 1000)
}
