package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	jsonRPCParseError       = -32700
	jsonRPCInvalidRequest   = -32600
	jsonRPCUnsupportedError = -32000
	jsonRPCUpstreamError    = -32001
	rpcRouteHeader          = "Sei-RPC-Route"
	maxBlockReferenceDepth  = 16
)

var blockParameterIndexes = map[string]int{
	"debug_getRawBlock":                          0,
	"debug_getRawHeader":                         0,
	"debug_getRawReceipts":                       0,
	"debug_traceBlockByNumber":                   0,
	"debug_traceCall":                            1,
	"eth_call":                                   1,
	"eth_createAccessList":                       1,
	"eth_estimateGas":                            1,
	"eth_estimateGasAfterCalls":                  2,
	"eth_getBalance":                             1,
	"eth_getBlockByNumber":                       0,
	"eth_getBlockReceipts":                       0,
	"eth_getBlockTransactionCountByNumber":       0,
	"eth_getCode":                                1,
	"eth_getProof":                               2,
	"eth_getRawTransactionByBlockNumberAndIndex": 0,
	"eth_getStorageAt":                           2,
	"eth_getTransactionByBlockNumberAndIndex":    0,
	"eth_getTransactionCount":                    1,
	"eth_getUncleByBlockNumberAndIndex":          0,
	"eth_getUncleCountByBlockNumber":             0,
}

type router struct {
	live               *upstream
	frozen             []*upstream
	client             *http.Client
	maxRequestBodySize int64
	liveProxy          *httputil.ReverseProxy
}

type upstream struct {
	freezeHeight uint64
	endpoint     *url.URL
}

type rpcCall struct {
	raw     json.RawMessage
	method  string
	params  json.RawMessage
	id      json.RawMessage
	hasID   bool
	isValid bool
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   rpcError        `json:"error"`
}

type blockReference struct {
	height uint64
	known  bool
	live   bool
}

type batchGroup struct {
	upstream  *upstream
	calls     []rpcCall
	responses []json.RawMessage
	err       error
}

func newRouter(liveAddress string, frozenConfigs []frozenNodeConfig, client *http.Client, maxRequestBodySize int64) (*router, error) {
	liveURL, err := parseEndpoint(liveAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid live node: %w", err)
	}
	if client == nil {
		client = &http.Client{}
	}
	if maxRequestBodySize <= 0 {
		return nil, errors.New("maximum request body size must be positive")
	}

	live := &upstream{endpoint: liveURL}
	frozen := make([]*upstream, 0, len(frozenConfigs))
	seenHeights := make(map[uint64]struct{}, len(frozenConfigs))
	for _, cfg := range frozenConfigs {
		if cfg.freezeHeight == 0 {
			return nil, errors.New("freeze height must be positive")
		}
		if _, exists := seenHeights[cfg.freezeHeight]; exists {
			return nil, fmt.Errorf("duplicate freeze height %d", cfg.freezeHeight)
		}
		seenHeights[cfg.freezeHeight] = struct{}{}
		endpoint, err := parseEndpoint(cfg.address)
		if err != nil {
			return nil, fmt.Errorf("invalid frozen node at height %d: %w", cfg.freezeHeight, err)
		}
		frozen = append(frozen, &upstream{freezeHeight: cfg.freezeHeight, endpoint: endpoint})
	}
	sort.Slice(frozen, func(i, j int) bool {
		return frozen[i].freezeHeight < frozen[j].freezeHeight
	})

	liveProxy := httputil.NewSingleHostReverseProxy(liveURL)
	liveProxy.Transport = client.Transport
	return &router{
		live:               live,
		frozen:             frozen,
		client:             client,
		maxRequestBodySize: maxRequestBodySize,
		liveProxy:          liveProxy,
	}, nil
}

func parseEndpoint(address string) (*url.URL, error) {
	address = strings.TrimSpace(address)
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	endpoint, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("scheme must be http or https")
	}
	if endpoint.Host == "" {
		return nil, errors.New("address must include a host")
	}
	if endpoint.Fragment != "" {
		return nil, errors.New("address must not include a fragment")
	}
	return endpoint, nil
}

func (r *router) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		r.liveProxy.ServeHTTP(w, request)
		return
	}

	request.Body = http.MaxBytesReader(w, request.Body, r.maxRequestBodySize)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		r.serveBatch(w, request, body)
		return
	}
	r.serveSingle(w, request, body)
}

