package p256

import (
	"embed"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	VerifyMethod = "verify"
)

const (
	P256VerifyAddress = "0x0000000000000000000000000000000000001011"
	GasCostPerByte    = 300
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	VerifyID []byte
}

func NewPrecompile() (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{}

	for name, m := range newAbi.Methods {
		switch name {
		case VerifyMethod:
			p.VerifyID = m.ID
		}
	}

	return pcommon.NewPrecompile(newAbi, p, common.HexToAddress(P256VerifyAddress), "p256Verify"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return uint64(GasCostPerByte * len(input))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM) (ret []byte, err error) {
	if err = pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, errors.New("cannot delegatecall P256Verify")
	}

	switch method.Name {
	case VerifyMethod:
		return p.verify(ctx, method, args, caller)
	}
	return
}

// verify verifies the secp256r1 signature
// Implements https://github.com/ethereum/RIPs/blob/master/RIPS/rip-7212.md
func (p PrecompileExecutor) verify(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address) (ret []byte, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		rerr = err
		return
	}
	input := args[0].([]byte)

	// Required input length is 160 bytes
	const p256VerifyInputLength = 160
	// Check the input length
	if len(input) != p256VerifyInputLength {
		// Input length is invalid
		rerr = errors.New("invalid input length")
		return
	}

	// Extract the hash, r, s, x, y from the input
	hash := input[0:32]
	r, s := new(big.Int).SetBytes(input[32:64]), new(big.Int).SetBytes(input[64:96])
	x, y := new(big.Int).SetBytes(input[96:128]), new(big.Int).SetBytes(input[128:160])

	// Verify the secp256r1 signature
	if Verify(hash, r, s, x, y) {
		// Signature is valid
		ret, rerr = method.Outputs.Pack(common.LeftPadBytes([]byte{1}, 32))
		return
	} else {
		// Signature is invalid
		return
	}
}
