// graph -- standalone GraphQL query layer for any EVM chain.
//
// Connects to an RPC endpoint, indexes events into local SQLite/ZapDB,
// serves a GraphQL API compatible with The Graph subgraph queries.
// Also runs as a Lux VM (G-Chain) when loaded by luxd as a plugin.
//
// Standalone:
//
//	graph --rpc=http://node:8545                          # any EVM
//	graph --rpc=http://node:9650/ext/bc/{chainID}/rpc     # any Lux subnet
//	graph --config=subgraphs.yaml                         # multi-subgraph
//	graph --rpc=http://node:8545 --schema=uniswap-v2      # specific schema
//
// Scale horizontally: run N graph nodes behind a load balancer.
// Each indexes independently into its own local storage.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luxfi/graph/engine"
	"github.com/luxfi/graph/indexer"
	"github.com/luxfi/graph/storage"
)

var version = "dev"

func main() {
	var (
		rpcEndpoint = flag.String("rpc", "", "EVM JSON-RPC endpoint")
		httpAddr    = flag.String("http", ":8080", "GraphQL HTTP listen address")
		dataDir     = flag.String("data", "", "Data directory (default: ~/.graph/data)")
		schemaName  = flag.String("schema", "", "Built-in schema: amm, amm-v2, amm-v3, amm-v4, erc20, erc721, all")
		configFile  = flag.String("config", "", "Subgraph config YAML")
		showVersion = flag.Bool("version", false, "Show version")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("graph %s\n", version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if *rpcEndpoint == "" {
		*rpcEndpoint = os.Getenv("RPC_ENDPOINT")
	}
	if *rpcEndpoint == "" {
		fmt.Fprintln(os.Stderr, "graph: --rpc or RPC_ENDPOINT required")
		os.Exit(1)
	}

	if *dataDir == "" {
		*dataDir = envOr("DATA_DIR", filepath.Join(homeDir(), ".graph", "data"))
	}

	slog.Info("starting", "version", version, "rpc", *rpcEndpoint)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; cancel() }()

	// Storage: SQLite WAL (default) or in-memory (nosqlite build)
	store, err := storage.New(*dataDir)
	if err != nil {
		slog.Error("storage", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := store.Init(ctx); err != nil {
		slog.Error("storage init", "error", err)
		os.Exit(1)
	}

	// WAL streaming to S3 with PQ encryption (noop if REPLICATE_S3_ENDPOINT unset)
	stopReplicate := storage.StartReplicate(filepath.Join(*dataDir, "graph.db"))
	defer stopReplicate()

	// Indexer: subscribe to EVM events via RPC
	idx := indexer.New(*rpcEndpoint, store)
	go func() {
		if err := idx.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("indexer", "error", err)
			os.Exit(1)
		}
	}()

	// Query engine: GraphQL over indexed data
	eng := engine.New(store, &engine.Config{
		MaxQueryDepth:  10,
		MaxResultSize:  1 << 20, // 1MB
		QueryTimeoutMs: 30000,
	})

	// Load schema
	if *configFile != "" {
		if err := eng.LoadConfig(*configFile); err != nil {
			slog.Error("config", "error", err)
			os.Exit(1)
		}
	} else if *schemaName != "" {
		if err := eng.LoadBuiltin(*schemaName); err != nil {
			slog.Error("schema", "name", *schemaName, "error", err)
			os.Exit(1)
		}
	} else {
		// Default: full DEX schema (v2+v3 compatible)
		eng.LoadBuiltin("amm")
	}

	m := &metrics{}

	// All components mount under /v1/explorer. Override via GRAPH_PREFIX.
	prefix := os.Getenv("GRAPH_PREFIX")
	if prefix == "" {
		prefix = "/v1/explorer"
	}
	mux := http.NewServeMux()

	// GraphQL endpoint with MaxBytesReader (10MB) and metrics tracking
	mux.HandleFunc("POST "+prefix+"/graphql", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB
		m.queryCount.Add(1)
		m.blockHeight.Store(int64(idx.Status().LatestBlock))

		var req engine.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			m.queryErrors.Add(1)
			http.Error(w, `{"errors":[{"message":"invalid JSON"}]}`, http.StatusBadRequest)
			return
		}

		resp := eng.Execute(r.Context(), &req)
		if len(resp.Errors) > 0 {
			m.queryErrors.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("GET "+prefix+"/graphql", eng.HandleGraphiQL)

	mux.HandleFunc("GET "+prefix+"/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := idx.Status()
		fmt.Fprintf(w, `{"status":"ok","block":%d,"indexed":%d}`, status.LatestBlock, status.IndexedEvents)
	})

	mux.HandleFunc("GET "+prefix+"/ready", func(w http.ResponseWriter, r *http.Request) {
		status := idx.Status()
		w.Header().Set("Content-Type", "application/json")
		if status.LatestBlock == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"ready":false,"reason":"indexer has not processed any blocks"}`)
			return
		}
		fmt.Fprintf(w, `{"ready":true,"block":%d}`, status.LatestBlock)
	})

	mux.HandleFunc("GET "+prefix+"/metrics", m.handleMetrics)

	mux.HandleFunc("GET "+prefix+"/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"graph","version":"%s","graphql":"%s/graphql"}`, version, prefix)
	})

	// Middleware stack: security headers + CORS -> request logging -> rate limiting
	rl := newRateLimiter(100, 100) // 100 req/s per IP, burst 100
	handler := securityMiddleware(mux)
	handler = loggingMiddleware(handler)
	handler = rateLimitMiddleware(rl, handler)

	srv := &http.Server{
		Addr:              *httpAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
	go func() {
		slog.Info("listening", "addr", *httpAddr, "graphql", fmt.Sprintf("http://localhost%s/graphql", *httpAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	slog.Info("stopped")
}

// securityMiddleware adds CORS and security headers.
func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs every request with method, path, status, and duration.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sr, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sr.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", clientIP(r),
		)
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
