package wasmd

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	InstantiateMethod = "instantiate"
	ExecuteMethod     = "execute"
	QueryMethod       = "query"
)

const WasmdAddress = "0x0000000000000000000000000000000000001002"

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

var MaxUint64BigInt = new(big.Int).SetUint64(math.MaxUint64)

type Precompile struct {
	pcommon.Precompile
	evmKeeper       pcommon.EVMKeeper
	bankKeeper      pcommon.BankKeeper
	wasmdKeeper     pcommon.WasmdKeeper
	wasmdViewKeeper pcommon.WasmdViewKeeper
	address         common.Address

	InstantiateID []byte
	ExecuteID     []byte
	QueryID       []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper, wasmdKeeper pcommon.WasmdKeeper, wasmdViewKeeper pcommon.WasmdViewKeeper, bankKeeper pcommon.BankKeeper) (*Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the staking ABI %s", err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	p := &Precompile{
		Precompile:      pcommon.Precompile{ABI: newAbi},
		wasmdKeeper:     wasmdKeeper,
		wasmdViewKeeper: wasmdViewKeeper,
		evmKeeper:       evmKeeper,
		bankKeeper:      bankKeeper,
		address:         common.HexToAddress(WasmdAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case "instantiate":
			p.InstantiateID = m.ID
		case "execute":
			p.ExecuteID = m.ID
		case "query":
			p.QueryID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID := input[:4]

	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return 0
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method.Name))
}

func (Precompile) IsTransaction(method string) bool {
	switch method {
	case ExecuteMethod:
		return true
	case InstantiateMethod:
		return true
	default:
		return false
	}
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, suppliedGas uint64, value *big.Int) (ret []byte, remainingGas uint64, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, 0, err
	}
	if err := pcommon.ValidateCaller(ctx, p.evmKeeper, caller, callingContract); err != nil {
		return nil, 0, err
	}
	gasMultipler := p.evmKeeper.GetPriorityNormalizer(ctx)
	gasLimitBigInt := new(big.Int).Mul(new(big.Int).SetUint64(suppliedGas), gasMultipler.RoundInt().BigInt())
	if gasLimitBigInt.Cmp(MaxUint64BigInt) > 0 {
		gasLimitBigInt = MaxUint64BigInt
	}
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(gasLimitBigInt.Uint64()))

	switch method.Name {
	case InstantiateMethod:
		return p.instantiate(ctx, method, caller, args, value)
	case ExecuteMethod:
		return p.execute(ctx, method, caller, args, value)
	case QueryMethod:
		return p.query(ctx, method, args, value)
	}
	return
}

func (p Precompile) Run(*vm.EVM, common.Address, []byte, *big.Int) ([]byte, error) {
	panic("static gas Run is not implemented for dynamic gas precompile")
}

func (p Precompile) instantiate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	pcommon.AssertArgsLength(args, 5)

	// type assertion will always succeed because it's already validated in p.Prepare call in Run()
	codeID := args[0].(uint64)
	creatorAddr := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	var adminAddr sdk.AccAddress
	adminAddrStr := args[1].(string)
	if len(adminAddrStr) > 0 {
		adminAddrDecoded, err := sdk.AccAddressFromBech32(adminAddrStr)
		if err != nil {
			rerr = err
			return
		}
		adminAddr = adminAddrDecoded
	}
	msg := args[2].([]byte)
	label := args[3].(string)
	coins := sdk.NewCoins()
	coinsBz := args[4].([]byte)
	if err := json.Unmarshal(coinsBz, &coins); err != nil {
		rerr = err
		return
	}
	if !coins.AmountOf(sdk.MustGetBaseDenom()).IsZero() {
		rerr = errors.New("deposit of usei must be done through the `value` field")
		return
	}
	if value != nil {
		coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), creatorAddr, value, p.bankKeeper)
		if err != nil {
			rerr = err
			return
		}
		coins = coins.Add(coin)
	}

	addr, data, err := p.wasmdKeeper.Instantiate(ctx, codeID, creatorAddr, adminAddr, msg, label, coins)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(addr.String(), data)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p Precompile) execute(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	pcommon.AssertArgsLength(args, 3)

	// type assertion will always succeed because it's already validated in p.Prepare call in Run()
	contractAddrStr := args[0].(string)
	// addresses will be sent in Sei format
	contractAddr, err := sdk.AccAddressFromBech32(contractAddrStr)
	if err != nil {
		rerr = err
		return
	}
	senderAddr := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	msg := args[1].([]byte)
	coins := sdk.NewCoins()
	coinsBz := args[2].([]byte)
	if err := json.Unmarshal(coinsBz, &coins); err != nil {
		rerr = err
		return
	}
	if !coins.AmountOf(sdk.MustGetBaseDenom()).IsZero() {
		rerr = errors.New("deposit of usei must be done through the `value` field")
		return
	}
	if value != nil {
		coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), senderAddr, value, p.bankKeeper)
		if err != nil {
			rerr = err
			return
		}
		coins = coins.Add(coin)
	}
	res, err := p.wasmdKeeper.Execute(ctx, contractAddr, senderAddr, msg, coins)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(res)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p Precompile) query(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	pcommon.AssertNonPayable(value)
	pcommon.AssertArgsLength(args, 2)

	contractAddrStr := args[0].(string)
	// addresses will be sent in Sei format
	contractAddr, err := sdk.AccAddressFromBech32(contractAddrStr)
	if err != nil {
		rerr = err
		return
	}
	req := args[1].([]byte)
	res, err := p.wasmdViewKeeper.QuerySmart(ctx, contractAddr, req)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(res)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}
