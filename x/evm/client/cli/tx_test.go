package cli

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestGetChainId(t *testing.T) {

	tests := []struct {
		name       string
		chainIdHex string
		chainId    int64
		hasURL     bool
	}{
		{"mainnet chain id", "0x531", 1329, true},
		{"testnet chain id", "0x530", 1328, true},
		{"url error chain id", "", 0, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			//Setup RPC Server with result and get URL
			rpcServer := getRPCServer(t, test.chainIdHex)
			defer rpcServer.Close()

			if test.hasURL {
				chainId, err := getChainId(rpcServer.URL)
				require.NoError(t, err)
				require.Equal(t, *big.NewInt(test.chainId), *chainId)
			} else {
				_, err := getChainId("")
				require.Error(t, err)
			}
		})
	}
}

func TestGetNonce(t *testing.T) {
	//Test nonce is zero for a new wallet
	//Generate a new privateKey from secp256k1 and get public key
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	require.NoError(t, err)

	tests := []struct {
		name      string
		publicKey ecdsa.PublicKey
		nonceHex  string
		nonce     uint64
	}{
		{"new address", privateKey.PublicKey, "0x0", uint64(0)},
		{"active address", privateKey.PublicKey, "0x5", uint64(5)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			//Setup RPC Server with result and get URL
			rpcServer := getRPCServer(t, test.nonceHex)
			defer rpcServer.Close()

			nonce, err := getNonce(rpcServer.URL, test.publicKey)
			require.NoError(t, err)
			require.Equal(t, nonce, test.nonce)
		})
	}
}

func getRPCServer(t *testing.T, result string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		//Standard method call response from POST request to RPC
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      "send-cli",
			"result":  result,
		}

		//Adjust to default GET response if not POST
		if r.Method != http.MethodPost {
			response = map[string]any{
				"sei": []map[string]any{
					{
						"id":    "evm:local",
						"alias": "sei",
						"state": "OK",
					},
				},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		require.NoError(t, err)
	}))
}
