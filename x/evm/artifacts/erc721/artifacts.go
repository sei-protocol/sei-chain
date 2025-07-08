package erc721

import (
	"embed"
	"sync"

	"github.com/sei-protocol/sei-chain/utils"
)

const CurrentVersion uint16 = 6

//go:embed cwerc721.wasm
var f embed.FS

var cachedBin []byte
var cacheMtx *sync.RWMutex = &sync.RWMutex{}

func GetBin() []byte {
	if cached := getCachedBin(); len(cached) > 0 {
		return cached
	}
	bz, err := f.ReadFile("cwerc721.wasm")
	if err != nil {
		panic("failed to read ERC721 wrapper contract wasm")
	}
	setCachedBin(bz)
	return utils.Copy(bz)
}

func getCachedBin() []byte {
	cacheMtx.RLock()
	defer cacheMtx.RUnlock()
	return utils.Copy(cachedBin)
}

func setCachedBin(bin []byte) {
	cacheMtx.Lock()
	defer cacheMtx.Unlock()
	cachedBin = bin
}
