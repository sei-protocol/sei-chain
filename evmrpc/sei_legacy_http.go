package evmrpc

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
)

// seiLegacyHTTPMaxBody matches github.com/ethereum/go-ethereum/rpc.defaultBodyLimit (5MiB), the
// default HTTP request body cap used by rpc.Server before ServeHTTP. The legacy gate must not
// read more than the inner JSON-RPC stack will accept (see rpc.Server.SetHTTPBodyLimit).
const seiLegacyHTTPMaxBody = 5 * 1024 * 1024

// wrapSeiLegacyHTTP wraps the EVM JSON-RPC HTTP handler to enforce [evm].enabled_legacy_sei_apis for
// gated sei_* and sei2_* methods. Disallowed calls get a JSON-RPC error without invoking the inner handler;
// allowed calls pass through unchanged. Optional deprecation: HTTP header SeiLegacyDeprecationHTTPHeader
// on successful forwards (no JSON body mutation). allowlist nil disables the wrapper.
func wrapSeiLegacyHTTP(inner http.Handler, allowlist map[string]struct{}) http.Handler {
	if allowlist == nil {
		return inner
	}
	return &seiLegacyHTTPGate{inner: inner, allowlist: allowlist}
}

type seiLegacyHTTPGate struct {
	inner     http.Handler
	allowlist map[string]struct{}
}

func (g *seiLegacyHTTPGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Read the body once; delegate JSON-RPC validation to the inner handler. We only intercept
	// when we can parse JSON-RPC and the method is a gated sei_* / sei2_* name.
	body, err := io.ReadAll(io.LimitReader(r.Body, seiLegacyHTTPMaxBody))
	_ = r.Body.Close()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	rec := httptest.NewRecorder()
	sub := r.Clone(r.Context())
	sub.Body = io.NopCloser(bytes.NewReader(body))
	sub.ContentLength = int64(len(body))
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
	for i, raw := range msgs {
		var msg struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			// JSON-RPC batch entries must be objects. Non-objects (e.g. 42, "x", [1]) are invalid;
			// answer with -32600 here and do not forward them — avoids skipping the gate for
			// siblings and avoids relying on the inner server to reject the slot.
			invalidReq[i] = true
			continue
		}
		methods[i] = msg.Method
		ids[i] = msg.ID
	}
	blocked := make([]bool, len(msgs))
	blockedErr := make([]error, len(msgs))
	for i := range msgs {
		if invalidReq[i] {
			continue
		}
		if err := seiLegacyGateError(methods[i], g.allowlist); err != nil {
			blocked[i] = true
			blockedErr[i] = err
		}
	}
	var forward []json.RawMessage
	forwardLegacy := false
	for i := range msgs {
		if invalidReq[i] || blocked[i] {
			continue
		}
		forward = append(forward, msgs[i])
		if seiLegacyForwardedGatedMethod(methods[i], g.allowlist) {
			forwardLegacy = true
		}
	}
	if len(forward) == 0 {
		outArr := make([]json.RawMessage, len(msgs))
		for i := range msgs {
			if invalidReq[i] {
				outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32600, seiLegacyBatchInvalidReqMsg))
				continue
			}
			outArr[i] = json.RawMessage(marshalBlockedResponse(orNullID(ids[i]), blockedErr[i]))
		}
		writeJSONArrayResponse(w, http.StatusOK, outArr)
		return
	}
	forwardBody, err := json.Marshal(forward)
	if err != nil {
		g.serveInnerWithBody(w, r, body)
		return
	}
	rec := httptest.NewRecorder()
	sub := r.Clone(r.Context())
	sub.Body = io.NopCloser(bytes.NewReader(forwardBody))
	sub.ContentLength = int64(len(forwardBody))
	sub.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(forwardBody)), nil
	}
	g.inner.ServeHTTP(rec, sub)
	outArr := mergeSeiLegacyHTTPBatch(invalidReq, blocked, blockedErr, ids, len(msgs), rec.Body.Bytes())
	copyHTTPHeader(w.Header(), rec.Header())
	if forwardLegacy {
		w.Header().Set(SeiLegacyDeprecationHTTPHeader, SeiLegacyDeprecationMessage)
	}
	writeJSONArrayResponse(w, http.StatusOK, outArr)
}

const seiLegacyBatchInternalErr = "invalid or incomplete JSON-RPC batch response from server"

// seiLegacyBatchInvalidReqMsg is the JSON-RPC 2.0 recommended message for error code -32600.
const seiLegacyBatchInvalidReqMsg = "Invalid Request"

