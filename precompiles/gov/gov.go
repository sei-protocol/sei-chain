package gov

import (
	"bytes"
	"embed"
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	VoteMethod    = "vote"
	DepositMethod = "deposit"
)

const (
	GovAddress = "0x0000000000000000000000000000000000001006"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	govKeeper  pcommon.GovKeeper
	evmKeeper  pcommon.EVMKeeper
	bankKeeper pcommon.BankKeeper
	address    common.Address

	VoteID    []byte
	DepositID []byte
}

func NewPrecompile(govKeeper pcommon.GovKeeper, evmKeeper pcommon.EVMKeeper, bankKeeper pcommon.BankKeeper) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		govKeeper:  govKeeper,
		evmKeeper:  evmKeeper,
		address:    common.HexToAddress(GovAddress),
		bankKeeper: bankKeeper,
	}

	for name, m := range newAbi.Methods {
		switch name {
		case VoteMethod:
			p.VoteID = m.ID
		case DepositMethod:
			p.DepositID = m.ID
		}
	}

	return pcommon.NewPrecompile(newAbi, p, p.address, "gov"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if bytes.Equal(method.ID, p.VoteID) {
		return 30000
	} else if bytes.Equal(method.ID, p.DepositID) {
		return 30000
	}

	// This should never happen since this is going to fail during Run
	return pcommon.UnknownMethodCallGas
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM) (bz []byte, err error) {
	if readOnly {
		return nil, errors.New("cannot call gov precompile from staticcall")
	}
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, errors.New("cannot delegatecall gov")
	}

	switch method.Name {
	case VoteMethod:
		return p.vote(ctx, method, caller, args, value)
	case DepositMethod:
		return p.deposit(ctx, method, caller, args, value)
	}
	return
}

func (p PrecompileExecutor) vote(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	voter, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}
	proposalID := args[0].(uint64)
	voteOption := args[1].(int32)
	err := p.govKeeper.AddVote(ctx, proposalID, voter, govtypes.NewNonSplitVoteOption(govtypes.VoteOption(voteOption)))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p PrecompileExecutor) deposit(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	depositor, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}
	proposalID := args[0].(uint64)
	if value == nil || value.Sign() == 0 {
		return nil, errors.New("set `value` field to non-zero to deposit fund")
	}
	coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), depositor, value, p.bankKeeper)
	if err != nil {
		return nil, err
	}
	res, err := p.govKeeper.AddDeposit(ctx, proposalID, depositor, sdk.NewCoins(coin))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(res)
}
