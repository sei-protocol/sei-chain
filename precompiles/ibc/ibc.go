package ibc

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"

	"github.com/cosmos/cosmos-sdk/types/bech32"

	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"

	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	TransferMethod                   = "transfer"
	TransferWithDefaultTimeoutMethod = "transferWithDefaultTimeout"
)

const (
	IBCAddress = "0x0000000000000000000000000000000000001009"
)

var _ vm.PrecompiledContract = &Precompile{}
var _ vm.DynamicGasPrecompiledContract = &Precompile{}

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

type Precompile struct {
	pcommon.Precompile
	address          common.Address
	transferKeeper   pcommon.TransferKeeper
	evmKeeper        pcommon.EVMKeeper
	clientKeeper     pcommon.ClientKeeper
	connectionKeeper pcommon.ConnectionKeeper
	channelKeeper    pcommon.ChannelKeeper

	TransferID                   []byte
	TransferWithDefaultTimeoutID []byte
}

func NewPrecompile(
	transferKeeper pcommon.TransferKeeper,
	evmKeeper pcommon.EVMKeeper,
	clientKeeper pcommon.ClientKeeper,
	connectionKeeper pcommon.ConnectionKeeper,
	channelKeeper pcommon.ChannelKeeper) (*Precompile, error) {
	newAbi := GetABI()

	p := &Precompile{
		Precompile:       pcommon.Precompile{ABI: newAbi},
		address:          common.HexToAddress(IBCAddress),
		transferKeeper:   transferKeeper,
		evmKeeper:        evmKeeper,
		clientKeeper:     clientKeeper,
		connectionKeeper: connectionKeeper,
		channelKeeper:    channelKeeper,
	}

	for name, m := range newAbi.Methods {
		switch name {
		case TransferMethod:
			p.TransferID = m.ID
		case TransferWithDefaultTimeoutMethod:
			p.TransferWithDefaultTimeoutID = m.ID
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

func (p Precompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, suppliedGas uint64, _ *big.Int, _ *tracing.Hooks, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	defer func() {
		if err != nil {
			evm.StateDB.(*state.DBImpl).SetPrecompileError(err)
		}
	}()
	if readOnly {
		return nil, 0, errors.New("cannot call IBC precompile from staticcall")
	}
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, 0, err
	}
	if caller.Cmp(callingContract) != 0 {
		return nil, 0, errors.New("cannot delegatecall IBC")
	}

	gasMultiplier := p.evmKeeper.GetPriorityNormalizer(ctx)
	gasLimitBigInt := new(big.Int).Mul(new(big.Int).SetUint64(suppliedGas), gasMultiplier.TruncateInt().BigInt())
	if gasLimitBigInt.Cmp(utils.BigMaxU64) > 0 {
		gasLimitBigInt = utils.BigMaxU64
	}
	ctx = ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, gasLimitBigInt.Uint64()))

	switch method.Name {
	case TransferMethod:
		return p.transfer(ctx, method, args, caller)
	case TransferWithDefaultTimeoutMethod:
		return p.transferWithDefaultTimeout(ctx, method, args, caller)
	}
	return
}

func (p Precompile) Run(*vm.EVM, common.Address, common.Address, []byte, *big.Int, bool) (bz []byte, err error) {
	panic("static gas Run is not implemented for dynamic gas precompile")
}

