package provider

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type MockProviderServer struct {
	handlerFunc http.HandlerFunc
	server      *httptest.Server
}

func NewMockProviderServer() MockProviderServer {
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
	server := httptest.NewUnstartedServer(m.handlerFunc)
	server.StartTLS()
	m.server = server
	m.InjectServerCertificatesIntoDefaultDialer()
}

func (m *MockProviderServer) Close() {
	if m.server != nil {
		// restore default dialer
		websocket.DefaultDialer = &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 45 * time.Second,
		}
		m.server.Close()
	}
}

func (m *MockProviderServer) GetBaseURL() string {
	if m.server != nil {
		return strings.TrimPrefix(m.server.URL, "https://")
	}
	return ""
}

func (m *MockProviderServer) GetWebsocketURL() string {
	if m.server != nil {
		return "wss" + strings.TrimPrefix(m.server.URL, "https")
	}
	return ""
}

func (m *MockProviderServer) InjectServerCertificatesIntoDefaultDialer() {
	certs := x509.NewCertPool()
	for _, c := range m.server.TLS.Certificates {
		roots, err := x509.ParseCertificates(c.Certificate[len(c.Certificate)-1])
		if err != nil {
			panic(fmt.Errorf("error parsing server's root cert: %v", err))
		}
		for _, root := range roots {
			certs.AddCert(root)
		}
	}

	testDialer := websocket.Dialer{
		Subprotocols:    []string{"p1", "p2"},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	testDialer.TLSClientConfig = &tls.Config{
		RootCAs:    certs,
		MinVersion: tls.VersionTLS12,
	}
	websocket.DefaultDialer = &testDialer
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
