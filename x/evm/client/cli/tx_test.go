package cli

import (
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	evmRPCMainnet = "https://evm-rpc.sei-apis.com"
	evmRPCTestnet = "https://evm-rpc-testnet.sei-apis.com"
)

func TestGetChainId(t *testing.T) {

	tests := []struct {
		name    string
		rpc     string
		chainId int64
		hasErr  bool
	}{
		{"mainnet chain id", evmRPCMainnet, 1329, false},
		{"testnet chain id", evmRPCTestnet, 1328, false},
		{"error chain id", "", 0, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(st *testing.T) {
			chainId, err := getChainId(test.rpc)
			if test.hasErr {
				require.Error(st, err)
			} else {
				require.NoError(st, err)
				require.Equal(st, *big.NewInt(test.chainId), *chainId)
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
		rpc       string
		publicKey ecdsa.PublicKey
		nonce     uint64
	}{
		{"mainnet new address", evmRPCMainnet, privateKey.PublicKey, uint64(0)},
		{"testnet new address", evmRPCTestnet, privateKey.PublicKey, uint64(0)},
	}

	for _, test := range tests {
		t.Run(test.name, func(st *testing.T) {
			nonce, err := getNonce(test.rpc, test.publicKey)
			require.NoError(st, err)
			require.Equal(st, nonce, test.nonce)
		})
	}

	//NOTE: Could add tests for known active public keys for mainnet and testnet
}
