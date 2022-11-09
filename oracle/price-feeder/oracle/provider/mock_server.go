package provider

import (
	"net/http"
	"net/http/httptest"

	"github.com/gorilla/websocket"
)

type MockProviderServer struct {
	handlerFunc http.HandlerFunc
	server      *httptest.Server
}

func NewMockServer() MockProviderServer {
	mockProvider := MockProviderServer{}
	// default to echo handler
	mockProvider.SetHandler(echo)
	return mockProvider
}

func (m *MockProviderServer) SetHandler(handler http.HandlerFunc) {
	m.Close()
	m.handlerFunc = handler
	m.Start()
}

func (m *MockProviderServer) Start() {
	m.server = httptest.NewServer(http.HandlerFunc(m.handlerFunc))
}

func (m *MockProviderServer) Close() {
	m.server.Close()
}

var upgrader = websocket.Upgrader{}

func echo(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			break
		}
		err = c.WriteMessage(mt, message)
		if err != nil {
			break
		}
	}
}
