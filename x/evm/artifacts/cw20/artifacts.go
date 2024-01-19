package cw20

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
)

//go:embed CW20ERC20Wrapper.abi
//go:embed CW20ERC20Wrapper.bin
var f embed.FS

var cachedBin []byte

func GetABI() []byte {
	bz, err := f.ReadFile("CW20ERC20Wrapper.abi")
	if err != nil {
		panic("failed to read CW20ERC20 contract ABI")
	}
	return bz
}

func GetBin() []byte {
	if cachedBin != nil {
		return cachedBin
	}
	code, err := f.ReadFile("CW20ERC20Wrapper.bin")
	if err != nil {
		panic("failed to read CW20ERC20 contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode CW20ERC20 contract binary")
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
	abi, err := Cw20MetaData.GetAbi()
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
