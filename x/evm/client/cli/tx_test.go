package cli

import (
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var (
	EVM_RPC_MAINNET = "https://evm-rpc.sei-apis.com"
	EVM_RPC_TESTNET = "https://evm-rpc-testnet.sei-apis.com"
)

func TestGetChainId(t *testing.T) {
	//Run on mainnet RPC url
	mChainId, err := getChainId(EVM_RPC_MAINNET)
	require.Nil(t, err)
	require.Equal(t, *big.NewInt(1329), *mChainId)

	//Run on testnet RPC url
	tChainId, err := getChainId(EVM_RPC_TESTNET)
	require.Nil(t, err)
	require.Equal(t, *big.NewInt(1328), *tChainId)

	//Run with no RPC url
	_, err = getChainId("")
	require.Error(t, err)
}

func TestGetNonce(t *testing.T) {
	//Test nonce is zero for a new wallet
	//Generate a new privateKey from secp256k1 and get public key
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	require.Nil(t, err)

	//Check nonce on both mainnet and testnet
	nonce, err := getNonce(EVM_RPC_MAINNET, privateKey.PublicKey)
	require.Nil(t, err)
	require.Equal(t, nonce, uint64(0))

	nonce, err = getNonce(EVM_RPC_TESTNET, privateKey.PublicKey)
	require.Nil(t, err)
	require.Equal(t, nonce, uint64(0))

	//NOTE: Could add tests for known active public keys for mainnet and testnet
}
