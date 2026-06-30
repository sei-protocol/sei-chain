package evmrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
)

const (
	invalidRequestCode  = -32600
	seiLegacyNotEnabled = -32601
	internalErrorCode   = -32603
)

// wrapSeiLegacyHTTP wraps the EVM JSON-RPC HTTP handler to enforce [evm].enabled_legacy_sei_apis for
// gated sei_* and sei2_* methods. Disallowed calls get a JSON-RPC error without invoking the inner handler.
// Single-object allowed calls pass through unchanged; batches forward a filtered subset and merge inner
// results back by JSON-RPC id. Deprecation header on successful forwards of gated methods. nil allowlist = no wrap.
//
// maxBody bounds the request body the gate buffers before JSON-RPC parsing. It must match the configured
// per-request cap (rpc.Server.SetHTTPBodyLimit / requestSizeLimiter) so the gate never truncates a body the
// inner stack would otherwise accept; maxBody <= 0 falls back to defaultMaxRequestBodyBytes (the 5MiB
// go-ethereum default).
func wrapSeiLegacyHTTP(inner http.Handler, allowlist map[string]struct{}, maxBody int64) http.Handler {
	if allowlist == nil {
		return inner
	}
	if maxBody <= 0 {
		maxBody = defaultMaxRequestBodyBytes
	}
	return &seiLegacyHTTPGate{inner: inner, allowlist: allowlist, maxBody: maxBody}
}

type seiLegacyHTTPGate struct {
	inner     http.Handler
	allowlist map[string]struct{}
	maxBody   int64
}

func (g *seiLegacyHTTPGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Read maxBody+1 so an over-limit body is rejected with 413, not silently truncated to
	// maxBody and forwarded. The outer MaxBytesReader trips here for chunked bodies; the length
	// check below covers the gate running standalone. Both return 413.
	body, err := io.ReadAll(io.LimitReader(r.Body, g.maxBody+1))
	_ = r.Body.Close()
	if err != nil {
		if maxErr := (*http.MaxBytesError)(nil); errors.As(err, &maxErr) {
			recordRequestRejected(r.Context(), rejectReasonOversize)
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if int64(len(body)) > g.maxBody {
		recordRequestRejected(r.Context(), rejectReasonOversize)
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	trim := bytes.TrimSpace(body)
	if len(trim) > 0 && trim[0] == '[' {
		g.handleBatch(w, r, body)
		return
	}
	g.handleSingle(w, r, body)
}

func (g *seiLegacyHTTPGate) serveInnerWithBody(w http.ResponseWriter, r *http.Request, body []byte) {
	sub := r.Clone(r.Context())
	sub.Body = io.NopCloser(bytes.NewReader(body))
	sub.ContentLength = int64(len(body))
	sub.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	g.inner.ServeHTTP(w, sub)
}

func orNullID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage(`null`)
	}
	return id
}

func (g *seiLegacyHTTPGate) handleSingle(w http.ResponseWriter, r *http.Request, body []byte) {
	var msg struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		g.serveInnerWithBody(w, r, body)
		return
	}
	if err := seiLegacyGateError(msg.Method, g.allowlist); err != nil {
		writeSeiLegacyBlocked(w, orNullID(msg.ID), err)
		return
	}
	// Non-gated methods (eth_*, web3_*, net_*, etc.) need no recording or
	// header injection — pass straight through so the gzip handler writes
	// directly to the real http.ResponseWriter.
	if !seiLegacyIsGatedNamespaceMethod(msg.Method) {
		g.serveInnerWithBody(w, r, body)
		return
	}
	rec := httptest.NewRecorder()
	sub := r.Clone(r.Context())
	sub.Body = io.NopCloser(bytes.NewReader(body))
	sub.ContentLength = int64(len(body))
	// Prevent the inner gzip handler from compressing into the recorder;
	// we need plain JSON so copyHTTPHeader does not propagate a stale
	// Content-Encoding: gzip for a body that is replayed uncompressed.
	sub.Header.Del("Accept-Encoding")
	sub.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	g.inner.ServeHTTP(rec, sub)
	if seiLegacyForwardedGatedMethod(msg.Method, g.allowlist) {
		rec.Header().Set(SeiLegacyDeprecationHTTPHeader, SeiLegacyDeprecationMessage)
	}
	copyHTTPHeader(w.Header(), rec.Header())
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

type jsonrpcMessage struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id"`
}