func (p Precompile) transfer(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 9); err != nil {
		rerr = err
		return
	}
	validatedArgs, err := p.validateCommonArgs(ctx, args, caller)
	if err != nil {
		rerr = err
		return
	}

	if validatedArgs.amount.Cmp(big.NewInt(0)) == 0 {
		// short circuit
		remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
		ret, rerr = method.Outputs.Pack(true)
		return
	}

	coin := sdk.Coin{
		Denom:  validatedArgs.denom,
		Amount: sdk.NewIntFromBigInt(validatedArgs.amount),
	}

	revisionNumber, ok := args[5].(uint64)
	if !ok {
		rerr = errors.New("revisionNumber is not a uint64")
		return
	}

	revisionHeight, ok := args[6].(uint64)
	if !ok {
		rerr = errors.New("revisionHeight is not a uint64")
		return
	}

	height := clienttypes.Height{
		RevisionNumber: revisionNumber,
		RevisionHeight: revisionHeight,
	}

	timeoutTimestamp, ok := args[7].(uint64)
	if !ok {
		rerr = errors.New("timeoutTimestamp is not a uint64")
		return
	}

	msg := types.MsgTransfer{
		SourcePort:       validatedArgs.port,
		SourceChannel:    validatedArgs.channelID,
		Token:            coin,
		Sender:           validatedArgs.senderSeiAddr.String(),
		Receiver:         validatedArgs.receiverAddressString,
		TimeoutHeight:    height,
		TimeoutTimestamp: timeoutTimestamp,
	}

	msg = addMemo(args[8], msg)

	err = msg.ValidateBasic()
	if err != nil {
		rerr = err
		return
	}

	_, err = p.transferKeeper.Transfer(sdk.WrapSDKContext(ctx), &msg)

	if err != nil {
		rerr = err
		return
	}
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	ret, rerr = method.Outputs.Pack(true)
	return
}

func (p Precompile) transferWithDefaultTimeout(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()

	if err := pcommon.ValidateArgsLength(args, 6); err != nil {
		rerr = err
		return
	}
	validatedArgs, err := p.validateCommonArgs(ctx, args, caller)
	if err != nil {
		rerr = err
		return
	}

	if validatedArgs.amount.Cmp(big.NewInt(0)) == 0 {
		// short circuit
		remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
		ret, rerr = method.Outputs.Pack(true)
		return
	}

	coin := sdk.Coin{
		Denom:  validatedArgs.denom,
		Amount: sdk.NewIntFromBigInt(validatedArgs.amount),
	}

	connection, err := p.getChannelConnection(ctx, validatedArgs.port, validatedArgs.channelID)

	if err != nil {
		rerr = err
		return
	}

	latestConsensusHeight, err := p.getConsensusLatestHeight(ctx, *connection)
	if err != nil {
		rerr = err
		return
	}

	height, err := GetAdjustedHeight(*latestConsensusHeight)
	if err != nil {
		rerr = err
		return
	}

	timeoutTimestamp, err := p.GetAdjustedTimestamp(ctx, connection.ClientId, *latestConsensusHeight)
	if err != nil {
		rerr = err
		return
	}

	msg := types.MsgTransfer{
		SourcePort:       validatedArgs.port,
		SourceChannel:    validatedArgs.channelID,
		Token:            coin,
		Sender:           validatedArgs.senderSeiAddr.String(),
		Receiver:         validatedArgs.receiverAddressString,
		TimeoutHeight:    height,
		TimeoutTimestamp: timeoutTimestamp,
	}

	msg = addMemo(args[5], msg)

	err = msg.ValidateBasic()
	if err != nil {
		rerr = err
		return
	}

	_, err = p.transferKeeper.Transfer(sdk.WrapSDKContext(ctx), &msg)

	if err != nil {
		rerr = err
		return
	}
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	ret, rerr = method.Outputs.Pack(true)
	return
}

func (Precompile) IsTransaction(method string) bool {
	switch method {
	case TransferMethod:
		return true
	case TransferWithDefaultTimeoutMethod:
		return true
	default:
		return false
	}
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) GetName() string {
	return "ibc"
}

func (p Precompile) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	seiAddr, found := p.evmKeeper.GetSeiAddress(ctx, addr)
	if !found {
		return nil, fmt.Errorf("EVM address %s is not associated", addr.Hex())
	}
	return seiAddr, nil
}

func (p Precompile) getChannelConnection(ctx sdk.Context, port string, channelID string) (*connectiontypes.ConnectionEnd, error) {
	channel, found := p.channelKeeper.GetChannel(ctx, port, channelID)
	if !found {
		return nil, errors.New("channel not found")
	}

	connection, found := p.connectionKeeper.GetConnection(ctx, channel.ConnectionHops[0])

	if !found {
		return nil, errors.New("connection not found")
	}
	return &connection, nil
}

