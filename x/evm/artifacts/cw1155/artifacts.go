package cw1155

import (
	"embed"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

const CurrentVersion uint16 = 2

//go:embed CW1155ERC1155Pointer.abi
var f embed.FS

var cachedABI *abi.ABI
var cacheMtx *sync.RWMutex = &sync.RWMutex{}

func GetABI() []byte {
	bz, err := f.ReadFile("CW1155ERC1155Pointer.abi")
	if err != nil {
		panic("failed to read CW1155ERC1155Pointer contract ABI")
	}
	return bz
}

func GetParsedABI() *abi.ABI {
	if cached := getCachedABI(); cached != nil {
		return cached
	}
	parsedABI, err := abi.JSON(strings.NewReader(string(GetABI())))
	if err != nil {
		panic(err)
	}
	setCachedABI(&parsedABI)
	return &parsedABI
}

func getCachedABI() *abi.ABI {
	cacheMtx.RLock()
	defer cacheMtx.RUnlock()
	return cachedABI
}

func setCachedABI(a *abi.ABI) {
	cacheMtx.Lock()
	defer cacheMtx.Unlock()
	cachedABI = a
}
