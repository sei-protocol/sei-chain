package server

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const minCompressBytes = 1024

var gzPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

type gzipResponseWriter struct {
	resp http.ResponseWriter

	gz            *gzip.Writer
	contentLength uint64
	hasLength     bool
	inited        bool
}

func (w *gzipResponseWriter) init() {
	if w.inited {
		return
	}
	w.inited = true

	hdr := w.resp.Header()
	length := hdr.Get("content-length")
	if len(length) > 0 {
		if n, err := strconv.ParseUint(length, 10, 64); err == nil {
			w.hasLength = true
			w.contentLength = n
		}
	}

	// Skip compression if the inner handler already encoded the response, opted
	// out via Transfer-Encoding: identity, or emitted a byte range whose
	// Content-Range offsets describe the uncompressed body.
	if hdr.Get("content-encoding") != "" || hdr.Get("transfer-encoding") == "identity" ||
		hdr.Get("content-range") != "" {
		return
	}

	// Skip compression for small responses with a known Content-Length; below
	// the threshold the gzip overhead outweighs the savings and the CPU cost
	// per byte is not worth it for unauthenticated callers.
	if w.hasLength && w.contentLength < minCompressBytes {
		return
	}

	w.gz = gzPool.Get().(*gzip.Writer)
	w.gz.Reset(w.resp)
	hdr.Del("content-length")
	hdr.Set("content-encoding", "gzip")
}

func (w *gzipResponseWriter) Header() http.Header {
	return w.resp.Header()
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	// Bodyless responses must not be compressed — gzip would write a stream
	// terminator into what must be an empty body (RFC 7230 §3.3). 206 responses
	// carry Content-Range offsets that describe the uncompressed body.
	if status == http.StatusNoContent || status == http.StatusNotModified ||
		status == http.StatusPartialContent || (status >= 100 && status <= 199) {
		w.inited = true // skip gzip setup
		w.resp.WriteHeader(status)
		return
	}
	w.init()
	w.resp.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	w.init()

	if w.gz == nil {
		return w.resp.Write(b)
	}

	return w.gz.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	// Decide the encoding before headers are committed: a bare Flush() before
	// any Write() would otherwise commit identity-encoded headers, after which a
	// later Write() sets Content-Encoding: gzip too late and emits gzip bytes
	// under an identity encoding.
	w.init()
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.resp.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker by forwarding to the inner ResponseWriter.
// The gzip writer is closed first so the hijacked connection starts clean.
func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.resp.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("gzip middleware: underlying ResponseWriter does not implement http.Hijacker")
	}
	w.close()
	return h.Hijack()
}

func (w *gzipResponseWriter) close() {
	if w.gz == nil {
		return
	}
	_ = w.gz.Close()
	gzPool.Put(w.gz)
	w.gz = nil
}

// abort returns the writer to the pool without emitting the gzip footer, used
// when a panic unwinds mid-response so we don't frame a valid gzip stream
// ahead of the plaintext 500 body appended by the recovery handler.
func (w *gzipResponseWriter) abort() {
	if w.gz == nil {
		return
	}
	gzPool.Put(w.gz)
	w.gz = nil
}

// acceptsGzip reports whether the request advertises gzip encoding support,
// respecting q-values and the "*" wildcard per RFC 7231 §5.3.4.
func acceptsGzip(r *http.Request) bool {
	gzipQ := -1.0
	starQ := -1.0
	for _, field := range r.Header["Accept-Encoding"] {
		for part := range strings.SplitSeq(field, ",") {
			part = strings.TrimSpace(part)
			coding, params, _ := strings.Cut(part, ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			q := 1.0
			for p := range strings.SplitSeq(params, ";") {
				p = strings.TrimSpace(p)
				if k, v, ok := strings.Cut(p, "="); ok && strings.ToLower(strings.TrimSpace(k)) == "q" {
					if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
						q = f
					}
				}
			}
			switch coding {
			case "gzip":
				gzipQ = q
			case "*":
				starQ = q
			}
		}
	}
	if gzipQ >= 0 {
		return gzipQ > 0
	}
	if starQ >= 0 {
		return starQ > 0
	}
	return false
}

// ensureVaryAcceptEncoding appends Accept-Encoding to the Vary header exactly
// once, deduplicating against any value already set by the inner handler or
// CORS middleware. Vary: * already implies Accept-Encoding, so it is left as-is.
func ensureVaryAcceptEncoding(h http.Header) {
	existing := h.Get("Vary")
	for part := range strings.SplitSeq(existing, ",") {
		v := strings.TrimSpace(part)
		if strings.EqualFold(v, "Accept-Encoding") || v == "*" {
			return
		}
	}
	if existing == "" {
		h.Set("Vary", "Accept-Encoding")
	} else {
		h.Set("Vary", existing+", Accept-Encoding")
	}
}

// hasUpgradeToken reports whether the Upgrade header contains token (RFC 7230
// §6.7 permits a comma-separated list; each token is matched case-insensitively).
func hasUpgradeToken(r *http.Request, token string) bool {
	for _, field := range r.Header["Upgrade"] {
		for part := range strings.SplitSeq(field, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// NewGzipHandler wraps next with gzip response compression. Compression is
// skipped for clients that do not advertise gzip support via Accept-Encoding.
// WebSocket upgrades are passed through unmodified; gzipResponseWriter also
// implements http.Hijacker as defense-in-depth for non-canonical Upgrade values.
func NewGzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Vary must be set on every response — compressed or not — so that CDN
		// caches key on Accept-Encoding and never serve a wrong variant.
		ensureVaryAcceptEncoding(w.Header())

		if !acceptsGzip(r) || hasUpgradeToken(r, "websocket") {
			next.ServeHTTP(w, r)
			return
		}

		wrapper := &gzipResponseWriter{resp: w}
		defer func() {
			if p := recover(); p != nil {
				wrapper.abort()
				panic(p)
			}
			wrapper.close()
		}()

		next.ServeHTTP(wrapper, r)
	})
}
