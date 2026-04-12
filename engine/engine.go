// Package engine implements the GraphQL query execution engine.
//
// Extracted from node/vms/graphvm — same resolvers, no consensus deps.
// Reads from local storage (SQLite/ZapDB), serves GraphQL queries.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/graph/resolvers/ai"
	"github.com/luxfi/graph/resolvers/bridge"
	"github.com/luxfi/graph/resolvers/dao"
	"github.com/luxfi/graph/resolvers/derivatives"
	"github.com/luxfi/graph/resolvers/dex"
	"github.com/luxfi/graph/resolvers/did"
	"github.com/luxfi/graph/resolvers/exchange"
	"github.com/luxfi/graph/resolvers/fhe"
	"github.com/luxfi/graph/resolvers/governance"
	"github.com/luxfi/graph/resolvers/identity"
	"github.com/luxfi/graph/resolvers/key"
	"github.com/luxfi/graph/resolvers/liquid"
	"github.com/luxfi/graph/resolvers/oracle"
	"github.com/luxfi/graph/resolvers/platform"
	"github.com/luxfi/graph/resolvers/precompile"
	"github.com/luxfi/graph/resolvers/prediction"
	"github.com/luxfi/graph/resolvers/privacy"
	"github.com/luxfi/graph/resolvers/quantum"
	"github.com/luxfi/graph/resolvers/relay"
	"github.com/luxfi/graph/resolvers/securities"
	"github.com/luxfi/graph/resolvers/servicenode"
	"github.com/luxfi/graph/resolvers/treasury"
	"github.com/luxfi/graph/storage"
)

// Config controls query execution limits.
type Config struct {
	MaxQueryDepth  int `json:"maxQueryDepth" yaml:"max_query_depth"`
	MaxResultSize  int `json:"maxResultSize" yaml:"max_result_size"`
	QueryTimeoutMs int `json:"queryTimeoutMs" yaml:"query_timeout_ms"`
}

// Engine executes GraphQL queries against indexed data.
type Engine struct {
	store   *storage.Store
	config  *Config
	timeout time.Duration

	mu        sync.RWMutex
	resolvers map[string]ResolverFunc
	schemas   map[string]*Schema
}

// Schema represents a deployed subgraph schema.
type Schema struct {
	Name    string   `json:"name" yaml:"name"`
	Version string   `json:"version" yaml:"version"`
	Source  string   `json:"source" yaml:"source"` // GraphQL SDL
	Types   []string `json:"types" yaml:"types"`
}

// ResolverFunc resolves a GraphQL field from storage.
type ResolverFunc func(ctx context.Context, store *storage.Store, args map[string]interface{}) (interface{}, error)

// Request is an incoming GraphQL request.
type Request struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName,omitempty"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
}

// Response is a GraphQL response.
type Response struct {
	Data   interface{} `json:"data,omitempty"`
	Errors []Error     `json:"errors,omitempty"`
}

// Error is a GraphQL error.
type Error struct {
	Message string `json:"message"`
}

// New creates a query engine backed by the given store.
func New(store *storage.Store, cfg *Config) *Engine {
	if cfg == nil {
		cfg = &Config{MaxQueryDepth: 10, MaxResultSize: 1 << 20, QueryTimeoutMs: 30000}
	}

	e := &Engine{
		store:     store,
		config:    cfg,
		timeout:   time.Duration(cfg.QueryTimeoutMs) * time.Millisecond,
		resolvers: make(map[string]ResolverFunc),
		schemas:   make(map[string]*Schema),
	}

	e.registerBuiltinResolvers()
	return e
}

// RegisterResolver adds a named resolver.
func (e *Engine) RegisterResolver(name string, fn ResolverFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resolvers[name] = fn
}

