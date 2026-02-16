package bls

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

// EIP-2537 precompile addresses (7 operations at 0x0b-0x11)
const (
	G1AddAddress   = "0x000000000000000000000000000000000000000b"
	G1MSMAddress   = "0x000000000000000000000000000000000000000c"
	G2AddAddress   = "0x000000000000000000000000000000000000000d"
	G2MSMAddress   = "0x000000000000000000000000000000000000000e"
	PairingAddress = "0x000000000000000000000000000000000000000f"
	MapG1Address   = "0x0000000000000000000000000000000000000010"
	MapG2Address   = "0x0000000000000000000000000000000000000011"
)

// OpInfo stores metadata for an EIP-2537 BLS precompile operation.
type OpInfo struct {
	Addr string
	Name string
	Impl vm.PrecompiledContract
}

// AllOps returns all 7 EIP-2537 BLS12-381 precompile operations.
func AllOps() []OpInfo {
	return []OpInfo{
		{G1AddAddress, "blsG1Add", &vm.Bls12381G1Add{}},
		{G1MSMAddress, "blsG1MSM", &vm.Bls12381G1MultiExp{}},
		{G2AddAddress, "blsG2Add", &vm.Bls12381G2Add{}},
		{G2MSMAddress, "blsG2MSM", &vm.Bls12381G2MultiExp{}},
		{PairingAddress, "blsPairing", &vm.Bls12381Pairing{}},
		{MapG1Address, "blsMapG1", &vm.Bls12381MapG1{}},
		{MapG2Address, "blsMapG2", &vm.Bls12381MapG2{}},
	}
}

// BLSPrecompile wraps a native go-ethereum EIP-2537 precompile to satisfy
// Sei's IPrecompile interface. It handles raw calldata (no ABI encoding)
// per the EIP-2537 specification.
type BLSPrecompile struct {
	vm.PrecompiledContract
	address common.Address
	name    string
}

// NewPrecompile creates a BLS precompile wrapper for the given operation.
func NewPrecompile(op OpInfo) *BLSPrecompile {
	return &BLSPrecompile{
		PrecompiledContract: op.Impl,
		address:             common.HexToAddress(op.Addr),
		name:                op.Name,
	}
}

// GetVersioned returns versioned precompiles for a specific BLS operation.
func GetVersioned(op OpInfo) utils.VersionedPrecompiles {
	return utils.VersionedPrecompiles{
		"1": NewPrecompile(op),
	}
}

func (p *BLSPrecompile) GetABI() abi.ABI {
	return abi.ABI{}
}

func (p *BLSPrecompile) GetName() string {
	return p.name
}

func (p *BLSPrecompile) Address() common.Address {
	return p.address
}
