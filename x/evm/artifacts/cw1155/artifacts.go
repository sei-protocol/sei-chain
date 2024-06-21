package cw1155

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721" // TODO: delete
)

const CurrentVersion uint16 = 1

// //go:embed CW1155ERC1155Pointer.abi
// //go:embed CW1155ERC1155Pointer.bin
var f embed.FS

var cachedBin []byte
var cachedLegacyBin []byte
var cachedABI *abi.ABI

func GetABI() []byte {
	// bz, err := f.ReadFile("CW1155ERC1155Pointer.abi")
	bz, err := f.ReadFile("../cw721/CW721ERC721Pointer.abi") // TODO: remove this line and uncomment line above
	if err != nil {
		panic("failed to read CW1155ERC1155Pointer contract ABI")
	}
	return bz
}

func GetParsedABI() *abi.ABI {
	if cachedABI != nil {
		return cachedABI
	}
	parsedABI, err := abi.JSON(strings.NewReader(string(GetABI())))
	if err != nil {
		panic(err)
	}
	cachedABI = &parsedABI
	return cachedABI
}

func GetBin() []byte {
	if cachedBin != nil {
		return cachedBin
	}
	// code, err := f.ReadFile("CW1155ERC1155Pointer.bin")
	code, err := f.ReadFile("../cw721/CW721ERC721Pointer.bin") // TODO: remove this line and uncomment line above
	if err != nil {
		panic("failed to read CW1155ERC1155Pointer contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode CW1155ERC1155Pointer contract binary")
	}
	cachedBin = bz
	return bz
}

func IsCodeFromBin(code []byte) bool {
	return isCodeFromBin(code, GetBin())
}

func isCodeFromBin(code []byte, bin []byte) bool {
	binLen := len(bin)
	if len(code) < binLen {
		return false
	}
	if !bytes.Equal(code[:binLen], bin) {
		return false
	}
	// abi, err := Cw1155MetaData.GetAbi()
	abi, err := cw721.Cw721MetaData.GetAbi() // TODO: remove this line and uncomment line above
	if err != nil {
		fmt.Printf("error getting metadata ABI: %s\n", err)
		return false
	}
	args, err := abi.Constructor.Inputs.Unpack(code[binLen:])
	if err != nil || len(args) != 3 {
		return false
	}
	_, isA0String := args[0].(string)
	_, isA1String := args[1].(string)
	_, isA2String := args[2].(string)
	return isA0String && isA1String && isA2String
}
