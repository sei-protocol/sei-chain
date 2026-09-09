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

func TestIsLoopbackOrLinkLocalURL(t *testing.T) {
	for _, s := range []string{
		"http://localhost:8545",
		"http://LocalHost:8545",
		"http://127.0.0.1:8545",
		"http://127.9.9.9:8545",
		"http://[::1]:8545",
		"http://169.254.169.254/",
		"http://[fe80::1]:8545",
	} {
		require.True(t, IsLoopbackOrLinkLocalURL(*OrPanic1(url.Parse(s))))
	}
	for _, s := range []string{
		"http://validator.example:8545",
		"http://10.0.0.1:8545",
		"http://8.8.8.8:8545",
		"http://localhost.validator.example:8545",
	} {
		require.False(t, IsLoopbackOrLinkLocalURL(*OrPanic1(url.Parse(s))))
	}
}
