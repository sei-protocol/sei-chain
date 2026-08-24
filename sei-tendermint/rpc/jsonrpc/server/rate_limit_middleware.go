package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"
)

var errRateLimitBodyTooLarge = errors.New("request body too large")

type rateLimitMiddleware struct {
	inner http.Handler
	gate  *RateLimitGate
}

// NewRateLimitMiddleware wraps inner with CometBFT RPC HTTP rate-limit admission.
// When gate is nil, inner is returned unchanged.
//
// All HTTP verbs are limited, including OPTIONS; genuine CORS preflights are
// already terminated by the CORS handler wrapping this middleware.
func NewRateLimitMiddleware(inner http.Handler, gate *RateLimitGate) http.Handler {
	if gate == nil {
		return inner
	}
	return &rateLimitMiddleware{inner: inner, gate: gate}
}

func (m *rateLimitMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := m.gate.Registry().IPFromHTTPRequest(r)

	if isCometBFTRootPath(r) {
		body, err := readRateLimitBoundedBody(r.Body, m.gate.MaxBodyBytes())
		if err != nil {
			if isRateLimitRequestBodyTooLarge(err) {
				m.rejectAdmission(r.Context(), w, ip, body, http.StatusRequestEntityTooLarge, rpctypes.CodeInvalidRequest, "request body too large")
				return
			}
			m.rejectAdmission(r.Context(), w, ip, body, http.StatusBadRequest, rpctypes.CodeInvalidRequest, "bad request")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		if len(body) == 0 {
			allowed, rejectMethod := m.gate.CheckCatalog(r.Context(), ip)
			if !allowed {
				if rejectMethod != "" {
					logger.Debug("rate limit rejected catalog request", "ip", ip, "method", rejectMethod, "plane", m.gate.Plane())
				}
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			m.inner.ServeHTTP(w, r)
			return
		}

		allowed, rejectMethod, checkErr := m.gate.CheckPOST(r.Context(), ip, bytes.NewReader(body))
		if checkErr != nil {
			writeJSONRPCErrorWithStatus(w, body, http.StatusBadRequest, rpctypes.CodeParseError, "decoding request: %v", checkErr)
			return
		}
		if !allowed {
			logger.Debug("rate limit rejected JSON-RPC request", "ip", ip, "method", rejectMethod, "plane", m.gate.Plane())
			writeJSONRPCErrorWithStatus(w, body, http.StatusTooManyRequests, rpctypes.CodeInternalError, "too many requests")
			return
		}

		m.inner.ServeHTTP(w, r)
		return
	}

	if isCometBFTURIRPCRequest(r) {
		allowed, rejectMethod, checkErr := m.gate.CheckURI(r.Context(), ip, r.URL.Path)
		if checkErr != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if !allowed {
			if rejectMethod != "" {
				logger.Debug("rate limit rejected URI request", "ip", ip, "method", rejectMethod, "plane", m.gate.Plane())
			}
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		m.inner.ServeHTTP(w, r)
		return
	}

	m.inner.ServeHTTP(w, r)
}

func (m *rateLimitMiddleware) rejectAdmission(ctx context.Context, w http.ResponseWriter, ip string, body []byte, status int, code rpctypes.ErrorCode, msg string) {
	if m.gate.ChargeAdmissionRejection(ctx, ip) {
		writeJSONRPCErrorWithStatus(w, body, http.StatusTooManyRequests, rpctypes.CodeInternalError, "too many requests")
		return
	}
	writeJSONRPCErrorWithStatus(w, body, status, code, "%s", msg)
}

// isCometBFTRootPath reports requests to the JSON-RPC root (/).
func isCometBFTRootPath(r *http.Request) bool {
	return r.URL.Path == "/" || r.URL.Path == ""
}

// isCometBFTURIRPCRequest reports CometBFT URI RPC routes (/status, /block, etc.).
// The mux registers these for any HTTP method; params may arrive in the query
// string or form-encoded body.
func isCometBFTURIRPCRequest(r *http.Request) bool {
	return r.URL.Path != "/" && r.URL.Path != ""
}

func readRateLimitBoundedBody(body io.ReadCloser, maxBytes int64) ([]byte, error) {
	if body == nil {
		return nil, errors.New("missing request body")
	}
	defer body.Close()

	if maxBytes <= 0 {
		return io.ReadAll(body)
	}

	lr := &io.LimitedReader{R: body, N: maxBytes + 1}
	buf, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxBytes {
		// Leave the unread remainder for net/http to drop the connection rather
		// than draining an unbounded oversize payload here.
		return nil, errRateLimitBodyTooLarge
	}
	return buf, nil
}

func isRateLimitRequestBodyTooLarge(err error) bool {
	if errors.Is(err, errRateLimitBodyTooLarge) {
		return true
	}
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