// logic taken from hasValidID in https://github.com/ethereum/go-ethereum/blob/master/rpc/json.go
// null is valid: it is used in error responses per JSON-RPC 2.0 §5
func (m *jsonrpcMessage) hasValidID() bool {
	return len(m.ID) > 0 && m.ID[0] != '{' && m.ID[0] != '['
}

func (g *seiLegacyHTTPGate) handleBatch(w http.ResponseWriter, r *http.Request, body []byte) {
	var msgs []json.RawMessage
	if err := json.Unmarshal(body, &msgs); err != nil {
		g.serveInnerWithBody(w, r, body)
		return
	}
	if len(msgs) == 0 {
		g.serveInnerWithBody(w, r, body)
		return
	}
	methods := make([]string, len(msgs))
	ids := make([]json.RawMessage, len(msgs))
	invalidReq := make([]bool, len(msgs))
	blocked := make([]bool, len(msgs))
	blockedErr := make([]error, len(msgs))
	for i, raw := range msgs {
		var msg jsonrpcMessage
		if err := json.Unmarshal(raw, &msg); err != nil || (!isJSONRPCNotificationID(msg.ID) && !msg.hasValidID()) {
			// Batch element is not a JSON object, or has an invalid (object/array) id; synthesize -32600 and do not forward.
			invalidReq[i] = true
			continue
		}
		methods[i] = msg.Method
		ids[i] = msg.ID
		if err := seiLegacyGateError(methods[i], g.allowlist); err != nil {
			blocked[i] = true
			blockedErr[i] = err
		}
	}

	var forward []json.RawMessage
	synthIDs := make([]json.RawMessage, len(msgs))
	forwardLegacy := false
	synthCounter := 0
	for i := range msgs {
		if invalidReq[i] || blocked[i] {
			continue
		}
		msg := msgs[i]
		if !isJSONRPCNotificationID(ids[i]) {
			sid := json.RawMessage(strconv.AppendInt(nil, int64(synthCounter), 10))
			synthIDs[i] = sid
			synthCounter++
			msg = setJSONObjectID(msg, sid)
		}
		forward = append(forward, msg)
		if !forwardLegacy && seiLegacyForwardedGatedMethod(methods[i], g.allowlist) {
			forwardLegacy = true
		}
	}
	if len(forward) == 0 {
		outArr := seiLegacyBatchResponsesNoForward(invalidReq, blockedErr, ids, len(msgs))
		writeJSONRPCBatchResponse(w, http.StatusOK, outArr)
		return
	}
	forwardBody, err := json.Marshal(forward)
	if err != nil {
		g.serveInnerWithBody(w, r, body)
		return
	}

	// Fast path: every element is forwarded (nothing blocked/invalid) and none
	// are gated sei_*/sei2_* methods.  Skip the recorder so the gzip handler
	// writes directly to the real http.ResponseWriter — same fix as handleSingle.
	allForwarded := len(forward) == len(msgs)
	if allForwarded && !forwardLegacy {
		g.serveInnerWithBody(w, r, body)
		return
	}

	rec := httptest.NewRecorder()
	sub := r.Clone(r.Context())
	sub.Body = io.NopCloser(bytes.NewReader(forwardBody))
	sub.ContentLength = int64(len(forwardBody))
	// Prevent the inner gzip handler from compressing into the recorder;
	// mergeSeiLegacyHTTPBatch needs plain JSON to unmarshal inner results.
	sub.Header.Del("Accept-Encoding")
	sub.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(forwardBody)), nil
	}
	g.inner.ServeHTTP(rec, sub)
	outArr := mergeSeiLegacyHTTPBatch(invalidReq, blocked, blockedErr, ids, synthIDs, len(msgs), rec.Body.Bytes())
	copyHTTPHeader(w.Header(), rec.Header())
	if forwardLegacy {
		w.Header().Set(SeiLegacyDeprecationHTTPHeader, SeiLegacyDeprecationMessage)
	}
	writeJSONRPCBatchResponse(w, rec.Code, outArr)
}

