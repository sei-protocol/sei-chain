package precompiles

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/addr"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	"github.com/sei-protocol/sei-chain/precompiles/distribution"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	"github.com/sei-protocol/sei-chain/precompiles/json"
	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	"github.com/sei-protocol/sei-chain/precompiles/p256"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	"github.com/sei-protocol/sei-chain/precompiles/pointerview"
	"github.com/sei-protocol/sei-chain/precompiles/solo"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
)

var FailFastPrecompileAddresses = []common.Address{
	common.HexToAddress(bank.BankAddress),
	common.HexToAddress(wasmd.WasmdAddress),
	common.HexToAddress(json.JSONAddress),
	common.HexToAddress(addr.AddrAddress),
	common.HexToAddress(staking.StakingAddress),
	common.HexToAddress(gov.GovAddress),
	common.HexToAddress(distribution.DistrAddress),
	common.HexToAddress(oracle.OracleAddress),
	common.HexToAddress(ibc.IBCAddress),
	common.HexToAddress(pointerview.PointerViewAddress),
	common.HexToAddress(pointer.PointerAddress),
	common.HexToAddress(solo.SoloAddress),
	common.HexToAddress(p256.P256VerifyAddress),
}

// InvalidPrecompileCallError is an error type that implements vm.AbortError,
// signaling that execution should abort and this error should propagate
// through the entire call stack.
type InvalidPrecompileCallError struct{}

func (e *InvalidPrecompileCallError) Error() string {
	return "invalid precompile call"
}

// IsAbortError implements vm.AbortError interface, signaling that this error
// should propagate through the EVM call stack instead of being swallowed.
func (e *InvalidPrecompileCallError) IsAbortError() bool {
	return true
}

// ErrInvalidPrecompileCall is the singleton error instance for invalid precompile calls.
// It implements vm.AbortError to ensure it propagates through the call stack.
var ErrInvalidPrecompileCall error = &InvalidPrecompileCallError{}

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
	for _, addr := range FailFastPrecompileAddresses {
		AllCustomPrecompilesFailFast[addr] = FailFastSingleton
	}
}
