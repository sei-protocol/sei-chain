package evmrpc

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
)

const seiLegacyHTTPMaxBody = 32 << 20 // 32MiB, typical RPC max message limits

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
	for i, raw := range msgs {
		var msg struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			g.serveInnerWithBody(w, r, body)
			return
		}
		methods[i] = msg.Method
		ids[i] = msg.ID
	}
	blocked := make([]bool, len(msgs))
	blockedErr := make([]error, len(msgs))
	for i := range msgs {
		if err := seiLegacyGateError(methods[i], g.allowlist); err != nil {
			blocked[i] = true
			blockedErr[i] = err
		}
	}
	var forward []json.RawMessage
	forwardLegacy := false
	for i := range msgs {
		if !blocked[i] {
			forward = append(forward, msgs[i])
			if seiLegacyForwardedGatedMethod(methods[i], g.allowlist) {
				forwardLegacy = true
			}
		}
	}
	if len(forward) == 0 {
		outArr := make([]json.RawMessage, len(msgs))
		for i := range msgs {
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
	var innerArr []json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &innerArr); err != nil || len(innerArr) != len(forward) {
		copyHTTPHeader(w.Header(), rec.Header())
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
		return
	}
	outArr := make([]json.RawMessage, len(msgs))
	innerPos := 0
	for i := range msgs {
		if blocked[i] {
			outArr[i] = json.RawMessage(marshalBlockedResponse(orNullID(ids[i]), blockedErr[i]))
			continue
		}
		outArr[i] = innerArr[innerPos]
		innerPos++
	}
	copyHTTPHeader(w.Header(), rec.Header())
	if forwardLegacy {
		w.Header().Set(SeiLegacyDeprecationHTTPHeader, SeiLegacyDeprecationMessage)
	}
	writeJSONArrayResponse(w, rec.Code, outArr)
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
