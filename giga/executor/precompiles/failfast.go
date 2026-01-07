package precompiles

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

var ErrInvalidPrecompileCall = errors.New("invalid precompile call")

type FailFastPrecompile struct{}

var FailFastSingleton vm.PrecompiledContract = &FailFastPrecompile{}

func (p *FailFastPrecompile) RequiredGas(input []byte) uint64 {
	return 0
}

func (p *FailFastPrecompile) Run(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, value *big.Int, readOnly bool, isFromDelegateCall bool, hooks *tracing.Hooks) ([]byte, error) {
	return nil, ErrInvalidPrecompileCall
}

var AllCustomPrecompilesFailFast = map[common.Address]vm.PrecompiledContract{}

func init() {
	for addr := 0x1001; addr <= 0x100C; addr++ {
		evmAddr := common.HexToAddress(fmt.Sprintf("0x%X", addr))
		AllCustomPrecompilesFailFast[evmAddr] = FailFastSingleton
	}
}
