package cw721

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/sei-protocol/sei-chain/utils"
)

const CurrentVersion uint16 = 6

//go:embed CW721ERC721Pointer.abi
//go:embed CW721ERC721Pointer.bin
//go:embed legacy.bin
var f embed.FS

var cachedBin []byte
var cachedLegacyBin []byte
var cachedABI *abi.ABI
var cacheMtx *sync.RWMutex = &sync.RWMutex{}

func GetABI() []byte {
	bz, err := f.ReadFile("CW721ERC721Pointer.abi")
	if err != nil {
		panic("failed to read CW721ERC721Pointer contract ABI")
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

func GetBin() []byte {
	if cached := getCachedBin(); len(cached) > 0 {
		return cached
	}
	code, err := f.ReadFile("CW721ERC721Pointer.bin")
	if err != nil {
		panic("failed to read CW721ERC721Pointer contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode CW721ERC721Pointer contract binary")
	}
	setCachedBin(bz)
	return utils.Copy(bz)
}

func GetLegacyBin() []byte {
	if cached := getCachedLegacyBin(); len(cached) > 0 {
		return cached
	}
	code, err := f.ReadFile("legacy.bin")
	if err != nil {
		panic("failed to read CW721ERC721Pointer legacy contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode CW721ERC721Pointer legacy contract binary")
	}
	setCachedLegacyBin(bz)
	return utils.Copy(bz)
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

func getCachedLegacyBin() []byte {
	cacheMtx.RLock()
	defer cacheMtx.RUnlock()
	return utils.Copy(cachedLegacyBin)
}

func setCachedLegacyBin(bin []byte) {
	cacheMtx.Lock()
	defer cacheMtx.Unlock()
	cachedLegacyBin = bin
}

func IsCodeFromBin(code []byte) bool {
	return isCodeFromBin(code, GetBin()) || isCodeFromBin(code, GetLegacyBin())
}

func isCodeFromBin(code []byte, bin []byte) bool {
	binLen := len(bin)
	if len(code) < binLen {
		return false
	}
	if !bytes.Equal(code[:binLen], bin) {
		return false
	}
	abi, err := Cw721MetaData.GetAbi()
	if err != nil {
		fmt.Printf("error getting metadata ABI: %s\n", err)
		return false
	}
	args, err := abi.Constructor.Inputs.Unpack(code[binLen:])
	if err != nil || len(args) != 3 {
		return false
	}
	_, isA0String := args[0].(string)
	_, isA1String := args[1].(string)
	_, isA2String := args[2].(string)
	return isA0String && isA1String && isA2String
}
