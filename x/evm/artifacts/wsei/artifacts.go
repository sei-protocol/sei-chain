package wsei

import (
	"embed"
	"encoding/hex"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

const CurrentVersion uint16 = 1

//go:embed WSEI.abi
//go:embed WSEI.bin
var f embed.FS

var cachedBin []byte
var cachedABI *abi.ABI

func GetABI() []byte {
	bz, err := f.ReadFile("WSEI.abi")
	if err != nil {
		panic("failed to read WSEI contract ABI")
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
	code, err := f.ReadFile("WSEI.bin")
	if err != nil {
		panic("failed to read WSEI contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode WSEI contract binary")
	}
	cachedBin = bz
	return bz
}
