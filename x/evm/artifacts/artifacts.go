package artifacts

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
)

//go:embed NativeSeiTokensERC20.abi
//go:embed NativeSeiTokensERC20.bin
var f embed.FS

var cachedSeiTokensERC20Bin []byte

func GetNativeSeiTokensERC20ABI() []byte {
	bz, err := f.ReadFile("NativeSeiTokensERC20.abi")
	if err != nil {
		panic("failed to read native ERC20 contract binary")
	}
	return bz
}

func GetNativeSeiTokensERC20Bin() []byte {
	if cachedSeiTokensERC20Bin != nil {
		return cachedSeiTokensERC20Bin
	}
	code, err := f.ReadFile("NativeSeiTokensERC20.bin")
	if err != nil {
		panic("failed to read native ERC20 contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode native ERC20 contract binary")
	}
	cachedSeiTokensERC20Bin = bz
	return bz
}

func IsCodeNativeSeiTokensERC20Wrapper(code []byte) bool {
	binLen := len(GetNativeSeiTokensERC20Bin())
	if len(code) < binLen {
		return false
	}
	if !bytes.Equal(code[:binLen], GetNativeSeiTokensERC20Bin()) {
		return false
	}
	abi, err := ArtifactsMetaData.GetAbi()
	if err != nil {
		fmt.Printf("error getting ERC20 wrapper metadata ABI: %s\n", err)
		return false
	}
	args, err := abi.Constructor.Inputs.Unpack(code[binLen:])
	if err != nil || len(args) != 1 {
		return false
	}
	_, isString := args[0].(string)
	return isString
}
