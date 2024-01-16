package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	// ///// Use to generate a random pk
	// privateKey, err := crypto.GenerateKey()
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// pkStr := hex.EncodeToString(crypto.FromECDSA(privateKey))

	////// Or use this
	pkStr := "7ffb23cf52c91528f212155d7030fda09bb19a577f794ef8ad2f86cfe9aa2066"

	// Decode the hex string to a byte slice
	privateKeyBytes, err := hex.DecodeString(pkStr)
	if err != nil {
		log.Fatal(err)
	}

	// Generate a new ecdsa.PrivateKey based on the S256 curve
	privateKey := new(ecdsa.PrivateKey)
	privateKey.PublicKey.Curve = elliptic.P256()
	privateKey.D = new(big.Int).SetBytes(privateKeyBytes)
	privateKey.PublicKey.X, privateKey.PublicKey.Y = privateKey.PublicKey.Curve.ScalarBaseMult(privateKeyBytes)
	////// Or use this [END]

	fmt.Println("pkStr = ", pkStr)
	fmt.Println("privateKey = ", privateKey)
	addr := getAddressFromPrivateKey(pkStr)
	fmt.Println("addr = ", addr)

	// You can convert it to a hex string if needed
	privateKeyHex := hex.EncodeToString(crypto.FromECDSA(privateKey))
	log.Println("Private Key:", privateKeyHex)

	data := []byte("")
	signature, err := crypto.Sign(crypto.Keccak256Hash(data).Bytes(), privateKey)
	if err != nil {
		log.Fatal(err)
	}

	rHex := hex.EncodeToString(signature[:32])
	sHex := hex.EncodeToString(signature[32:64])
	vHex := hex.EncodeToString([]byte{signature[64]})

	fmt.Printf("r = 0x%v\n", rHex)
	fmt.Printf("s = 0x%v\n", sHex)
	fmt.Printf("v = 0x%v\n", vHex)

	// reconstruct the signature from v,r,s, make sure you get the same address
	publicKeyECDSA, err := crypto.SigToPub(crypto.Keccak256Hash(data).Bytes(), signature)
	if err != nil {
		log.Fatal(err)
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)
	log.Println("Address:", address.Hex())

}

func getAddressFromPrivateKey(privateKeyHex string) string {
	// Hex decode the private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		log.Fatalf("Failed to decode private key: %v", err)
	}

	// Obtain the public key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Fatal("Error casting public key to ECDSA")
	}

	// Get the address
	address := crypto.PubkeyToAddress(*publicKeyECDSA)
	fmt.Println("Address:", address.Hex())
	return address.Hex()
}
