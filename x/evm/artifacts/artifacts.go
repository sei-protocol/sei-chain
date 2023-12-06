package artifacts

import (
	"embed"
	"encoding/hex"
)

//go:embed NativeSeiTokensERC20.abi
//go:embed NativeSeiTokensERC20.bin
var f embed.FS

func GetNativeSeiTokensERC20ABI() []byte {
	bz, err := f.ReadFile("NativeSeiTokensERC20.abi")
	if err != nil {
		panic("failed to read native ERC20 contract binary")
	}
	return bz
}

func GetNativeSeiTokensERC20Bin() []byte {
	code, err := f.ReadFile("NativeSeiTokensERC20.bin")
	if err != nil {
		panic("failed to read native ERC20 contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode native ERC20 contract binary")
	}
	return bz
}
