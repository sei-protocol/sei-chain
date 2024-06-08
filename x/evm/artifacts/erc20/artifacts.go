package erc20

import "embed"

const CurrentVersion uint16 = 1

//go:embed cwerc20.wasm
var f embed.FS

var cachedBin []byte

func GetBin() []byte {
	if cachedBin != nil {
		return cachedBin
	}
	bz, err := f.ReadFile("cwerc20.wasm")
	if err != nil {
		panic("failed to read ERC20 wrapper contract wasm")
	}
	cachedBin = bz
	return bz
}