func (r *router) serveSingle(w http.ResponseWriter, request *http.Request, body []byte) {
	call, err := decodeCall(body)
	if err != nil {
		if json.Valid(body) {
			writeRPCError(w, nil, rpcError{Code: jsonRPCInvalidRequest, Message: "invalid request"})
		} else {
			writeRPCError(w, nil, rpcError{Code: jsonRPCParseError, Message: "parse error"})
		}
		return
	}
	if !call.isValid {
		writeRPCError(w, nil, rpcError{Code: jsonRPCInvalidRequest, Message: "invalid request"})
		return
	}
	target, routingErr := r.route(call)
	if routingErr != nil {
		if call.hasID {
			writeRPCError(w, call.id, *routingErr)
		}
		return
	}
	if err := r.proxy(w, request, target, body); err != nil {
		if call.hasID {
			writeRPCError(w, call.id, rpcError{Code: jsonRPCUpstreamError, Message: "upstream request failed"})
		}
	}
}

func (r *router) serveBatch(w http.ResponseWriter, request *http.Request, body []byte) {
	var rawCalls []json.RawMessage
	if err := json.Unmarshal(body, &rawCalls); err != nil {
		writeRPCError(w, nil, rpcError{Code: jsonRPCParseError, Message: "parse error"})
		return
	}
	if len(rawCalls) == 0 {
		writeRPCError(w, nil, rpcError{Code: jsonRPCInvalidRequest, Message: "invalid request"})
		return
	}

	calls := make([]rpcCall, 0, len(rawCalls))
	for _, raw := range rawCalls {
		call, err := decodeCall(raw)
		if err != nil {
			call = rpcCall{raw: raw}
		}
		calls = append(calls, call)
	}

	if target, ok := r.singleBatchTarget(calls); ok {
		if err := r.proxy(w, request, target, body); err != nil {
			writeBatchUpstreamErrors(w, calls)
		}
		return
	}

	groups, localResponses := r.groupBatch(calls)
	r.fetchBatchGroups(request, groups)
	responses := append([]json.RawMessage(nil), localResponses...)
	for _, group := range groups {
		if group.err != nil {
			responses = append(responses, upstreamErrorResponses(group.calls)...)
			continue
		}
		responses = append(responses, group.responses...)
	}
	w.Header().Set(rpcRouteHeader, "mixed")
	writeBatchResponses(w, responses)
}

func decodeCall(raw json.RawMessage) (rpcCall, error) {
	call := rpcCall{raw: raw}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return call, err
	}
	if fields == nil {
		return call, nil
	}
	methodRaw, ok := fields["method"]
	if !ok || json.Unmarshal(methodRaw, &call.method) != nil || call.method == "" {
		return call, nil
	}
	call.params = fields["params"]
	call.id, call.hasID = fields["id"]
	call.isValid = true
	return call, nil
}

func (r *router) singleBatchTarget(calls []rpcCall) (*upstream, bool) {
	var target *upstream
	for _, call := range calls {
		if !call.isValid {
			return nil, false
		}
		callTarget, routingErr := r.route(call)
		if routingErr != nil {
			return nil, false
		}
		if target == nil {
			target = callTarget
			continue
		}
		if target != callTarget {
			return nil, false
		}
	}
	return target, target != nil
}

func (r *router) groupBatch(calls []rpcCall) ([]*batchGroup, []json.RawMessage) {
	groupsByTarget := make(map[*upstream]*batchGroup)
	groups := make([]*batchGroup, 0)
	localResponses := make([]json.RawMessage, 0)
	for _, call := range calls {
		if !call.isValid {
			localResponses = append(localResponses, marshalRPCError(nil, rpcError{Code: jsonRPCInvalidRequest, Message: "invalid request"}))
			continue
		}
		target, routingErr := r.route(call)
		if routingErr != nil {
			if call.hasID {
				localResponses = append(localResponses, marshalRPCError(call.id, *routingErr))
			}
			continue
		}
		group := groupsByTarget[target]
		if group == nil {
			group = &batchGroup{upstream: target}
			groupsByTarget[target] = group
			groups = append(groups, group)
		}
		group.calls = append(group.calls, call)
	}
	return groups, localResponses
}

func (r *router) fetchBatchGroups(request *http.Request, groups []*batchGroup) {
	var wg sync.WaitGroup
	for _, group := range groups {
		wg.Add(1)
		go func(group *batchGroup) {
			defer wg.Done()
			payload := make([]json.RawMessage, 0, len(group.calls))
			for _, call := range group.calls {
				payload = append(payload, call.raw)
			}
			body, err := json.Marshal(payload)
			if err != nil {
				group.err = err
				return
			}
			responseBody, err := r.callUpstream(request, group.upstream, body)
			if err != nil {
				group.err = err
				return
			}
			group.responses, group.err = decodeBatchResponses(responseBody)
		}(group)
	}
	wg.Wait()
}