// LoadBuiltin loads a built-in schema by name.
func (e *Engine) LoadBuiltin(name string) error {
	switch name {
	case "amm", "amm-v2", "uniswap-v2":
		e.registerAMMResolvers()
		return nil
	case "amm-v3", "uniswap-v3":
		e.registerAMMResolvers()
		return nil
	case "amm-v4", "uniswap-v4", "v4":
		e.registerAMMResolvers()
		e.registerV4Resolvers()
		return nil
	case "dex":
		e.registerChain(dex.Register)
		return nil
	case "fhe", "threshold":
		e.registerChain(fhe.Register)
		return nil
	case "platform", "pchain":
		e.registerChain(platform.Register)
		return nil
	case "exchange", "xchain":
		e.registerChain(exchange.Register)
		return nil
	case "bridge", "bchain":
		e.registerChain(bridge.Register)
		return nil
	case "privacy", "zchain":
		e.registerChain(privacy.Register)
		return nil
	case "quantum", "qchain":
		e.registerChain(quantum.Register)
		return nil
	case "key", "kchain":
		e.registerChain(key.Register)
		return nil
	case "ai", "achain":
		e.registerChain(ai.Register)
		return nil
	case "identity", "ichain":
		e.registerChain(identity.Register)
		return nil
	case "oracle", "ochain":
		e.registerChain(oracle.Register)
		return nil
	case "relay", "rchain":
		e.registerChain(relay.Register)
		return nil
	case "servicenode", "schain":
		e.registerChain(servicenode.Register)
		return nil
	case "precompile", "precompiles":
		e.registerChain(precompile.Register)
		return nil
	case "governance":
		e.registerChain(governance.Register)
		return nil
	case "dao":
		e.registerChain(dao.Register)
		return nil
	case "treasury":
		e.registerChain(treasury.Register)
		return nil
	case "liquid", "liquid-staking":
		e.registerChain(liquid.Register)
		return nil
	case "did", "did-registry":
		e.registerChain(did.Register)
		return nil
	case "prediction", "prediction-market":
		e.registerChain(prediction.Register)
		return nil
	case "securities", "security-token":
		e.registerChain(securities.Register)
		return nil
	case "derivatives", "futures", "options":
		e.registerChain(derivatives.Register)
		return nil
	case "all":
		e.registerAMMResolvers()
		e.registerV4Resolvers()
		e.registerERC20Resolvers()
		e.registerERC721Resolvers()
		e.registerChain(dex.Register)
		e.registerChain(fhe.Register)
		e.registerChain(platform.Register)
		e.registerChain(exchange.Register)
		e.registerChain(bridge.Register)
		e.registerChain(privacy.Register)
		e.registerChain(quantum.Register)
		e.registerChain(key.Register)
		e.registerChain(ai.Register)
		e.registerChain(identity.Register)
		e.registerChain(oracle.Register)
		e.registerChain(relay.Register)
		e.registerChain(servicenode.Register)
		e.registerChain(precompile.Register)
		e.registerChain(governance.Register)
		e.registerChain(dao.Register)
		e.registerChain(treasury.Register)
		e.registerChain(liquid.Register)
		e.registerChain(did.Register)
		e.registerChain(prediction.Register)
		e.registerChain(securities.Register)
		e.registerChain(derivatives.Register)
		return nil
	case "erc20":
		e.registerERC20Resolvers()
		return nil
	case "erc721":
		e.registerERC721Resolvers()
		return nil
	default:
		return fmt.Errorf("unknown built-in schema: %s", name)
	}
}

// LoadConfig loads subgraph configuration from a YAML file.
func (e *Engine) LoadConfig(path string) error {
	// TODO: parse subgraph.yaml, register event handlers + resolvers
	return fmt.Errorf("config loading not yet implemented: %s", path)
}

