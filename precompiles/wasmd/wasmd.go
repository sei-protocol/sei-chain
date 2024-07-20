package wasmd

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	InstantiateMethod  = "instantiate"
	ExecuteMethod      = "execute"
	ExecuteBatchMethod = "execute_batch"
	QueryMethod        = "query"
)

const WasmdAddress = "0x0000000000000000000000000000000000001002"

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper       pcommon.EVMKeeper
	bankKeeper      pcommon.BankKeeper
	wasmdKeeper     pcommon.WasmdKeeper
	wasmdViewKeeper pcommon.WasmdViewKeeper
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

func NewPrecompile(evmKeeper pcommon.EVMKeeper, wasmdKeeper pcommon.WasmdKeeper, wasmdViewKeeper pcommon.WasmdViewKeeper, bankKeeper pcommon.BankKeeper) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	executor := &PrecompileExecutor{
		wasmdKeeper:     wasmdKeeper,
		wasmdViewKeeper: wasmdViewKeeper,
		evmKeeper:       evmKeeper,
		bankKeeper:      bankKeeper,
		address:         common.HexToAddress(WasmdAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case InstantiateMethod:
			executor.InstantiateID = m.ID
		case ExecuteMethod:
			executor.ExecuteID = m.ID
		case ExecuteBatchMethod:
			executor.ExecuteBatchID = m.ID
		case QueryMethod:
			executor.QueryID = m.ID
		}
	}
	return pcommon.NewDynamicGasPrecompile(newAbi, executor, common.HexToAddress(WasmdAddress), "wasmd"), nil
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64) (ret []byte, remainingGas uint64, err error) {
	if method.Name != QueryMethod && !ctx.IsEVM() {
		return nil, 0, errors.New("sei does not support CW->EVM->CW call pattern")
	}
	switch method.Name {
	case InstantiateMethod:
		return p.instantiate(ctx, method, caller, callingContract, args, value, readOnly)
	case ExecuteMethod:
		return p.execute(ctx, method, caller, callingContract, args, value, readOnly)
	case ExecuteBatchMethod:
		return p.executeBatch(ctx, method, caller, callingContract, args, value, readOnly)
	case QueryMethod:
		return p.query(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) EVMKeeper() pcommon.EVMKeeper {
	return p.evmKeeper
}

func (p PrecompileExecutor) instantiate(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool) (ret []byte, remainingGas uint64, rerr error) {
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
		rerr = types.NewAssociationMissingErr(caller.Hex())
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
		coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), creatorAddr, useiAmtAsWei, p.bankKeeper)
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

func (p PrecompileExecutor) executeBatch(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool) (ret []byte, remainingGas uint64, rerr error) {
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
	// Copy to avoid modifying the original value
	var valueCopy *big.Int
	if value != nil {
		valueCopy = new(big.Int).Set(value)
	} else {
		valueCopy = value
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
			rerr = types.NewAssociationMissingErr(caller.Hex())
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
		if valueCopy != nil && !useiAmt.IsZero() {
			// process coin amount from the value provided
			useiAmtAsWei := useiAmt.Mul(state.SdkUseiToSweiMultiplier).BigInt()
			coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), senderAddr, useiAmtAsWei, p.bankKeeper)
			if err != nil {
				rerr = err
				return
			}
			valueCopy.Sub(valueCopy, useiAmtAsWei)
			if valueCopy.Sign() == -1 {
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
	if valueCopy != nil && valueCopy.Sign() != 0 {
		rerr = errors.New("value remaining after execution, must match provided amounts exactly")
		return
	}
	ret, rerr = method.Outputs.Pack(responses)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool) (ret []byte, remainingGas uint64, rerr error) {
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
		rerr = types.NewAssociationMissingErr(caller.Hex())
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
		coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), senderAddr, useiAmtAsWei, p.bankKeeper)
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

func (p PrecompileExecutor) query(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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
