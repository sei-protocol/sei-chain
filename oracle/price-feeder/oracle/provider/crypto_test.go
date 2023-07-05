package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/config"
	"github.com/sei-protocol/sei-chain/oracle/price-feeder/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestCryptoProvider(t *testing.T) {
	_, err := NewCryptoProvider(
		context.TODO(),
		zerolog.New(os.Stdout).With().Timestamp().Logger(),
		config.ProviderEndpoint{
			Name:      config.ProviderCrypto,
			Rest:      cryptoRestHost,
			Websocket: "wss://uat-stream.3ona.com",
		},
		types.CurrencyPair{},
	)
	require.NoError(t, err)

	time.Sleep(1 * time.Minute)
}