// Execute runs a GraphQL query.
func (e *Engine) Execute(ctx context.Context, req *Request) *Response {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return &Response{Errors: []Error{{Message: "empty query"}}}
	}
	if len(query) > 100000 {
		return &Response{Errors: []Error{{Message: "query too large"}}}
	}
	if strings.HasPrefix(strings.ToLower(query), "mutation") {
		return &Response{Errors: []Error{{Message: "mutations not allowed — g-node is read-only"}}}
	}

	// Query depth check: count max nested brace depth, reject > MaxQueryDepth
	if depth := queryDepth(query); depth > e.config.MaxQueryDepth {
		return &Response{Errors: []Error{{Message: fmt.Sprintf("query depth %d exceeds maximum %d", depth, e.config.MaxQueryDepth)}}}
	}

	// Parse top-level fields
	fields, err := parseTopFields(query)
	if err != nil {
		return &Response{Errors: []Error{{Message: err.Error()}}}
	}

	if len(fields) > 20 {
		return &Response{Errors: []Error{{Message: fmt.Sprintf("too many top-level fields (%d), maximum 20", len(fields))}}}
	}

	data := make(map[string]interface{})
	for _, f := range fields {
		e.mu.RLock()
		resolver, ok := e.resolvers[f.name]
		e.mu.RUnlock()

		if !ok {
			return &Response{Errors: []Error{{Message: fmt.Sprintf("unknown field: %s", f.name)}}}
		}

		result, err := resolver(ctx, e.store, f.args)
		if err != nil {
			return &Response{Errors: []Error{{Message: err.Error()}}}
		}

		key := f.name
		if f.alias != "" {
			key = f.alias
		}
		data[key] = result
	}

	encoded, _ := json.Marshal(data)
	if len(encoded) > e.config.MaxResultSize {
		return &Response{Errors: []Error{{Message: "result exceeds maximum size"}}}
	}

	return &Response{Data: data}
}

