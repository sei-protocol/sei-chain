package parity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxResponseBytes bounds a JSON-RPC response body.
const maxResponseBytes = 64 << 20

// HTTPCaller returns a Caller that posts each request to the JSON-RPC endpoint
// at url.
func HTTPCaller(client *http.Client, url string) Caller {
	return func(ctx context.Context, method string, params []json.RawMessage) (Response, error) {
		if params == nil {
			params = []json.RawMessage{}
		}
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		if err != nil {
			return Response{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return Response{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return Response{}, err
		}
		defer func() { _ = res.Body.Close() }()
		bz, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
		if err != nil {
			return Response{}, err
		}
		var response Response
		if err := json.Unmarshal(bz, &response); err != nil {
			return Response{}, fmt.Errorf("decode %s response (HTTP %d): %w", method, res.StatusCode, err)
		}
		if response.Result == nil && response.Error == nil {
			return Response{}, fmt.Errorf("%s response (HTTP %d) has neither result nor error", method, res.StatusCode)
		}
		return response, nil
	}
}
