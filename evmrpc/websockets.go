package evmrpc

import (
	"net/http"
)

type wsConnectionHandler struct {
	underlying http.Handler
}

func (h *wsConnectionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	recordWebsocketConnect()
	h.underlying.ServeHTTP(w, r)
}

func NewWSConnectionHandler(handler http.Handler) http.Handler {
	return &wsConnectionHandler{underlying: handler}
}
