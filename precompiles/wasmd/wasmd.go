package wasmd

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	InstantiateMethod      = "instantiate"
	ExecuteMethod          = "execute"
	ExecuteBatchMethod     = "execute_batch"
	QueryMethod            = "query"
	ContractInfoMethod     = "contractInfo"
	ContractHistoryMethod  = "contractHistory"
	ContractsByCodeMethod  = "contractsByCode"
	AllContractStateMethod = "allContractState"
	RawContractStateMethod = "rawContractState"
	CodeMethod             = "code"
	CodesMethod            = "codes"
	PinnedCodesMethod      = "pinnedCodes"
)

const WasmdAddress = "0x0000000000000000000000000000000000001002"
const PrecompileName = "wasmd"

var (
	Address                 = common.HexToAddress(WasmdAddress)
	ErrExecuteBatchDisabled = errors.New("wasmd execute_batch is disabled")
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper       utils.EVMKeeper
	bankKeeper      utils.BankKeeper
	wasmdKeeper     utils.WasmdKeeper
	wasmdViewKeeper utils.WasmdViewKeeper
	address         common.Address

	InstantiateID      []byte
	ExecuteID          []byte
	ExecuteBatchID     []byte
	QueryID            []byte
	ContractInfoID     []byte
	ContractHistoryID  []byte
	ContractsByCodeID  []byte
	AllContractStateID []byte
	RawContractStateID []byte
	CodeID             []byte
	CodesID            []byte
	PinnedCodesID      []byte
}

type ExecuteMsg struct {
	ContractAddress string `json:"contractAddress"`
	Msg             []byte `json:"msg"`
	Coins           []byte `json:"coins"`
}

func GetABI() abi.ABI {
	return pcommon.MustGetABI(f, "abi.json")
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := GetABI()

	executor := &PrecompileExecutor{
		wasmdKeeper:     keepers.WasmdK(),
		wasmdViewKeeper: keepers.WasmdVK(),
		evmKeeper:       keepers.EVMK(),
		bankKeeper:      keepers.BankK(),
		address:         Address,
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
		case ContractInfoMethod:
			executor.ContractInfoID = m.ID
		case ContractHistoryMethod:
			executor.ContractHistoryID = m.ID
		case ContractsByCodeMethod:
			executor.ContractsByCodeID = m.ID
		case AllContractStateMethod:
			executor.AllContractStateID = m.ID
		case RawContractStateMethod:
			executor.RawContractStateID = m.ID
		case CodeMethod:
			executor.CodeID = m.ID
		case CodesMethod:
			executor.CodesID = m.ID
		case PinnedCodesMethod:
			executor.PinnedCodesID = m.ID
		}
	}
	return pcommon.NewDynamicGasPrecompile(newAbi, executor, Address, PrecompileName), nil
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	if !isQueryMethod(method.Name) && !ctx.IsEVM() {
		return nil, 0, errors.New("sei does not support CW->EVM->CW call pattern")
	}
	switch method.Name {
	case InstantiateMethod:
		return p.instantiate(ctx, method, caller, callingContract, args, value, readOnly, hooks, evm)
	case ExecuteMethod:
		return p.execute(ctx, method, caller, callingContract, args, value, readOnly, hooks, evm)
	case ExecuteBatchMethod:
		return nil, pcommon.GetRemainingGas(ctx, p.evmKeeper), ErrExecuteBatchDisabled
	case QueryMethod:
		return p.query(ctx, method, args, value)
	case ContractInfoMethod:
		return p.contractInfo(ctx, method, args, value)
	case ContractHistoryMethod:
		return p.contractHistory(ctx, method, args, value)
	case ContractsByCodeMethod:
		return p.contractsByCode(ctx, method, args, value)
	case AllContractStateMethod:
		return p.allContractState(ctx, method, args, value)
	case RawContractStateMethod:
		return p.rawContractState(ctx, method, args, value)
	case CodeMethod:
		return p.code(ctx, method, args, value)
	case CodesMethod:
		return p.codes(ctx, method, args, value)
	case PinnedCodesMethod:
		return p.pinnedCodes(ctx, method, args, value)
	}
	return
}

