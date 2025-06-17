package v552

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common/legacy/v552"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

const (
	InstantiateMethod  = "instantiate"
	ExecuteMethod      = "execute"
	ExecuteBatchMethod = "execute_batch"
	QueryMethod        = "query"
)

const WasmdAddress = "0x0000000000000000000000000000000000001002"

var _ vm.PrecompiledContract = &Precompile{}
var _ vm.DynamicGasPrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type Precompile struct {
	pcommon.Precompile
	evmKeeper       putils.EVMKeeper
	bankKeeper      putils.BankKeeper
	wasmdKeeper     putils.WasmdKeeper
	wasmdViewKeeper putils.WasmdViewKeeper
	address         common.Address

	InstantiateID  []byte
	ExecuteID      []byte
	ExecuteBatchID []byte
	QueryID        []byte
}

type ExecuteMsg struct {
	ContractAddress string `json:"contractAddress"`
	Msg             []byte `json:"msg"`
	Coins           []byte `json:"coins"`
}

func NewPrecompile(keepers putils.Keepers) (*Precompile, error) {
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
		wasmdKeeper:     keepers.WasmdK(),
		wasmdViewKeeper: keepers.WasmdVK(),
		evmKeeper:       keepers.EVMK(),
		bankKeeper:      keepers.BankK(),
		address:         common.HexToAddress(WasmdAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case InstantiateMethod:
			p.InstantiateID = m.ID
		case ExecuteMethod:
			p.ExecuteID = m.ID
		case ExecuteBatchMethod:
			p.ExecuteBatchID = m.ID
		case QueryMethod:
			p.QueryID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID, err := pcommon.ExtractMethodID(input)
	if err != nil {
		return pcommon.UnknownMethodCallGas
	}

	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return pcommon.UnknownMethodCallGas
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method.Name))
}

func (Precompile) IsTransaction(method string) bool {
	switch method {
	case ExecuteMethod:
		return true
	case ExecuteBatchMethod:
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

func (p Precompile) GetName() string {
	return "wasmd"
}

func (p Precompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, suppliedGas uint64, value *big.Int, hooks *tracing.Hooks, readOnly bool, _ bool) (ret []byte, remainingGas uint64, err error) {
	defer func() {
		if err != nil {
			evm.StateDB.(*state.DBImpl).SetPrecompileError(err)
		}
	}()
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, 0, err
	}
	if method.Name != QueryMethod && !ctx.IsEVM() {
		return nil, 0, errors.New("sei does not support CW->EVM->CW call pattern")
	}
	gasMultipler := p.evmKeeper.GetPriorityNormalizerPre580(ctx)
	gasLimitBigInt := sdk.NewDecFromInt(sdk.NewIntFromUint64(suppliedGas)).Mul(gasMultipler).TruncateInt().BigInt()
	if gasLimitBigInt.Cmp(utils.BigMaxU64) > 0 {
		gasLimitBigInt = utils.BigMaxU64
	}
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, gasLimitBigInt.Uint64()))

	switch method.Name {
	case InstantiateMethod:
		return p.instantiate(ctx, method, caller, callingContract, args, value, readOnly, hooks, evm)
	case ExecuteMethod:
		return p.execute(ctx, method, caller, callingContract, args, value, readOnly, hooks, evm)
	case ExecuteBatchMethod:
		return p.executeBatch(ctx, method, caller, callingContract, args, value, readOnly, hooks, evm)
	case QueryMethod:
		return p.query(ctx, method, args, value)
	}
	return
}

func (p Precompile) Run(*vm.EVM, common.Address, common.Address, []byte, *big.Int, bool, bool, *tracing.Hooks) ([]byte, error) {
	panic("static gas Run is not implemented for dynamic gas precompile")
}

