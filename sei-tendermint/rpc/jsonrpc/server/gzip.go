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
}

func (w *gzipResponseWriter) Header() http.Header {
	return w.resp.Header()
}

func (w *gzipResponseWriter) WriteHeader(status int) {
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
		err = w.gz.Close()
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

// NewGzipHandler wraps next with gzip response compression. Compression is
// skipped for clients that do not advertise gzip support via Accept-Encoding
// and for WebSocket upgrade requests, preserving the http.Hijacker interface.
func NewGzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}

		wrapper := &gzipResponseWriter{resp: w}
		defer wrapper.close()

		next.ServeHTTP(wrapper, r)
	})
}