func decodeBatchResponses(body []byte) ([]json.RawMessage, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var responses []json.RawMessage
	if err := json.Unmarshal(body, &responses); err == nil {
		return responses, nil
	}
	var response json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, errors.New("upstream returned invalid JSON")
	}
	return []json.RawMessage{response}, nil
}

func (r *router) route(call rpcCall) (*upstream, *rpcError) {
	switch call.method {
	case "eth_getLogs":
		return r.routeGetLogs(call.params)
	case "eth_feeHistory":
		return r.routeFeeHistory(call.params)
	default:
		parameterIndex, ok := blockParameterIndexes[call.method]
		if !ok {
			return r.live, nil
		}
		parameter, ok := positionalParameter(call.params, parameterIndex)
		if !ok {
			return r.live, nil
		}
		return r.upstreamForReference(parseBlockReference(parameter)), nil
	}
}

func (r *router) routeGetLogs(params json.RawMessage) (*upstream, *rpcError) {
	filterRaw, ok := positionalParameter(params, 0)
	if !ok {
		return r.live, nil
	}
	var filter map[string]json.RawMessage
	if json.Unmarshal(filterRaw, &filter) != nil || filter == nil {
		return r.live, nil
	}
	if blockHash, exists := filter["blockHash"]; exists && !bytes.Equal(bytes.TrimSpace(blockHash), []byte("null")) {
		return r.live, nil
	}

	fromRaw, hasFrom := filter["fromBlock"]
	toRaw, hasTo := filter["toBlock"]
	if !hasFrom && hasTo {
		fromRaw = toRaw
		hasFrom = true
	}
	from := blockReference{live: true}
	to := blockReference{live: true}
	if hasFrom {
		from = parseBlockReference(fromRaw)
	}
	if hasTo {
		to = parseBlockReference(toRaw)
	}
	return r.routeRange(from, to)
}

func (r *router) routeFeeHistory(params json.RawMessage) (*upstream, *rpcError) {
	countRaw, hasCount := positionalParameter(params, 0)
	lastRaw, hasLast := positionalParameter(params, 1)
	if !hasCount || !hasLast {
		return r.live, nil
	}
	count, ok := parseQuantity(countRaw)
	if !ok {
		return r.live, nil
	}
	last := parseBlockReference(lastRaw)
	if !last.known || last.live {
		return r.live, nil
	}
	firstHeight := last.height
	if count > 1 {
		if count-1 > last.height {
			firstHeight = 0
		} else {
			firstHeight = last.height - count + 1
		}
	}
	return r.routeRange(blockReference{height: firstHeight, known: true}, last)
}

func (r *router) routeRange(from, to blockReference) (*upstream, *rpcError) {
	if (!from.known && !from.live) || (!to.known && !to.live) {
		return r.live, nil
	}
	if from.known && to.known && from.height > to.height {
		return r.live, nil
	}
	fromTarget := r.upstreamForReference(from)
	toTarget := r.upstreamForReference(to)
	if fromTarget != toTarget {
		return nil, &rpcError{
			Code:    jsonRPCUnsupportedError,
			Message: "block ranges spanning multiple frozen-node intervals are not supported",
		}
	}
	return fromTarget, nil
}

func (r *router) upstreamForReference(reference blockReference) *upstream {
	if !reference.known || reference.live {
		return r.live
	}
	for _, frozen := range r.frozen {
		if reference.height < frozen.freezeHeight {
			return frozen
		}
	}
	return r.live
}

func parseBlockReference(raw json.RawMessage) blockReference {
	for depth := 0; depth <= maxBlockReferenceDepth; depth++ {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			return blockReference{}
		}
		if trimmed[0] == '{' {
			if depth == maxBlockReferenceDepth {
				return blockReference{}
			}
			var object map[string]json.RawMessage
			if json.Unmarshal(trimmed, &object) != nil {
				return blockReference{}
			}
			blockNumber, ok := object["blockNumber"]
			if !ok {
				return blockReference{}
			}
			raw = blockNumber
			continue
		}

		var value string
		if json.Unmarshal(trimmed, &value) != nil {
			return blockReference{}
		}
		switch value {
		case "earliest":
			return blockReference{height: 0, known: true}
		case "latest", "pending", "safe", "finalized":
			return blockReference{live: true}
		}
		height, err := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 64)
		if err != nil || !strings.HasPrefix(value, "0x") || height > math.MaxInt64 {
			return blockReference{}
		}
		return blockReference{height: height, known: true}
	}
	return blockReference{}
}

