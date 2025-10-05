package relay

import (
	"embed"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/sei-protocol/sei-chain/utils"
)

const CurrentVersion uint16 = 1

//go:embed ClaimVerifier.abi
//go:embed ClaimVerifier.bin
var f embed.FS

var (
	cachedBin []byte
	cachedABI *abi.ABI
	cacheMtx  sync.RWMutex
)

func GetABI() []byte {
	bz, err := f.ReadFile("ClaimVerifier.abi")
	if err != nil {
		panic("failed to read ClaimVerifier contract ABI")
	}
	return bz
}

func GetParsedABI() *abi.ABI {
	if cached := getCachedABI(); cached != nil {
		return cached
	}
	parsed, err := abi.JSON(strings.NewReader(string(GetABI())))
	if err != nil {
		panic(err)
	}
	setCachedABI(&parsed)
	return &parsed
}

func GetBin() []byte {
	if cached := getCachedBin(); len(cached) > 0 {
		return cached
	}
	code, err := f.ReadFile("ClaimVerifier.bin")
	if err != nil {
		panic("failed to read ClaimVerifier contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode ClaimVerifier contract binary")
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
