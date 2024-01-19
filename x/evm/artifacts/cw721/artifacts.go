package cw721

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
)

//go:embed CW721ERC721Wrapper.abi
//go:embed CW721ERC721Wrapper.bin
var f embed.FS

var cachedBin []byte

func GetABI() []byte {
	bz, err := f.ReadFile("CW721ERC721Wrapper.abi")
	if err != nil {
		panic("failed to read CW721ERC721Wrapper contract ABI")
	}
	return bz
}

func GetBin() []byte {
	if cachedBin != nil {
		return cachedBin
	}
	code, err := f.ReadFile("CW721ERC721Wrapper.bin")
	if err != nil {
		panic("failed to read CW721ERC721Wrapper contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode CW721ERC721Wrapper contract binary")
	}
	cachedBin = bz
	return bz
}

func IsCodeFromBin(code []byte) bool {
	binLen := len(GetBin())
	if len(code) < binLen {
		return false
	}
	if !bytes.Equal(code[:binLen], GetBin()) {
		return false
	}
	abi, err := Cw721MetaData.GetAbi()
	if err != nil {
		fmt.Printf("error getting metadata ABI: %s\n", err)
		return false
	}
	args, err := abi.Constructor.Inputs.Unpack(code[binLen:])
	if err != nil || len(args) != 1 {
		return false
	}
	_, isString := args[0].(string)
	return isString
}