func parseQuantity(raw json.RawMessage) (uint64, bool) {
	trimmed := bytes.TrimSpace(raw)
	var value string
	if json.Unmarshal(trimmed, &value) == nil {
		if !strings.HasPrefix(value, "0x") {
			return 0, false
		}
		quantity, err := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 64)
		return quantity, err == nil
	}
	var quantity uint64
	if json.Unmarshal(trimmed, &quantity) != nil {
		return 0, false
	}
	return quantity, true
}

func positionalParameter(params json.RawMessage, index int) (json.RawMessage, bool) {
	if len(params) == 0 {
		return nil, false
	}
	var values []json.RawMessage
	if json.Unmarshal(params, &values) != nil || index < 0 || index >= len(values) {
		return nil, false
	}
	return values[index], true
}

func (r *router) proxy(w http.ResponseWriter, request *http.Request, target *upstream, body []byte) error {
	upstreamRequest, err := r.newUpstreamRequest(request, target, body)
	if err != nil {
		return err
	}
	response, err := r.client.Do(upstreamRequest)
	if err != nil {
		return err
	}

	copyResponseHeaders(w.Header(), response.Header)
	w.Header().Set(rpcRouteHeader, target.routeName())
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
	_ = response.Body.Close()
	return nil
}

func (u *upstream) routeName() string {
	if u.freezeHeight == 0 {
		return "live"
	}
	return fmt.Sprintf("frozen:%d", u.freezeHeight)
}

func (r *router) callUpstream(request *http.Request, target *upstream, body []byte) ([]byte, error) {
	upstreamRequest, err := r.newUpstreamRequest(request, target, body)
	if err != nil {
		return nil, err
	}
	response, err := r.client.Do(upstreamRequest)
	if err != nil {
		return nil, err
	}
	responseBody, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
	}
	return responseBody, nil
}

func (r *router) newUpstreamRequest(request *http.Request, target *upstream, body []byte) (*http.Request, error) {
	endpoint := joinedURL(target.endpoint, request.URL)
	upstreamRequest, err := http.NewRequestWithContext(request.Context(), request.Method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	upstreamRequest.Header = request.Header.Clone()
	removeHopByHopHeaders(upstreamRequest.Header)
	upstreamRequest.Header.Del("Accept-Encoding")
	if host := clientIP(request.RemoteAddr); host != "" {
		prior := upstreamRequest.Header.Get("X-Forwarded-For")
		if prior != "" {
			host = prior + ", " + host
		}
		upstreamRequest.Header.Set("X-Forwarded-For", host)
	}
	return upstreamRequest, nil
}

func joinedURL(base, incoming *url.URL) *url.URL {
	target := *base
	if incoming.Path != "" && incoming.Path != "/" {
		target.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(incoming.Path, "/")
		target.RawPath = ""
	}
	if target.RawQuery == "" {
		target.RawQuery = incoming.RawQuery
	} else if incoming.RawQuery != "" {
		target.RawQuery += "&" + incoming.RawQuery
	}
	return &target
}

func clientIP(remoteAddress string) string {
	if index := strings.LastIndex(remoteAddress, ":"); index >= 0 {
		return strings.Trim(remoteAddress[:index], "[]")
	}
	return remoteAddress
}

func removeHopByHopHeaders(header http.Header) {
	for _, name := range strings.Split(header.Get("Connection"), ",") {
		header.Del(strings.TrimSpace(name))
	}
	for _, name := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		header.Del(name)
	}
}

func copyResponseHeaders(destination, source http.Header) {
	cloned := source.Clone()
	removeHopByHopHeaders(cloned)
	for name, values := range cloned {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, rpcErr rpcError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(marshalRPCError(id, rpcErr))
}

func marshalRPCError(id json.RawMessage, rpcErr rpcError) json.RawMessage {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	response, _ := json.Marshal(rpcErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcErr,
	})
	return response
}

func writeBatchUpstreamErrors(w http.ResponseWriter, calls []rpcCall) {
	writeBatchResponses(w, upstreamErrorResponses(calls))
}

func upstreamErrorResponses(calls []rpcCall) []json.RawMessage {
	responses := make([]json.RawMessage, 0, len(calls))
	for _, call := range calls {
		if call.hasID {
			responses = append(responses, marshalRPCError(call.id, rpcError{
				Code:    jsonRPCUpstreamError,
				Message: "upstream request failed",
			}))
		}
	}
	return responses
}

func writeBatchResponses(w http.ResponseWriter, responses []json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if len(responses) == 0 {
		return
	}
	response, _ := json.Marshal(responses)
	_, _ = w.Write(response)
}
