package erc1155

import "embed"

const CurrentVersion uint16 = 1

//go:embed cwerc1155.wasm
var f embed.FS

var cachedBin []byte

func GetBin() []byte {
	if cachedBin != nil {
		return cachedBin
	}
	bz, err := f.ReadFile("cwerc1155.wasm")
	if err != nil {
		panic("failed to read ERC1155 wrapper contract wasm")
	}
	cachedBin = bz
	return bz
}
