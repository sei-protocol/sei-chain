package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

var gzPool = sync.Pool{
	New: func() any {
		w := gzip.NewWriter(io.Discard)
		return w
	},
}

type gzipResponseWriter struct {
	resp http.ResponseWriter

	gz            *gzip.Writer
	contentLength uint64
	written       uint64
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

	// Transfer-Encoding: identity signals that the inner handler wants to opt
	// out of compression (e.g. error responses near the write deadline).
	if hdr.Get("transfer-encoding") == "identity" {
		return
	}

	w.gz = gzPool.Get().(*gzip.Writer)
	w.gz.Reset(w.resp)
	hdr.Del("content-length")
	hdr.Set("content-encoding", "gzip")
	hdr.Add("vary", "Accept-Encoding")
}

func (w *gzipResponseWriter) Header() http.Header {
	return w.resp.Header()
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	// Bodyless responses must not be compressed — gzip would write a stream
	// terminator into what must be an empty body (RFC 7230 §3.3).
	if status == http.StatusNoContent || status == http.StatusNotModified ||
		(status >= 100 && status <= 199) {
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

	n, err := w.gz.Write(b)
	w.written += uint64(n) //nolint:gosec
	if w.hasLength && w.written >= w.contentLength {
		if cerr := w.gz.Close(); cerr != nil && err == nil {
			err = cerr
		}
		gzPool.Put(w.gz)
		w.gz = nil
	}
	return n, err
}

func (w *gzipResponseWriter) Flush() {
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.resp.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzipResponseWriter) close() {
	if w.gz == nil {
		return
	}
	_ = w.gz.Close()
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

// NewGzipHandler wraps next with gzip response compression. Compression is
// skipped for clients that do not advertise gzip support via Accept-Encoding
// and for WebSocket upgrade requests, preserving the http.Hijacker interface.
func NewGzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}

		wrapper := &gzipResponseWriter{resp: w}
		defer wrapper.close()

		next.ServeHTTP(wrapper, r)
	})
}
