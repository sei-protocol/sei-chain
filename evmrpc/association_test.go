package evmrpc_test

import (
	"fmt"
	"log"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestAssocation(t *testing.T) {

	// Generate a new private key
	fmt.Println("hi")
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		log.Fatalf("Failed to generate private key: %v", err)
	}

	// Sign an empty payload
	signature, err := crypto.Sign(crypto.Keccak256([]byte("")), privateKey)
	if err != nil {
		log.Fatalf("Failed to sign payload: %v", err)
	}

	// Print the signature
	fmt.Printf("Signature: %s\n", hexutil.Encode(signature)) // Outputs: Signature: 0x...

	// Extract the r, s, v values from the signature
	r := fmt.Sprintf("0x%v", new(big.Int).SetBytes(signature[:32]).Text(16))
	s := fmt.Sprintf("0x%v", new(big.Int).SetBytes(signature[32:64]).Text(16))
	v := fmt.Sprintf("0x%v", new(big.Int).SetBytes([]byte{signature[64]}).Text(16))

	// Print the r, s, v values
	fmt.Printf("R: %s\n", r) // Outputs: R: ...
	fmt.Printf("S: %s\n", s) // Outputs: S: ...
	fmt.Printf("V: %s\n", v) // Outputs: V: ...

	txArgs := map[string]interface{}{
		"r": r,
		"s": s,
		"v": v,
	}

	body := sendRequestGoodWithNamespace(t, "sei", "associate", txArgs)
	fmt.Printf("body = %s\n", body)
	// req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	// require.Nil(t, err)
	// req.Header.Set("Content-Type", "application/json")
	// res, err := http.DefaultClient.Do(req)
	// require.Nil(t, err)
	// resBody, err := io.ReadAll(res.Body)
	// require.Nil(t, err)
	// fmt.Println("resBody = ", string(resBody))
}
