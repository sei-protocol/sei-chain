package erc721

import "embed"

const CurrentVersion uint16 = 2

//go:embed cwerc721.wasm
var f embed.FS

var cachedBin []byte

func GetBin() []byte {
	if cachedBin != nil {
		return cachedBin
	}
	bz, err := f.ReadFile("cwerc721.wasm")
	if err != nil {
		panic("failed to read ERC721 wrapper contract wasm")
	}
	cachedBin = bz
	return bz
}
