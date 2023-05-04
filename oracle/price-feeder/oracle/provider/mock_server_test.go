package provider

import (
	"testing"

	"github.com/gorilla/websocket"
)

func TestMockServer(t *testing.T) {
	s := NewMockProviderServer()
	s.Start()
	defer s.Close()

	// Connect to the server
	ws, _, err := websocket.DefaultDialer.Dial(s.GetWebsocketURL(), nil)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer ws.Close()

	// Send message to server, read response and check to see if it's what we expect.
	for i := 0; i < 10; i++ {
		if err := ws.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
			t.Fatalf("%v", err)
		}
		_, p, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if string(p) != "hello" {
			t.Fatalf("bad message")
		}
	}
}
