package distribution

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	SetWithdrawAddressMethod                = "setWithdrawAddress"
	WithdrawDelegationRewardsMethod         = "withdrawDelegationRewards"
	WithdrawMultipleDelegationRewardsMethod = "withdrawMultipleDelegationRewards"
	RewardsMethod                           = "rewards"
)

const (
	DistrAddress = "0x0000000000000000000000000000000000001007"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

func GetABI() abi.ABI {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		panic(err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		panic(err)
	}
	return newAbi
}

type PrecompileExecutor struct {
	distrKeeper pcommon.DistributionKeeper
	evmKeeper   pcommon.EVMKeeper
	address     common.Address

	SetWithdrawAddrID                   []byte
	WithdrawDelegationRewardsID         []byte
	WithdrawMultipleDelegationRewardsID []byte
	RewardsID                           []byte
}

func NewPrecompile(distrKeeper pcommon.DistributionKeeper, evmKeeper pcommon.EVMKeeper) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := GetABI()

	p := &PrecompileExecutor{
		distrKeeper: distrKeeper,
		evmKeeper:   evmKeeper,
		address:     common.HexToAddress(DistrAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case SetWithdrawAddressMethod:
			p.SetWithdrawAddrID = m.ID
		case WithdrawDelegationRewardsMethod:
			p.WithdrawDelegationRewardsID = m.ID
		case WithdrawMultipleDelegationRewardsMethod:
			p.WithdrawMultipleDelegationRewardsID = m.ID
		case RewardsMethod:
			p.RewardsID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, p.address, "distribution"), nil
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if caller.Cmp(callingContract) != 0 {
		return nil, 0, errors.New("cannot delegatecall distr")
	}
	switch method.Name {
	case SetWithdrawAddressMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call distr precompile from staticcall")
		}
		return p.setWithdrawAddress(ctx, method, caller, args, value)
	case WithdrawDelegationRewardsMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call distr precompile from staticcall")
		}
		return p.withdrawDelegationRewards(ctx, method, caller, args, value)
	case WithdrawMultipleDelegationRewardsMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call distr precompile from staticcall")
		}
		return p.withdrawMultipleDelegationRewards(ctx, method, caller, args, value)
	case RewardsMethod:
		return p.rewards(ctx, method, args)
	}
	return
}

func (p PrecompileExecutor) EVMKeeper() pcommon.EVMKeeper {
	return p.evmKeeper
}

func (p PrecompileExecutor) GetName() string {
	return "distribution"
}

func (p PrecompileExecutor) setWithdrawAddress(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	if err := pcommon.ValidateNonPayable(value); err != nil {
		rerr = err
		return
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		rerr = err
		return
	}
	delegator, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		rerr = types.NewAssociationMissingErr(caller.Hex())
		return
	}
	withdrawAddr, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		rerr = err
		return
	}
	err = p.distrKeeper.SetWithdrawAddr(ctx, delegator, withdrawAddr)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) withdrawDelegationRewards(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	err := p.validateInput(value, args, 1)
	if err != nil {
		rerr = err
		return
	}

	delegator, err := p.getDelegator(ctx, caller)
	if err != nil {
		rerr = err
		return
	}
	_, err = p.withdraw(ctx, delegator, args[0].(string))
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) validateInput(value *big.Int, args []interface{}, expectedArgsLength int) error {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return err
	}

	if err := pcommon.ValidateArgsLength(args, expectedArgsLength); err != nil {
		return err
	}

	return nil
}

func (p PrecompileExecutor) withdraw(ctx sdk.Context, delegator sdk.AccAddress, validatorAddress string) (sdk.Coins, error) {
	validator, err := sdk.ValAddressFromBech32(validatorAddress)
	if err != nil {
		return nil, err
	}
	return p.distrKeeper.WithdrawDelegationRewards(ctx, delegator, validator)
}

func (p PrecompileExecutor) getDelegator(ctx sdk.Context, caller common.Address) (sdk.AccAddress, error) {
	delegator, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}

	return delegator, nil
}

func (p PrecompileExecutor) withdrawMultipleDelegationRewards(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	err := p.validateInput(value, args, 1)
	if err != nil {
		rerr = err
		return
	}

	delegator, err := p.getDelegator(ctx, caller)
	if err != nil {
		rerr = err
		return
	}
	validators := args[0].([]string)
	for _, valAddr := range validators {
		_, err := p.withdraw(ctx, delegator, valAddr)
		if err != nil {
			rerr = err
			return
		}
	}

	ret, rerr = method.Outputs.Pack(true)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	seiAddr, associated := p.evmKeeper.GetSeiAddress(ctx, addr)
	if !associated {
		return nil, errors.New("cannot use an unassociated address as withdraw address")
	}
	return seiAddr, nil
}

type Coin struct {
	Amount   *big.Int
	Denom    string
	Decimals *big.Int
}

func (p PrecompileExecutor) rewards(ctx sdk.Context, method *abi.Method, args []interface{}) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		rerr = err
		return
	}

	seiDelegatorAddress, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		rerr = err
		return
	}

	req := &distrtypes.QueryDelegationTotalRewardsRequest{
		DelegatorAddress: seiDelegatorAddress.String(),
	}

	wrappedC := sdk.WrapSDKContext(ctx)
	response, err := p.distrKeeper.DelegationTotalRewards(wrappedC, req)
	if err != nil {
		rerr = err
		return
	}

	coins := make([]Coin, len(response.Rewards))
	rewards := response.Rewards
	for i, reward := range rewards {
		coins[i] = Coin{}
		for _, coin := range reward.Reward {
			coins[i] = Coin{
				Amount:   coin.Amount.BigInt(),
				Denom:    coin.Denom,
				Decimals: big.NewInt(sdk.Precision),
			}
		}
	}
	ret, rerr = method.Outputs.Pack(coins)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}
