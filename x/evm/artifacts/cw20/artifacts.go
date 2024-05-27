package cw20

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/sei-protocol/sei-chain/x/evm/config"
)

const currentVersion uint16 = 1

var versionOverride uint16

// SetVersionWithOffset allows for overriding the version for integration test scenarios
func SetVersionWithOffset(offset int16) {
	// this allows for negative offsets to mock lower versions
	versionOverride = uint16(int16(currentVersion) + offset)
}

func CurrentVersion(ctx sdk.Context) uint16 {
	return config.GetVersionWthDefault(ctx, versionOverride, currentVersion)
}

//go:embed CW20ERC20Pointer.abi
//go:embed CW20ERC20Pointer.bin
//go:embed legacy.bin
var f embed.FS

var cachedBin []byte
var cachedLegacyBin []byte
var cachedABI *abi.ABI

func GetABI() []byte {
	bz, err := f.ReadFile("CW20ERC20Pointer.abi")
	if err != nil {
		panic("failed to read CW20ERC20 contract ABI")
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
	code, err := f.ReadFile("CW20ERC20Pointer.bin")
	if err != nil {
		panic("failed to read CW20ERC20 contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode CW20ERC20 contract binary")
	}
	cachedBin = bz
	return bz
}

func GetLegacyBin() []byte {
	if cachedLegacyBin != nil {
		return cachedLegacyBin
	}
	code, err := f.ReadFile("legacy.bin")
	if err != nil {
		panic("failed to read CW20ERC20 legacy contract binary")
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic("failed to decode CW20ERC20 legacy contract binary")
	}
	cachedLegacyBin = bz
	return bz
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
	abi, err := Cw20MetaData.GetAbi()
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