func (p Precompile) instantiate(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, hooks *tracing.Hooks, evm *vm.EVM) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	if readOnly {
		rerr = errors.New("cannot call instantiate from staticcall")
		return
	}
	if err := pcommon.ValidateArgsLength(args, 5); err != nil {
		rerr = err
		return
	}
	if caller.Cmp(callingContract) != 0 {
		rerr = errors.New("cannot delegatecall instantiate")
		return
	}

	// type assertion will always succeed because it's already validated in p.Prepare call in Run()
	codeID := args[0].(uint64)
	creatorAddr, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		rerr = fmt.Errorf("creator %s is not associated", caller.Hex())
		return
	}
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
	coinsValue := coins.AmountOf(sdk.MustGetBaseDenom()).Mul(state.SdkUseiToSweiMultiplier).BigInt()
	if (value == nil && coinsValue.Sign() == 1) || (value != nil && coinsValue.Cmp(value) != 0) {
		rerr = errors.New("coin amount must equal value specified")
		return
	}

	// Run basic validation, can also just expose validateLabel and validate validateWasmCode in sei-wasmd
	msgInstantiate := wasmtypes.MsgInstantiateContract{
		Sender: creatorAddr.String(),
		CodeID: codeID,
		Label:  label,
		Funds:  coins,
		Msg:    msg,
		Admin:  adminAddrStr,
	}

	if err := msgInstantiate.ValidateBasic(); err != nil {
		rerr = err
		return
	}
	useiAmt := coins.AmountOf(sdk.MustGetBaseDenom())
	if value != nil && !useiAmt.IsZero() {
		useiAmtAsWei := useiAmt.Mul(state.SdkUseiToSweiMultiplier).BigInt()
		coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), creatorAddr, useiAmtAsWei, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
		if err != nil {
			rerr = err
			return
		}
		// sanity check coin amounts match
		if !coin.Amount.Equal(useiAmt) {
			rerr = errors.New("mismatch between coins and payment value")
			return
		}
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

