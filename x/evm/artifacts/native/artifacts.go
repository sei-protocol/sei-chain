package native

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

const CurrentVersion uint16 = 1

//go:embed NativeSeiTokensERC20.abi
//go:embed NativeSeiTokensERC20.bin
var f embed.FS

var cachedBin []byte
var cachedABI *abi.ABI
var cacheMtx *sync.RWMutex = &sync.RWMutex{}

func GetABI() []byte {
	bz, err := f.ReadFile("NativeSeiTokensERC20.abi")
	if err != nil {
		panic("failed to read NativeSeiTokensERC20 contract ABI")
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
	code, err := f.ReadFile("NativeSeiTokensERC20.bin")
	if err != nil {
		panic("failed to read NativeSeiTokensERC20 contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode NativeSeiTokensERC20 contract binary")
	}
	setCachedBin(bz)
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

func IsCodeFromBin(code []byte) bool {
	binLen := len(GetBin())
	if len(code) < binLen {
		return false
	}
	if !bytes.Equal(code[:binLen], GetBin()) {
		return false
	}
	abi, err := NativeMetaData.GetAbi()
	if err != nil {
		fmt.Printf("error getting metadata ABI: %s\n", err)
		return false
	}
	args, err := abi.Constructor.Inputs.Unpack(code[binLen:])
	if err != nil || len(args) != 4 {
		return false
	}
	_, isString1 := args[0].(string)
	_, isString2 := args[1].(string)
	_, isString3 := args[2].(string)
	_, isUint8 := args[3].(uint8)
	return isString1 && isString2 && isString3 && isUint8
}