// mergeSeiLegacyHTTPBatch produces one JSON-RPC response object per client batch entry (lenMsgs),
// using blockedErr for gated slots, -32600 for invalidReq slots, and matching inner batch items
// to forwarded requests by JSON-RPC id (order of inner responses may differ from forwarding order).
// Notification-shaped requests (no id or null id) consume remaining inner responses in FIFO order after id matches.
func mergeSeiLegacyHTTPBatch(
	invalidReq []bool,
	blocked []bool,
	blockedErr []error,
	ids []json.RawMessage,
	lenMsgs int,
	innerBody []byte,
) []json.RawMessage {
	outArr := make([]json.RawMessage, lenMsgs)
	var innerArr []json.RawMessage
	if err := json.Unmarshal(innerBody, &innerArr); err != nil {
		for i := 0; i < lenMsgs; i++ {
			switch {
			case invalidReq[i]:
				outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32600, seiLegacyBatchInvalidReqMsg))
			case blocked[i]:
				outArr[i] = json.RawMessage(marshalBlockedResponse(orNullID(ids[i]), blockedErr[i]))
			default:
				outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32603, seiLegacyBatchInternalErr))
			}
		}
		return outArr
	}

	entries := make([]json.RawMessage, len(innerArr))
	idToIdx := make(map[string]int, len(innerArr))
	for j, raw := range innerArr {
		idRaw, hasKey, err := jsonRPCObjectIDKey(raw)
		if err != nil {
			for i := 0; i < lenMsgs; i++ {
				switch {
				case invalidReq[i]:
					outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32600, seiLegacyBatchInvalidReqMsg))
				case blocked[i]:
					outArr[i] = json.RawMessage(marshalBlockedResponse(orNullID(ids[i]), blockedErr[i]))
				default:
					outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32603, seiLegacyBatchInternalErr))
				}
			}
			return outArr
		}
		entries[j] = raw
		if !hasKey || isJSONRPCNotificationID(idRaw) {
			continue
		}
		k := rpcIDKey(idRaw)
		if firstIdx, ok := idToIdx[k]; ok {
			// Duplicate response id in inner batch — cannot disambiguate.
			if !bytes.Equal(entries[firstIdx], raw) {
				for i := 0; i < lenMsgs; i++ {
					switch {
					case invalidReq[i]:
						outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32600, seiLegacyBatchInvalidReqMsg))
					case blocked[i]:
						outArr[i] = json.RawMessage(marshalBlockedResponse(orNullID(ids[i]), blockedErr[i]))
					default:
						outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32603, seiLegacyBatchInternalErr))
					}
				}
				return outArr
			}
			continue
		}
		idToIdx[k] = j
	}

	used := make([]bool, len(innerArr))
	for i := 0; i < lenMsgs; i++ {
		if invalidReq[i] {
			outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32600, seiLegacyBatchInvalidReqMsg))
			continue
		}
		if blocked[i] {
			outArr[i] = json.RawMessage(marshalBlockedResponse(orNullID(ids[i]), blockedErr[i]))
			continue
		}
		if !isJSONRPCNotificationID(ids[i]) {
			k := rpcIDKey(ids[i])
			idx, ok := idToIdx[k]
			if !ok || used[idx] {
				outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32603, seiLegacyBatchInternalErr))
				continue
			}
			used[idx] = true
			outArr[i] = json.RawMessage(patchJSONRPCResponseIDIfNeeded(entries[idx], ids[i]))
			continue
		}
	}

	var fifo []int
	for j := range entries {
		if used[j] {
			continue
		}
		fifo = append(fifo, j)
	}

	notifSlots := make([]int, 0)
	for i := 0; i < lenMsgs; i++ {
		if blocked[i] || invalidReq[i] || !isJSONRPCNotificationID(ids[i]) {
			continue
		}
		notifSlots = append(notifSlots, i)
	}
	fifoPos := 0
	for _, i := range notifSlots {
		if fifoPos >= len(fifo) {
			outArr[i] = json.RawMessage(marshalJSONRPCError(orNullID(ids[i]), -32603, seiLegacyBatchInternalErr))
			continue
		}
		idx := fifo[fifoPos]
		fifoPos++
		used[idx] = true
		outArr[i] = json.RawMessage(patchJSONRPCResponseIDIfNeeded(entries[idx], ids[i]))
	}

	return outArr
}

func rpcIDKey(id json.RawMessage) string {
	return string(bytes.TrimSpace(id))
}

func isJSONRPCNotificationID(id json.RawMessage) bool {
	if len(id) == 0 {
		return true
	}
	return bytes.Equal(bytes.TrimSpace(id), []byte("null"))
}

// jsonRPCObjectIDKey returns the raw JSON for the "id" field if present.
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

// patchJSONRPCResponseIDIfNeeded rewrites "id" to match the client request when the inner server echoed a different id.
func patchJSONRPCResponseIDIfNeeded(resp json.RawMessage, wantID json.RawMessage) []byte {
	if isJSONRPCNotificationID(wantID) {
		return resp
	}
	got, has, err := jsonRPCObjectIDKey(resp)
	if err != nil || !has {
		return resp
	}
	if bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(wantID)) {
		return resp
	}
	var m map[string]any
	if err := json.Unmarshal(resp, &m); err != nil {
		return resp
	}
	var idVal any
	if err := json.Unmarshal(wantID, &idVal); err != nil {
		return resp
	}
	m["id"] = idVal
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
				"code":    -32603,
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