func (p Precompile) executeBatch(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, hooks *tracing.Hooks, evm *vm.EVM) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	if readOnly {
		rerr = errors.New("cannot call execute from staticcall")
		return
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		rerr = err
		return
	}

	executeMsgs := args[0].([]struct {
		ContractAddress string `json:"contractAddress"`
		Msg             []byte `json:"msg"`
		Coins           []byte `json:"coins"`
	})

	responses := make([][]byte, 0, len(executeMsgs))

	// validate coins add up to value
	validateValue := big.NewInt(0)
	for i := 0; i < len(executeMsgs); i++ {
		executeMsg := ExecuteMsg(executeMsgs[i])
		coinsBz := executeMsg.Coins
		coins := sdk.NewCoins()
		if err := json.Unmarshal(coinsBz, &coins); err != nil {
			rerr = err
			return
		}
		messageAmount := coins.AmountOf(sdk.MustGetBaseDenom()).Mul(state.SdkUseiToSweiMultiplier).BigInt()
		validateValue.Add(validateValue, messageAmount)
	}
	// if validateValue is greater than zero, then value must be provided, and they must be equal
	if (value == nil && validateValue.Sign() == 1) || (value != nil && validateValue.Cmp(value) != 0) {
		rerr = errors.New("sum of coin amounts must equal value specified")
		return
	}
	for i := 0; i < len(executeMsgs); i++ {
		executeMsg := ExecuteMsg(executeMsgs[i])

		// type assertion will always succeed because it's already validated in p.Prepare call in Run()
		contractAddrStr := executeMsg.ContractAddress
		if caller.Cmp(callingContract) != 0 {
			erc20pointer, _, erc20exists := p.evmKeeper.GetERC20CW20Pointer(ctx, contractAddrStr)
			erc721pointer, _, erc721exists := p.evmKeeper.GetERC721CW721Pointer(ctx, contractAddrStr)
			if (!erc20exists || erc20pointer.Cmp(callingContract) != 0) && (!erc721exists || erc721pointer.Cmp(callingContract) != 0) {
				return nil, 0, fmt.Errorf("%s is not a pointer of %s", callingContract.Hex(), contractAddrStr)
			}
		}

		contractAddr, err := sdk.AccAddressFromBech32(contractAddrStr)
		if err != nil {
			rerr = err
			return
		}
		senderAddr, senderAssociated := p.evmKeeper.GetSeiAddress(ctx, caller)
		if !senderAssociated {
			rerr = fmt.Errorf("sender %s is not associated", caller.Hex())
			return
		}
		msg := executeMsg.Msg
		coinsBz := executeMsg.Coins
		coins := sdk.NewCoins()
		if err := json.Unmarshal(coinsBz, &coins); err != nil {
			rerr = err
			return
		}
		useiAmt := coins.AmountOf(sdk.MustGetBaseDenom())
		if value != nil && !useiAmt.IsZero() {
			// process coin amount from the value provided
			useiAmtAsWei := useiAmt.Mul(state.SdkUseiToSweiMultiplier).BigInt()
			coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), senderAddr, useiAmtAsWei, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
			if err != nil {
				rerr = err
				return
			}
			value.Sub(value, useiAmtAsWei)
			if value.Sign() == -1 {
				rerr = errors.New("insufficient value provided for payment")
				return
			}
			// sanity check coin amounts match
			if !coin.Amount.Equal(useiAmt) {
				rerr = errors.New("mismatch between coins and payment value")
				return
			}
		}
		// Run basic validation, can also just expose validateLabel and validate validateWasmCode in sei-wasmd
		msgExecute := wasmtypes.MsgExecuteContract{
			Sender:   senderAddr.String(),
			Contract: contractAddr.String(),
			Msg:      msg,
			Funds:    coins,
		}
		if err := msgExecute.ValidateBasic(); err != nil {
			rerr = err
			return
		}

		res, err := p.wasmdKeeper.Execute(ctx, contractAddr, senderAddr, msg, coins)
		if err != nil {
			rerr = err
			return
		}
		responses = append(responses, res)
	}
	if value != nil && value.Sign() != 0 {
		rerr = errors.New("value remaining after execution, must match provided amounts exactly")
		return
	}
	ret, rerr = method.Outputs.Pack(responses)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p Precompile) execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, hooks *tracing.Hooks, evm *vm.EVM) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	if readOnly {
		rerr = errors.New("cannot call execute from staticcall")
		return
	}
	if err := pcommon.ValidateArgsLength(args, 3); err != nil {
		rerr = err
		return
	}

	// type assertion will always succeed because it's already validated in p.Prepare call in Run()
	contractAddrStr := args[0].(string)
	if caller.Cmp(callingContract) != 0 {
		erc20pointer, _, erc20exists := p.evmKeeper.GetERC20CW20Pointer(ctx, contractAddrStr)
		erc721pointer, _, erc721exists := p.evmKeeper.GetERC721CW721Pointer(ctx, contractAddrStr)
		if (!erc20exists || erc20pointer.Cmp(callingContract) != 0) && (!erc721exists || erc721pointer.Cmp(callingContract) != 0) {
			return nil, 0, fmt.Errorf("%s is not a pointer of %s", callingContract.Hex(), contractAddrStr)
		}
	}
	// addresses will be sent in Sei format
	contractAddr, err := sdk.AccAddressFromBech32(contractAddrStr)
	if err != nil {
		rerr = err
		return
	}
	senderAddr, found := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !found {
		rerr = fmt.Errorf("sender %s is not associated", caller.Hex())
		return
	}
	msg := args[1].([]byte)
	coins := sdk.NewCoins()
	coinsBz := args[2].([]byte)
	if err := json.Unmarshal(coinsBz, &coins); err != nil {
		rerr = err
		return
	}
	coinsValue := coins.AmountOf(sdk.MustGetBaseDenom()).Mul(state.SdkUseiToSweiMultiplier).BigInt()
	if (value == nil && coinsValue.Sign() == 1) || (value != nil && coinsValue.Cmp(value) != 0) {
		rerr = errors.New("coin amount must equal value specified")
		return
	}

	// Run basic validation, can also just expose validateLabel and validate validateWasmCode in sei-wasmd
	msgExecute := wasmtypes.MsgExecuteContract{
		Sender:   senderAddr.String(),
		Contract: contractAddr.String(),
		Msg:      msg,
		Funds:    coins,
	}

	if err := msgExecute.ValidateBasic(); err != nil {
		rerr = err
		return
	}

	useiAmt := coins.AmountOf(sdk.MustGetBaseDenom())
	if value != nil && !useiAmt.IsZero() {
		useiAmtAsWei := useiAmt.Mul(state.SdkUseiToSweiMultiplier).BigInt()
		coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), senderAddr, useiAmtAsWei, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
		if err != nil {
			rerr = err
			return
		}
		// sanity check coin amounts match
		if !coin.Amount.Equal(useiAmt) {
			rerr = errors.New("mismatch between coins and payment value")
			return
		}
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
	if err := pcommon.ValidateNonPayable(value); err != nil {
		rerr = err
		return
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		rerr = err
		return
	}

	contractAddrStr := args[0].(string)
	// addresses will be sent in Sei format
	contractAddr, err := sdk.AccAddressFromBech32(contractAddrStr)
	if err != nil {
		rerr = err
		return
	}
	req := args[1].([]byte)

	rawContractMessage := wasmtypes.RawContractMessage(req)
	if err := rawContractMessage.ValidateBasic(); err != nil {
		rerr = err
		return
	}

	res, err := p.wasmdViewKeeper.QuerySmart(ctx, contractAddr, req)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(res)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}
