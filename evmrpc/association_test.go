package evmrpc_test

import (
	"fmt"
	"log"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestAssocation(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		log.Fatalf("Failed to generate private key: %v", err)
	}

	// Sign an empty payload
	emptyHash := common.Hash{}
	signature, err := crypto.Sign(emptyHash[:], privateKey)
	if err != nil {
		log.Fatalf("Failed to sign payload: %v", err)
	}
	fmt.Printf("signature %+v\n", signature)
	fmt.Printf("signature string %+v\n", string(signature))

	txArgs := map[string]interface{}{
		"r": fmt.Sprintf("0x%v", new(big.Int).SetBytes(signature[:32]).Text(16)),
		"s": fmt.Sprintf("0x%v", new(big.Int).SetBytes(signature[32:64]).Text(16)),
		"v": fmt.Sprintf("0x%v", new(big.Int).SetBytes([]byte{signature[64]}).Text(16)),
	}

	body := sendRequestGoodWithNamespace(t, "sei", "associate", txArgs)
	require.Equal(t, body["result"], nil)
}

func TestGetSeiAddress(t *testing.T) {
	body := sendRequestGoodWithNamespace(t, "sei", "getSeiAddress", "0x1df809C639027b465B931BD63Ce71c8E5834D9d6")
	require.Equal(t, body["result"], "sei1mf0llhmqane5w2y8uynmghmk2w4mh0xll9seym")
}

func TestGetEvmAddress(t *testing.T) {
	body := sendRequestGoodWithNamespace(t, "sei", "getEVMAddress", "sei1mf0llhmqane5w2y8uynmghmk2w4mh0xll9seym")
	require.Equal(t, body["result"], "0x1df809C639027b465B931BD63Ce71c8E5834D9d6")
}

func TestBug(t *testing.T) {
	key, err := crypto.HexToECDSA("57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e")
	if err != nil {
		panic(err)
	}
	emptyHash := common.Hash{}
	sig, err := crypto.Sign(emptyHash[:], key)
	if err != nil {
		panic(err)
	}

	// Extract R, S, and V from the signature
	R := new(big.Int).SetBytes(sig[:32])
	S := new(big.Int).SetBytes(sig[32:64])
	V := big.NewInt(int64(sig[64]))

	// Adjust V from Ethereum format (27 or 28) to compact format (0 or 1)
	if V.Cmp(big.NewInt(27)) == 0 || V.Cmp(big.NewInt(28)) == 0 {
		V.Sub(V, big.NewInt(27))
	}

	fmt.Printf("r: %s\n", hexutil.EncodeBig(R))
	fmt.Printf("s: %s\n", hexutil.EncodeBig(S))
	fmt.Printf("v: %d\n", V)
	panic("here")
}
