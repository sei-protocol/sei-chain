package evmrpc

import (
	"github.com/gorilla/websocket"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"net/http"
	"sync/atomic"
)

type wsConnectionHandler struct {
	underlying            http.Handler
	metricConnectionCount int64
}

func (h *wsConnectionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u := websocket.Upgrader{}
	conn, err := u.Upgrade(w, r, nil)
	if err != nil {
		//TODO: log error
		return
	}

	// Increment the open connection counter
	atomic.AddInt64(&h.metricConnectionCount, 1)
	metrics.SetWebsocketConnections(atomic.LoadInt64(&h.metricConnectionCount))

	go func() {
		defer func() {
			// Decrement the open connection counter when the connection closes
			atomic.AddInt64(&h.metricConnectionCount, -1)
			if err := conn.Close(); err != nil {
				//TODO: log error
			}
		}()

		// Continue to use the existing underlying handler to handle the connection
		h.underlying.ServeHTTP(w, r)
	}()
}

func NewWSConnectionHandler(handler http.Handler) http.Handler {
	return &wsConnectionHandler{underlying: handler}
}
