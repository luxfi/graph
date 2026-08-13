package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// How this package talks to a chain.
//
// One type, because "ask the node something" is one job. The quoter reads pool
// state, the approval endpoint reads an allowance, and both want the same
// thing: a batch of eth_calls at head, answered positionally. Two copies of
// that would be two places for a timeout, a status check, or a decode to
// differ, and the difference would show up as one endpoint working while its
// neighbour reports a revert that never happened.

// node is a chain's read side: an endpoint and the client that dials it.
type node struct {
	rpc    string
	client *http.Client
}

func dial(rpc string) *node {
	return &node{rpc: rpc, client: &http.Client{Timeout: quoteBudget}}
}

type ethCall struct {
	To   string
	Data string
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

func ethCallReq(id int, to, data string) rpcRequest {
	return rpcRequest{JSONRPC: "2.0", ID: id, Method: "eth_call",
		Params: []any{map[string]string{"to": to, "data": data}, "latest"}}
}

// call runs a batch of eth_calls at head and returns each result positionally.
func (n *node) call(ctx context.Context, calls []ethCall) ([]string, error) {
	reqs := make([]rpcRequest, len(calls))
	for i, c := range calls {
		reqs[i] = ethCallReq(i, c.To, c.Data)
	}
	return n.batch(ctx, reqs)
}

// batch sends one JSON-RPC batch and returns each result positionally by id.
//
// A call that reverted comes back empty, and that is an answer — "this contract
// does not do that" is exactly how a pool's venue is settled. A batch that does
// not come back at all is an error, because then nothing is known and an empty
// result would be read as a revert that never happened.
func (n *node) batch(ctx context.Context, reqs []rpcRequest) ([]string, error) {
	out := make([]string, len(reqs))
	if len(reqs) == 0 {
		return out, nil
	}
	body, err := json.Marshal(reqs)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.rpc, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc: %s", resp.Status)
	}

	var results []struct {
		ID     int    `json:"id"`
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	for _, r := range results {
		if r.ID >= 0 && r.ID < len(out) {
			out[r.ID] = r.Result
		}
	}
	return out, nil
}