const seiLegacyBatchInternalErr = "invalid or incomplete JSON-RPC batch response from server"

const seiLegacyBatchInvalidReqMsg = "Invalid Request"

// seiLegacyBatchResponsesNoForward builds the batch JSON-RPC response when nothing is forwarded to the
// inner server: invalid slots yield -32600, blocked gated methods yield gate errors, and notifications
// (requests with no "id" member) are omitted (JSON-RPC 2.0: no response for notifications, including in batches).
func seiLegacyBatchResponsesNoForward(
	invalidReq []bool,
	blockedErr []error,
	ids []json.RawMessage,
	lenMsgs int,
) []json.RawMessage {
	out := make([]json.RawMessage, 0, lenMsgs)
	for i := range lenMsgs {
		if invalidReq[i] {
			out = append(out, marshalJSONRPCError(orNullID(ids[i]), invalidRequestCode, seiLegacyBatchInvalidReqMsg))
			continue
		}
		if isJSONRPCNotificationID(ids[i]) {
			continue
		}
		out = append(out, marshalBlockedResponse(orNullID(ids[i]), blockedErr[i]))
	}
	return out
}

// mergeSeiLegacyHTTPBatch merges inner batch results with gate/invalid slots. Output is ordered like the
// original batch but omits entries for JSON-RPC notifications (no "id" member), per JSON-RPC 2.0 batch rules.
// synthIDs holds the unique synthetic ID assigned to each forwarded non-notification request (nil for all
// others). The inner server echoes these synthetic IDs back, so idToIdx is always collision-free regardless
// of duplicate or null original IDs. Original IDs are restored in the output via patchJSONRPCResponseIDIfNeeded.
func mergeSeiLegacyHTTPBatch(
	invalidReq []bool,
	blocked []bool,
	blockedErr []error,
	ids []json.RawMessage,
	synthIDs []json.RawMessage,
	lenMsgs int,
	innerBody []byte,
) []json.RawMessage {
	appendMergeFailure := func() []json.RawMessage {
		out := make([]json.RawMessage, 0, lenMsgs)
		for i := 0; i < lenMsgs; i++ {
			switch {
			case invalidReq[i]:
				out = append(out, json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), invalidRequestCode, seiLegacyBatchInvalidReqMsg)))
			case isJSONRPCNotificationID(ids[i]):
				continue
			case blocked[i]:
				out = append(out, json.RawMessage(marshalBlockedResponse(orNullID(ids[i]), blockedErr[i])))
			default:
				out = append(out, json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), internalErrorCode, seiLegacyBatchInternalErr)))
			}
		}
		return out
	}

	var unmarshalledInnerBody []json.RawMessage
	if err := json.Unmarshal(innerBody, &unmarshalledInnerBody); err != nil {
		return appendMergeFailure()
	}

	entries := make([]json.RawMessage, len(unmarshalledInnerBody))
	idToIdx := make(map[string]int, len(unmarshalledInnerBody))
	for j, raw := range unmarshalledInnerBody {
		idRaw, hasKey, err := jsonRPCObjectIDKey(raw)
		if err != nil {
			// Skip malformed inner entry; the matching slot will fall through to
			// the internalErrorCode branch in the merge loop below.
			continue
		}
		entries[j] = raw
		if !hasKey || isJSONRPCNotificationID(idRaw) {
			continue
		}
		k := rpcIDKey(idRaw)
		if _, ok := idToIdx[k]; !ok {
			idToIdx[k] = j
		}
	}

	out := make([]json.RawMessage, 0, lenMsgs)
	used := make([]bool, len(unmarshalledInnerBody))
	for i := 0; i < lenMsgs; i++ {
		if invalidReq[i] {
			out = append(out, marshalJSONRPCError(orNullID(ids[i]), invalidRequestCode, seiLegacyBatchInvalidReqMsg))
			continue
		}
		if isJSONRPCNotificationID(ids[i]) {
			continue
		}
		if blocked[i] {
			out = append(out, marshalBlockedResponse(orNullID(ids[i]), blockedErr[i]))
			continue
		}
		k := rpcIDKey(synthIDs[i])
		idx, ok := idToIdx[k]
		if !ok || used[idx] {
			out = append(out, marshalJSONRPCError(orNullID(ids[i]), internalErrorCode, seiLegacyBatchInternalErr))
			continue
		}
		used[idx] = true
		out = append(out, patchJSONRPCResponseIDIfNeeded(entries[idx], ids[i]))
	}

	return out
}

