package utils

import (
	"net/url"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestCheckHTTPURL(t *testing.T) {
	for _, s := range []string{"http://validator.example:8545", "https://validator.example:8545"} {
		require.NoError(t, CheckHTTPURL(*OrPanic1(url.Parse(s))))
	}
	for _, s := range []string{"file://localhost/rpc", "ws://validator.example:8545", "http://", "http://u:p@validator.example:8545"} {
		require.Error(t, CheckHTTPURL(*OrPanic1(url.Parse(s))))
	}
}
