package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// metrics holds application-level counters exposed at /metrics.
type metrics struct {
	queryCount  atomic.Int64
	queryErrors atomic.Int64
	blockHeight atomic.Int64
}

func (m *metrics) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP graph_query_count Total GraphQL queries processed.\n")
	fmt.Fprintf(w, "# TYPE graph_query_count counter\n")
	fmt.Fprintf(w, "graph_query_count %d\n", m.queryCount.Load())
	fmt.Fprintf(w, "# HELP graph_query_errors Total GraphQL queries that returned errors.\n")
	fmt.Fprintf(w, "# TYPE graph_query_errors counter\n")
	fmt.Fprintf(w, "graph_query_errors %d\n", m.queryErrors.Load())
	fmt.Fprintf(w, "# HELP graph_indexer_block_height Latest indexed block number.\n")
	fmt.Fprintf(w, "# TYPE graph_indexer_block_height gauge\n")
	fmt.Fprintf(w, "graph_indexer_block_height %d\n", m.blockHeight.Load())
}
