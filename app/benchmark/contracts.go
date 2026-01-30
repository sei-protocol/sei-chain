package benchmark

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-load/generator/bindings"
)

// getERC20DeployData returns the deployment bytecode for ERC20 with constructor args.
func getERC20DeployData() []byte {
	parsed, err := bindings.ERC20MetaData.GetAbi()
	if err != nil {
		panic("failed to parse ERC20 ABI: " + err.Error())
	}

	// Constructor args: name = "LoadToken", symbol = "LT"
	// Use Constructor.Inputs.Pack directly to ensure correct encoding
	constructorArgs, err := parsed.Constructor.Inputs.Pack("LoadToken", "LT")
	if err != nil {
		panic("failed to pack ERC20 constructor args: " + err.Error())
	}

	bytecode := common.FromHex(bindings.ERC20Bin)
	return append(bytecode, constructorArgs...)
}

// getERC721DeployData returns the deployment bytecode for ERC721 with constructor args.
func getERC721DeployData() []byte {
	parsed, err := bindings.ERC721MetaData.GetAbi()
	if err != nil {
		panic("failed to parse ERC721 ABI: " + err.Error())
	}

	// Constructor args: name = "LoadNFT", symbol = "LNFT"
	constructorArgs, err := parsed.Constructor.Inputs.Pack("LoadNFT", "LNFT")
	if err != nil {
		panic("failed to pack ERC721 constructor args: " + err.Error())
	}

	bytecode := common.FromHex(bindings.ERC721Bin)
	return append(bytecode, constructorArgs...)
}

// getERC20ConflictDeployData returns the deployment bytecode for ERC20Conflict.
func getERC20ConflictDeployData() []byte {
	parsed, err := bindings.ERC20ConflictMetaData.GetAbi()
	if err != nil {
		panic("failed to parse ERC20Conflict ABI: " + err.Error())
	}

	// Constructor args: name = "ConflictToken", symbol = "CT"
	constructorArgs, err := parsed.Constructor.Inputs.Pack("ConflictToken", "CT")
	if err != nil {
		panic("failed to pack ERC20Conflict constructor args: " + err.Error())
	}

	bytecode := common.FromHex(bindings.ERC20ConflictBin)
	return append(bytecode, constructorArgs...)
}

// getERC20NoopDeployData returns the deployment bytecode for ERC20Noop.
func getERC20NoopDeployData() []byte {
	parsed, err := bindings.ERC20NoopMetaData.GetAbi()
	if err != nil {
		panic("failed to parse ERC20Noop ABI: " + err.Error())
	}

	// Constructor args: name = "NoopToken", symbol = "NT"
	constructorArgs, err := parsed.Constructor.Inputs.Pack("NoopToken", "NT")
	if err != nil {
		panic("failed to pack ERC20Noop constructor args: " + err.Error())
	}

	bytecode := common.FromHex(bindings.ERC20NoopBin)
	return append(bytecode, constructorArgs...)
}

// getDisperseDeployData returns the deployment bytecode for Disperse.
func getDisperseDeployData() []byte {
	// Disperse has no constructor args
	return common.FromHex(bindings.DisperseBin)
}

// packConstructorArgs packs constructor arguments using the provided ABI.
// The empty string "" selects the constructor.
func packConstructorArgs(parsedABI *abi.ABI, args ...interface{}) ([]byte, error) {
	return parsedABI.Pack("", args...)
}
