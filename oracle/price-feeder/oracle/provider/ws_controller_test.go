package provider

import (
	"testing"

	"github.com/gorilla/websocket"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/stretchr/testify/require"
)

type TestProvider struct {
	handlerCalled bool
}

func (mp *TestProvider) messageHandler(messageType int, bz []byte) {
	mp.handlerCalled = true
}

func TestWebsocketController_readSuccess(t *testing.T) {
	testCases := []struct {
		name                     string
		messageType              int
		bz                       []byte
		shouldCallMessageHandler bool
	}{
		{
			"message length is zero",
			1,
			[]byte{},
			false,
		},
		{
			"message is a pong",
			1,
			[]byte("pong"),
			false,
		},
		{
			"is a valid message for the messageHandler",
			1,
			[]byte("asdf"),
			true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			provider := TestProvider{}
			mockClient := new(websocket.Conn)
			c := &WebsocketController{
				providerName:   config.ProviderMock,
				messageHandler: provider.messageHandler,
				client:         mockClient,
			}

			c.readSuccess(testCase.messageType, testCase.bz)
			require.Equal(t, provider.handlerCalled, testCase.shouldCallMessageHandler)
		})
	}
}