// HandleGraphQL is the HTTP handler for POST /graphql.
func (e *Engine) HandleGraphQL(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"errors":[{"message":"invalid JSON"}]}`, http.StatusBadRequest)
		return
	}

	resp := e.Execute(r.Context(), &req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleGraphiQL serves the GraphiQL IDE for GET /graphql.
func (e *Engine) HandleGraphiQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, graphiqlHTML)
}

// registerChain registers resolvers from a chain-specific package.
// Uses a shim map to bridge the type alias boundary.
func (e *Engine) registerChain(register func(map[string]func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error))) {
	shim := make(map[string]func(context.Context, *storage.Store, map[string]interface{}) (interface{}, error))
	register(shim)
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, v := range shim {
		e.resolvers[k] = v
	}
}

// registerBuiltinResolvers adds core blockchain resolvers.
func (e *Engine) registerBuiltinResolvers() {
	e.resolvers["block"] = e.resolveBlock
	e.resolvers["blocks"] = e.resolveBlocks
	e.resolvers["transaction"] = e.resolveTransaction
	e.resolvers["transactions"] = e.resolveTransactions
	e.resolvers["token"] = e.resolveToken
	e.resolvers["tokens"] = e.resolveTokens
}

// field represents a parsed top-level GraphQL field.
type field struct {
	name  string
	alias string
	args  map[string]interface{}
}

// parseTopFields extracts top-level field names from a GraphQL query.
func parseTopFields(query string) ([]field, error) {
	// Strip query { ... } wrapper
	start := strings.Index(query, "{")
	end := strings.LastIndex(query, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("invalid query: missing braces")
	}

	body := strings.TrimSpace(query[start+1 : end])
	if body == "" {
		return nil, fmt.Errorf("empty selection set")
	}

	var fields []field
	// Simple tokenizer: split on top-level field boundaries
	depth := 0
	var current strings.Builder
	for _, ch := range body {
		switch ch {
		case '{':
			depth++
			current.WriteRune(ch)
		case '}':
			depth--
			current.WriteRune(ch)
		case '\n', '\r':
			if depth == 0 {
				if s := strings.TrimSpace(current.String()); s != "" {
					if f, err := parseField(s); err == nil {
						fields = append(fields, f)
					}
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}
	if s := strings.TrimSpace(current.String()); s != "" {
		if f, err := parseField(s); err == nil {
			fields = append(fields, f)
		}
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("no fields in query")
	}

	return fields, nil
}

func parseField(s string) (field, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return field{}, fmt.Errorf("empty field")
	}

	f := field{args: make(map[string]interface{})}

	// Check for alias — only if colon appears before any paren/brace
	if idx := strings.Index(s, ":"); idx > 0 && !strings.Contains(s[:idx], "(") && !strings.Contains(s[:idx], "{") {
		f.alias = strings.TrimSpace(s[:idx])
		s = strings.TrimSpace(s[idx+1:])
	}

	// Extract name (before args or subselection)
	nameEnd := len(s)
	for i, ch := range s {
		if ch == '(' || ch == '{' || ch == ' ' {
			nameEnd = i
			break
		}
	}
	f.name = s[:nameEnd]

	// Extract args from (...) — brace-aware to handle nested objects like where: { pool: "0x..." }
	if parenStart := strings.Index(s, "("); parenStart != -1 {
		parenEnd := findMatchingParen(s, parenStart)
		if parenEnd > parenStart {
			argsStr := s[parenStart+1 : parenEnd]
			f.args = parseArgs(argsStr)
		}
	}

	return f, nil
}

// findMatchingParen finds the closing ')' that matches the '(' at pos,
// respecting nested braces and quoted strings.
func findMatchingParen(s string, pos int) int {
	depth := 0
	inQuote := false
	for i := pos; i < len(s); i++ {
		ch := s[i]
		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch ch {
		case '(', '{':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		case '}':
			depth--
		}
	}
	return -1
}

// parseArgs splits a top-level argument string into key-value pairs,
// respecting nested braces so that `where: { pool: "0x...", date_gte: 123 }`
// is kept as a single value for key "where".
func parseArgs(s string) map[string]interface{} {
	args := make(map[string]interface{})
	s = strings.TrimSpace(s)
	if s == "" {
		return args
	}

	for len(s) > 0 {
		// Find key (up to first ':')
		colonIdx := -1
		for i := 0; i < len(s); i++ {
			if s[i] == ':' {
				colonIdx = i
				break
			}
		}
		if colonIdx < 0 {
			break
		}
		key := strings.TrimSpace(s[:colonIdx])
		s = strings.TrimSpace(s[colonIdx+1:])

		// Parse value — could be quoted string, nested object, or bare token
		var val interface{}
		if len(s) == 0 {
			break
		}
		switch s[0] {
		case '"':
			// Quoted string — find closing quote
			end := 1
			for end < len(s) {
				if s[end] == '"' && s[end-1] != '\\' {
					break
				}
				end++
			}
			if end < len(s) {
				val = s[1:end]
				s = strings.TrimSpace(s[end+1:])
			} else {
				val = s[1:]
				s = ""
			}
		case '{':
			// Nested object — find matching '}'
			depth := 0
			end := 0
			for i := 0; i < len(s); i++ {
				switch s[i] {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						end = i
						goto foundBrace
					}
				}
			}
			end = len(s) - 1
		foundBrace:
			inner := strings.TrimSpace(s[1:end])
			val = parseArgs(inner)
			s = strings.TrimSpace(s[end+1:])
		default:
			// Bare token (number, enum, etc.) — read until comma or end
			end := len(s)
			for i := 0; i < len(s); i++ {
				if s[i] == ',' {
					end = i
					break
				}
			}
			val = strings.TrimSpace(s[:end])
			s = strings.TrimSpace(s[end:])
		}

		// Strip leading comma separator
		if len(s) > 0 && s[0] == ',' {
			s = strings.TrimSpace(s[1:])
		}

		args[key] = val
	}

	return args
}

// queryDepth counts the maximum nesting depth of braces in the query.
// Each `{` increments depth, each `}` decrements. Ignores quoted strings.
func queryDepth(query string) int {
	maxDepth := 0
	depth := 0
	inQuote := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '"' && (i == 0 || query[i-1] != '\\') {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch ch {
		case '{':
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		case '}':
			depth--
		}
	}
	return maxDepth
}

const graphiqlHTML = `<!DOCTYPE html>
<html><head><title>g-node GraphQL</title>
<link rel="stylesheet" href="https://unpkg.com/graphiql@3/graphiql.min.css"/>
</head><body style="margin:0;overflow:hidden">
<div id="graphiql" style="height:100vh"></div>
<script src="https://unpkg.com/react@18/umd/react.production.min.js" crossorigin></script>
<script src="https://unpkg.com/react-dom@18/umd/react-dom.production.min.js" crossorigin></script>
<script src="https://unpkg.com/graphiql@3/graphiql.min.js" crossorigin></script>
<script>
ReactDOM.createRoot(document.getElementById('graphiql')).render(
  React.createElement(GraphiQL, {
    fetcher: GraphiQL.createFetcher({url: '/graphql'}),
    defaultEditorToolsVisibility: true,
  })
);
</script></body></html>`
