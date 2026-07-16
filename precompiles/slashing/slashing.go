package slashing

import (
	"embed"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
)

const (
	ParamsMethod       = "params"
	SigningInfoMethod  = "signingInfo"
	SigningInfosMethod = "signingInfos"
)

const (
	SlashingAddress = "0x0000000000000000000000000000000000001014"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper       utils.EVMKeeper
	slashingQuerier utils.SlashingQuerier

	ParamsID       []byte
	SigningInfoID  []byte
	SigningInfosID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:       keepers.EVMK(),
		slashingQuerier: keepers.SlashingQ(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case ParamsMethod:
			p.ParamsID = m.ID
		case SigningInfoMethod:
			p.SigningInfoID = m.ID
		case SigningInfosMethod:
			p.SigningInfosID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(SlashingAddress), "slashing"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (bz []byte, remainingGas uint64, err error) {
	// Needed to catch gas meter panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
		}
	}()
	switch method.Name {
	case ParamsMethod:
		return p.params(ctx, method, args, value)
	case SigningInfoMethod:
		return p.signingInfo(ctx, method, args, value)
	case SigningInfosMethod:
		return p.signingInfos(ctx, method, args, value)
	}
	return
}

type SlashingParams struct {
	SignedBlocksWindow      int64
	MinSignedPerWindow      string
	DowntimeJailDuration    uint64
	SlashFractionDoubleSign string
	SlashFractionDowntime   string
}

type SigningInfo struct {
	ValidatorAddress    string
	StartHeight         int64
	IndexOffset         int64
	JailedUntil         int64
	Tombstoned          bool
	MissedBlocksCounter int64
}

type SigningInfosResponse struct {
	SigningInfos []SigningInfo
	NextKey      []byte
}

func (p PrecompileExecutor) params(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	request := &slashingtypes.QueryParamsRequest{}
	response, err := p.slashingQuerier.Params(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	params := SlashingParams{
		SignedBlocksWindow:      response.Params.SignedBlocksWindow,
		MinSignedPerWindow:      response.Params.MinSignedPerWindow.String(),
		DowntimeJailDuration:    uint64(response.Params.DowntimeJailDuration.Seconds()),
		SlashFractionDoubleSign: response.Params.SlashFractionDoubleSign.String(),
		SlashFractionDowntime:   response.Params.SlashFractionDowntime.String(),
	}

	bz, err := method.Outputs.Pack(params)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) signingInfo(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	req := &slashingtypes.QuerySigningInfoRequest{
		ConsAddress: args[0].(string),
	}

	resp, err := p.slashingQuerier.SigningInfo(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	signingInfo := convertSigningInfoToPrecompileType(resp.ValSigningInfo)

	bz, err := method.Outputs.Pack(signingInfo)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) signingInfos(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	req := &slashingtypes.QuerySigningInfosRequest{
		Pagination: &query.PageRequest{
			Key: args[0].([]byte),
		},
	}

	resp, err := p.slashingQuerier.SigningInfos(sdk.WrapSDKContext(ctx), req)
	if err != nil {
		return nil, 0, err
	}

	res := SigningInfosResponse{
		SigningInfos: make([]SigningInfo, len(resp.Info)),
	}
	for i, info := range resp.Info {
		res.SigningInfos[i] = convertSigningInfoToPrecompileType(info)
	}
	if resp.Pagination != nil {
		res.NextKey = resp.Pagination.NextKey
	}

	bz, err := method.Outputs.Pack(res)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func convertSigningInfoToPrecompileType(info slashingtypes.ValidatorSigningInfo) SigningInfo {
	return SigningInfo{
		ValidatorAddress:    info.Address,
		StartHeight:         info.StartHeight,
		IndexOffset:         info.IndexOffset,
		JailedUntil:         info.JailedUntil.Unix(),
		Tombstoned:          info.Tombstoned,
		MissedBlocksCounter: info.MissedBlocksCounter,
	}
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