func rpcIDKey(id json.RawMessage) string {
	return string(bytes.TrimSpace(id))
}

// isJSONRPCNotificationID is true when the request omits "id" (JSON-RPC Notification).
// "id": null is discouraged but valid per JSON-RPC 2.0 and MUST receive a response like any other id.
func isJSONRPCNotificationID(id json.RawMessage) bool {
	return len(id) == 0
}

func jsonRPCObjectIDKey(raw json.RawMessage) (idField json.RawMessage, hasKey bool, err error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false, err
	}
	idField, hasKey = m["id"]
	return idField, hasKey, nil
}

func marshalJSONRPCError(id json.RawMessage, code int, message string) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
	return b
}

// patchJSONRPCResponseIDIfNeeded replaces response "id" with the client's id as raw JSON (avoids float64 rounding).
func patchJSONRPCResponseIDIfNeeded(resp json.RawMessage, wantID json.RawMessage) []byte {
	if isJSONRPCNotificationID(wantID) {
		return resp
	}
	got, has, err := jsonRPCObjectIDKey(resp)
	if err != nil || !has {
		return resp
	}
	wantTrimmed := bytes.TrimSpace(wantID)
	if bytes.Equal(bytes.TrimSpace(got), wantTrimmed) {
		return resp
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(resp, &m); err != nil {
		return resp
	}
	m["id"] = json.RawMessage(bytes.Clone(wantTrimmed))
	b, err := json.Marshal(m)
	if err != nil {
		return resp
	}
	return b
}

func copyHTTPHeader(dst, src http.Header) {
	for k, vv := range src {
		dst.Del(k)
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// writeJSONRPCBatchResponse writes a JSON-RPC batch response. Per JSON-RPC 2.0, if there are no
// response objects, the server must not return an empty JSON array — use an empty HTTP body instead.
func writeJSONRPCBatchResponse(w http.ResponseWriter, code int, arr []json.RawMessage) {
	if len(arr) == 0 {
		w.WriteHeader(code)
		return
	}
	writeJSONArrayResponse(w, code, arr)
}

// setJSONObjectID returns obj with its "id" field replaced by newID.
// Returns obj unchanged if it cannot be parsed as a JSON object.
func setJSONObjectID(obj json.RawMessage, newID json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(obj, &m); err != nil {
		return obj
	}
	m["id"] = newID
	b, err := json.Marshal(m)
	if err != nil {
		return obj
	}
	return b
}

func writeJSONArrayResponse(w http.ResponseWriter, code int, arr []json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(arr)
}

func writeSeiLegacyBlocked(w http.ResponseWriter, id json.RawMessage, gateErr error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(marshalBlockedResponse(id, gateErr))
}

func marshalBlockedResponse(id json.RawMessage, gateErr error) []byte {
	e, ok := gateErr.(*errSeiLegacyNotEnabled)
	if !ok {
		fallback, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]interface{}{
				"code":    internalErrorCode,
				"message": gateErr.Error(),
			},
		})
		return fallback
	}
	m := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    e.ErrorCode(),
			"message": e.Error(),
			"data":    e.ErrorData(),
		},
	}
	b, _ := json.Marshal(m)
	return b
}
