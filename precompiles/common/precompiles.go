package common

import (
	"errors"
	"fmt"
	"math/big"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

type Contexter interface {
	Ctx() sdk.Context
}

type Precompile struct {
	abi.ABI
}

func (p Precompile) RequiredGas(input []byte, isTransaction bool) uint64 {
	argsBz := input[4:] // first four bytes are method ID

	if isTransaction {
		return storetypes.KVGasConfig().WriteCostFlat + (storetypes.KVGasConfig().WriteCostPerByte * uint64(len(argsBz)))
	}

	return storetypes.KVGasConfig().ReadCostFlat + (storetypes.KVGasConfig().ReadCostPerByte * uint64(len(argsBz)))
}

func (p Precompile) Prepare(evm *vm.EVM, input []byte) (sdk.Context, *abi.Method, []interface{}, error) {
	ctxer, ok := evm.StateDB.(Contexter)
	if !ok {
		return sdk.Context{}, nil, nil, errors.New("cannot get context from EVM")
	}
	methodID := input[:4]
	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		return sdk.Context{}, nil, nil, err
	}

	argsBz := input[4:]
	args, err := method.Inputs.Unpack(argsBz)
	if err != nil {
		return sdk.Context{}, nil, nil, err
	}

	return ctxer.Ctx(), method, args, nil
}

func AssertArgsLength(args []interface{}, length int) {
	if len(args) != length {
		panic(fmt.Sprintf("expected %d arguments but got %d", length, len(args)))
	}
}

func AssertNonPayable(value *big.Int) {
	if value != nil && value.Sign() != 0 {
		panic("sending funds to a non-payable function")
	}
}

func HandlePayment(ctx sdk.Context, precompileAddr sdk.AccAddress, payer sdk.AccAddress, value *big.Int, bankKeeper BankKeeper) (sdk.Coin, error) {
	usei, wei := state.SplitUseiWeiAmount(value)
	if !wei.IsZero() {
		return sdk.Coin{}, fmt.Errorf("selected precompile function does not allow payment with non-zero wei remainder: received %s", value)
	}
	coin := sdk.NewCoin(sdk.MustGetBaseDenom(), usei)
	// refund payer because the following precompile logic will debit the payments from payer's account
	if err := bankKeeper.SendCoins(ctx, precompileAddr, payer, sdk.NewCoins(coin)); err != nil {
		return sdk.Coin{}, err
	}
	return coin, nil
}