func (p Precompile) getConsensusLatestHeight(ctx sdk.Context, connection connectiontypes.ConnectionEnd) (*clienttypes.Height, error) {
	clientState, found := p.clientKeeper.GetClientState(ctx, connection.ClientId)

	if !found {
		return nil, errors.New("could not get the client state")
	}

	latestHeight := clientState.GetLatestHeight()
	return &clienttypes.Height{
		RevisionNumber: latestHeight.GetRevisionNumber(),
		RevisionHeight: latestHeight.GetRevisionHeight(),
	}, nil
}

func GetAdjustedHeight(latestConsensusHeight clienttypes.Height) (clienttypes.Height, error) {
	defaultTimeoutHeight, err := clienttypes.ParseHeight(types.DefaultRelativePacketTimeoutHeight)
	if err != nil {
		return clienttypes.Height{}, err
	}

	absoluteHeight := latestConsensusHeight
	absoluteHeight.RevisionNumber += defaultTimeoutHeight.RevisionNumber
	absoluteHeight.RevisionHeight += defaultTimeoutHeight.RevisionHeight
	return absoluteHeight, nil
}

func (p Precompile) GetAdjustedTimestamp(ctx sdk.Context, clientId string, height clienttypes.Height) (uint64, error) {
	consensusState, found := p.clientKeeper.GetClientConsensusState(ctx, clientId, height)
	var consensusStateTimestamp uint64
	if found {
		consensusStateTimestamp = consensusState.GetTimestamp()
	}

	defaultRelativePacketTimeoutTimestamp := types.DefaultRelativePacketTimeoutTimestamp
	blockTime := ctx.BlockTime().UnixNano()
	if blockTime > 0 {
		now := uint64(blockTime)
		if now > consensusStateTimestamp {
			return now + defaultRelativePacketTimeoutTimestamp, nil
		} else {
			return consensusStateTimestamp + defaultRelativePacketTimeoutTimestamp, nil
		}
	} else {
		return 0, errors.New("block time is not greater than Jan 1st, 1970 12:00 AM")
	}
}

type ValidatedArgs struct {
	senderSeiAddr         sdk.AccAddress
	receiverAddressString string
	port                  string
	channelID             string
	denom                 string
	amount                *big.Int
}

func (p Precompile) validateCommonArgs(ctx sdk.Context, args []interface{}, caller common.Address) (*ValidatedArgs, error) {
	senderSeiAddr, ok := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !ok {
		return nil, errors.New("caller is not a valid SEI address")
	}

	receiverAddressString, ok := args[0].(string)
	if !ok {
		return nil, errors.New("receiverAddress is not a string")
	}
	_, bz, err := bech32.DecodeAndConvert(receiverAddressString)
	if err != nil {
		return nil, err
	}
	err = sdk.VerifyAddressFormat(bz)
	if err != nil {
		return nil, err
	}

	port, ok := args[1].(string)
	if !ok {
		return nil, errors.New("port is not a string")
	}
	if port == "" {
		return nil, errors.New("port cannot be empty")
	}

	channelID, ok := args[2].(string)
	if !ok {
		return nil, errors.New("channelID is not a string")
	}
	if channelID == "" {
		return nil, errors.New("channelID cannot be empty")
	}

	denom := args[3].(string)
	if denom == "" {
		return nil, errors.New("invalid denom")
	}

	amount, ok := args[4].(*big.Int)
	if !ok {
		return nil, errors.New("amount is not a big.Int")
	}
	return &ValidatedArgs{
		senderSeiAddr:         senderSeiAddr,
		receiverAddressString: receiverAddressString,
		port:                  port,
		channelID:             channelID,
		denom:                 denom,
		amount:                amount,
	}, nil
}

func addMemo(memoArg interface{}, transferMsg types.MsgTransfer) types.MsgTransfer {
	memo := ""
	if memoArg != nil {
		memo = memoArg.(string)
	}
	transferMsg.Memo = memo
	return transferMsg
}
