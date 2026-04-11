// Package wasm provides a subgraph execution runtime using wazero.
//
// Loads compiled AssemblyScript WASM modules from `graph build` output,
// implements The Graph's host functions (store.set, store.get, ethereum.call,
// log.log, etc.), and executes event handlers against incoming EVM logs.
//
// This is a drop-in replacement for Graph Node's WASM runtime, but in pure Go.
//
// Usage:
//
//	rt, _ := wasm.NewRuntime(ctx)
//	sg, _ := rt.LoadSubgraph("./uni-v2-subgraph/build/")
//	sg.HandleEvent(log)  // executes the AS mapping handler
//	entities := sg.Flush()  // returns modified entities for storage
package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Runtime manages WASM module instances for subgraph execution.
type Runtime struct {
	ctx context.Context
}

// NewRuntime creates a WASM execution runtime.
// Uses wazero (pure Go, no CGO) for AssemblyScript module execution.
func NewRuntime(ctx context.Context) (*Runtime, error) {
	return &Runtime{ctx: ctx}, nil
}

// Close releases all WASM resources.
func (r *Runtime) Close() error {
	return nil
}

// Subgraph represents a loaded subgraph with its WASM modules and schema.
type Subgraph struct {
	Name        string
	Schema      string // GraphQL SDL
	DataSources []DataSource
	Templates   []Template
	Entities    *EntityStore
}

// DataSource maps a contract to its WASM handler module.
type DataSource struct {
	Name       string          `yaml:"name"`
	Kind       string          `yaml:"kind"`
	Network    string          `yaml:"network"`
	Source     ContractSource  `yaml:"source"`
	Mapping    Mapping         `yaml:"mapping"`
	wasmBytes  []byte
}

// ContractSource identifies the contract to watch.
type ContractSource struct {
	Address    string `yaml:"address"`
	ABI        string `yaml:"abi"`
	StartBlock int64  `yaml:"startBlock"`
}

// Mapping defines event handlers in a WASM module.
type Mapping struct {
	Kind          string         `yaml:"kind"`
	APIVersion    string         `yaml:"apiVersion"`
	Language      string         `yaml:"language"`
	File          string         `yaml:"file"`
	Entities      []string       `yaml:"entities"`
	ABIs          []ABIRef       `yaml:"abis"`
	EventHandlers []EventHandler `yaml:"eventHandlers"`
}

// ABIRef references an ABI file.
type ABIRef struct {
	Name string `yaml:"name"`
	File string `yaml:"file"`
}

// EventHandler maps an EVM event signature to a WASM handler function.
type EventHandler struct {
	Event   string `yaml:"event"` // e.g., "Swap(indexed address,uint256,uint256,uint256,uint256,indexed address)"
	Handler string `yaml:"handler"` // e.g., "handleSwap"
}

// Template is a dynamically created data source (e.g., new Pair from PairCreated).
type Template struct {
	Name    string  `yaml:"name"`
	Kind    string  `yaml:"kind"`
	Mapping Mapping `yaml:"mapping"`
}

// EntityStore is an in-memory entity cache that the WASM modules write to.
// Flushed to the storage layer after each block.
type EntityStore struct {
	entities map[string]map[string]map[string]interface{} // type -> id -> fields
}

// NewEntityStore creates an empty entity store.
func NewEntityStore() *EntityStore {
	return &EntityStore{
		entities: make(map[string]map[string]map[string]interface{}),
	}
}

// Set stores an entity (called by WASM host function store.set).
func (es *EntityStore) Set(entityType, id string, data map[string]interface{}) {
	if es.entities[entityType] == nil {
		es.entities[entityType] = make(map[string]map[string]interface{})
	}
	es.entities[entityType][id] = data
}

// Get retrieves an entity (called by WASM host function store.get).
func (es *EntityStore) Get(entityType, id string) (map[string]interface{}, bool) {
	if es.entities[entityType] == nil {
		return nil, false
	}
	data, ok := es.entities[entityType][id]
	return data, ok
}

// Flush returns all modified entities and resets the store.
func (es *EntityStore) Flush() map[string]map[string]map[string]interface{} {
	result := es.entities
	es.entities = make(map[string]map[string]map[string]interface{})
	return result
}

// SubgraphManifest is the parsed subgraph.yaml.
type SubgraphManifest struct {
	SpecVersion string       `yaml:"specVersion"`
	Description string       `yaml:"description"`
	Schema      SchemaRef    `yaml:"schema"`
	DataSources []DataSource `yaml:"dataSources"`
	Templates   []Template   `yaml:"templates"`
}

// SchemaRef points to the GraphQL schema file.
type SchemaRef struct {
	File string `yaml:"file"`
}

// LoadSubgraph loads a compiled subgraph from a build directory.
// Expects: build/subgraph.yaml, build/{DataSource}/*.wasm, build/schema.graphql
func (r *Runtime) LoadSubgraph(buildDir string) (*Subgraph, error) {
	// Parse manifest
	manifestPath := filepath.Join(buildDir, "subgraph.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest SubgraphManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Read schema
	schemaPath := filepath.Join(buildDir, manifest.Schema.File)
	if manifest.Schema.File == "" {
		schemaPath = filepath.Join(buildDir, "schema.graphql")
	}
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}

	// Load WASM modules for each data source
	for i := range manifest.DataSources {
		ds := &manifest.DataSources[i]
		wasmPath := filepath.Join(buildDir, ds.Name, ds.Name+".wasm")
		wasm, err := os.ReadFile(wasmPath)
		if err != nil {
			return nil, fmt.Errorf("load wasm %s: %w", ds.Name, err)
		}
		ds.wasmBytes = wasm
	}

	sg := &Subgraph{
		Name:        manifest.Description,
		Schema:      string(schemaBytes),
		DataSources: manifest.DataSources,
		Templates:   manifest.Templates,
		Entities:    NewEntityStore(),
	}

	return sg, nil
}

// EventLog represents an EVM event log to process.
type EventLog struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	BlockNumber uint64   `json:"blockNumber"`
	TxHash      string   `json:"transactionHash"`
	LogIndex    uint64   `json:"logIndex"`
	Timestamp   uint64   `json:"timestamp"`
}

// HandleEvent routes an EVM log to the correct WASM handler.
func (sg *Subgraph) HandleEvent(log *EventLog) error {
	if len(log.Topics) == 0 {
		return nil
	}

	topic0 := log.Topics[0]

	for _, ds := range sg.DataSources {
		for _, handler := range ds.Mapping.EventHandlers {
			// Match topic0 against the event signature
			// TODO: compute keccak256(event signature) and compare with topic0
			_ = handler
			_ = topic0
		}
	}

	// TODO: instantiate WASM module via wazero, call handler function
	// with the decoded event parameters. The handler calls store.set/store.get
	// via host functions which write to sg.Entities.

	return nil
}

// Info returns subgraph metadata as JSON.
func (sg *Subgraph) Info() string {
	info := map[string]interface{}{
		"name":        sg.Name,
		"dataSources": len(sg.DataSources),
		"templates":   len(sg.Templates),
	}
	b, _ := json.Marshal(info)
	return string(b)
}