// isQueryMethod reports whether method is a read-only wasmd query, which may
// run outside an EVM call context unlike the transaction methods.
func isQueryMethod(method string) bool {
	switch method {
	case QueryMethod, ContractInfoMethod, ContractHistoryMethod, ContractsByCodeMethod,
		AllContractStateMethod, RawContractStateMethod, CodeMethod, CodesMethod, PinnedCodesMethod:
		return true
	default:
		return false
	}
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (p PrecompileExecutor) instantiate(ctx sdk.Context, method *abi.Method, caller common.Address, _ common.Address, args []interface{}, value *big.Int, readOnly bool, hooks *tracing.Hooks, evm *vm.EVM) (ret []byte, remainingGas uint64, rerr error) {
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
	if ctx.EVMPrecompileCalledFromDelegateCall() {
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

func (p PrecompileExecutor) execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, hooks *tracing.Hooks, evm *vm.EVM) (ret []byte, remainingGas uint64, rerr error) {
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
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		erc20pointer, _, erc20exists := p.evmKeeper.GetERC20CW20Pointer(ctx, contractAddrStr)
		erc721pointer, _, erc721exists := p.evmKeeper.GetERC721CW721Pointer(ctx, contractAddrStr)
		erc1155pointer, _, erc1155exists := p.evmKeeper.GetERC1155CW1155Pointer(ctx, contractAddrStr)
		if (!erc20exists || erc20pointer.Cmp(callingContract) != 0) && (!erc721exists || erc721pointer.Cmp(callingContract) != 0) && (!erc1155exists || erc1155pointer.Cmp(callingContract) != 0) {
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
	res, err := p.wasmdViewKeeper.QuerySmartSafe(ctx, contractAddr, req)
	if err != nil {
		rerr = err
		return
	}
	ret, rerr = method.Outputs.Pack(res)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

type ContractInfo struct {
	CodeID    uint64
	Creator   string
	Admin     string
	Label     string
	IbcPortID string
}

type ContractCodeHistoryEntry struct {
	Operation uint8
	CodeID    uint64
	Msg       []byte
}

type Model struct {
	Key   []byte
	Value []byte
}

type AccessConfig struct {
	Permission uint8
	Address    string
}

type CodeInfo struct {
	CodeID                uint64
	Creator               string
	DataHash              []byte
	InstantiatePermission AccessConfig
}

func (p PrecompileExecutor) contractInfo(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	req := &wasmtypes.QueryContractInfoRequest{Address: args[0].(string)}
	response, err := p.wasmdViewKeeper.ContractInfo(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		rerr = err
		return
	}

	ret, rerr = method.Outputs.Pack(ContractInfo{
		CodeID:    response.CodeID,
		Creator:   response.Creator,
		Admin:     response.Admin,
		Label:     response.Label,
		IbcPortID: response.IBCPortID,
	})
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) contractHistory(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	req := &wasmtypes.QueryContractHistoryRequest{
		Address:    args[0].(string),
		Pagination: &query.PageRequest{Key: args[1].([]byte)},
	}
	response, err := p.wasmdViewKeeper.ContractHistory(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		rerr = err
		return
	}

	entries := make([]ContractCodeHistoryEntry, 0, len(response.Entries))
	for _, entry := range response.Entries {
		entries = append(entries, ContractCodeHistoryEntry{
			Operation: uint8(entry.Operation), //nolint:gosec // operation is one of a handful of enum values; no overflow risk
			CodeID:    entry.CodeID,
			Msg:       entry.Msg,
		})
	}
	var nextKey []byte
	if response.Pagination != nil {
		nextKey = response.Pagination.NextKey
	}

	ret, rerr = method.Outputs.Pack(entries, nextKey)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) contractsByCode(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	req := &wasmtypes.QueryContractsByCodeRequest{
		CodeId:     args[0].(uint64),
		Pagination: &query.PageRequest{Key: args[1].([]byte)},
	}
	response, err := p.wasmdViewKeeper.ContractsByCode(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		rerr = err
		return
	}

	contracts := response.Contracts
	if contracts == nil {
		contracts = []string{}
	}
	var nextKey []byte
	if response.Pagination != nil {
		nextKey = response.Pagination.NextKey
	}

	ret, rerr = method.Outputs.Pack(contracts, nextKey)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) allContractState(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	req := &wasmtypes.QueryAllContractStateRequest{
		Address:    args[0].(string),
		Pagination: &query.PageRequest{Key: args[1].([]byte)},
	}
	response, err := p.wasmdViewKeeper.AllContractState(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		rerr = err
		return
	}

	models := make([]Model, 0, len(response.Models))
	for _, model := range response.Models {
		models = append(models, Model{Key: model.Key, Value: model.Value})
	}
	var nextKey []byte
	if response.Pagination != nil {
		nextKey = response.Pagination.NextKey
	}

	ret, rerr = method.Outputs.Pack(models, nextKey)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) rawContractState(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	req := &wasmtypes.QueryRawContractStateRequest{
		Address:   args[0].(string),
		QueryData: args[1].([]byte),
	}
	response, err := p.wasmdViewKeeper.RawContractState(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		rerr = err
		return
	}

	ret, rerr = method.Outputs.Pack(response.Data)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) code(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	req := &wasmtypes.QueryCodeRequest{CodeId: args[0].(uint64)}
	response, err := p.wasmdViewKeeper.Code(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		rerr = err
		return
	}

	info := CodeInfo{
		CodeID:   response.CodeID,
		Creator:  response.Creator,
		DataHash: response.DataHash,
		InstantiatePermission: AccessConfig{
			Permission: uint8(response.InstantiatePermission.Permission), //nolint:gosec // permission is one of a handful of enum values; no overflow risk
			Address:    response.InstantiatePermission.Address,
		},
	}
	ret, rerr = method.Outputs.Pack(info, response.Data)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) codes(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	req := &wasmtypes.QueryCodesRequest{Pagination: &query.PageRequest{Key: args[0].([]byte)}}
	response, err := p.wasmdViewKeeper.Codes(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		rerr = err
		return
	}

	codeInfos := make([]CodeInfo, 0, len(response.CodeInfos))
	for _, info := range response.CodeInfos {
		codeInfos = append(codeInfos, CodeInfo{
			CodeID:   info.CodeID,
			Creator:  info.Creator,
			DataHash: info.DataHash,
			InstantiatePermission: AccessConfig{
				Permission: uint8(info.InstantiatePermission.Permission), //nolint:gosec // permission is one of a handful of enum values; no overflow risk
				Address:    info.InstantiatePermission.Address,
			},
		})
	}
	var nextKey []byte
	if response.Pagination != nil {
		nextKey = response.Pagination.NextKey
	}

	ret, rerr = method.Outputs.Pack(codeInfos, nextKey)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func (p PrecompileExecutor) pinnedCodes(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) (ret []byte, remainingGas uint64, rerr error) {
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

	req := &wasmtypes.QueryPinnedCodesRequest{Pagination: &query.PageRequest{Key: args[0].([]byte)}}
	response, err := p.wasmdViewKeeper.PinnedCodes(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		rerr = err
		return
	}

	codeIDs := response.CodeIDs
	if codeIDs == nil {
		codeIDs = []uint64{}
	}
	var nextKey []byte
	if response.Pagination != nil {
		nextKey = response.Pagination.NextKey
	}

	ret, rerr = method.Outputs.Pack(codeIDs, nextKey)
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	return
}

func IsWasmdCall(to *common.Address) bool {
	return to != nil && (to.Cmp(Address) == 0)
}
