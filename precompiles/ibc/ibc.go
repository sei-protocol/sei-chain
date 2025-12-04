package ibc

import (
	"embed"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v3/modules/core/03-connection/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	TransferMethod                   = "transfer"
	TransferWithDefaultTimeoutMethod = "transferWithDefaultTimeout"
)

const (
	IBCAddress = "0x0000000000000000000000000000000000001009"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	transferKeeper   utils.TransferKeeper
	evmKeeper        utils.EVMKeeper
	clientKeeper     utils.ClientKeeper
	connectionKeeper utils.ConnectionKeeper
	channelKeeper    utils.ChannelKeeper

	TransferID                   []byte
	TransferWithDefaultTimeoutID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		transferKeeper:   keepers.TransferK(),
		evmKeeper:        keepers.EVMK(),
		clientKeeper:     keepers.ClientK(),
		connectionKeeper: keepers.ConnectionK(),
		channelKeeper:    keepers.ChannelK(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case TransferMethod:
			p.TransferID = m.ID
		case TransferWithDefaultTimeoutMethod:
			p.TransferWithDefaultTimeoutID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(IBCAddress), "ibc"), nil
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, _ *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	if err = pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if readOnly {
		return nil, 0, errors.New("cannot call IBC precompile from staticcall")
	}
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall IBC")
	}

	switch method.Name {
	case TransferMethod:
		return p.transfer(ctx, method, args, caller)
	case TransferWithDefaultTimeoutMethod:
		return p.transferWithDefaultTimeout(ctx, method, args, caller)
	}
	return
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (p PrecompileExecutor) transfer(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address) (ret []byte, remainingGas uint64, rerr error) {
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

func (p PrecompileExecutor) transferWithDefaultTimeout(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address) (ret []byte, remainingGas uint64, rerr error) {
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

func (p PrecompileExecutor) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	seiAddr, found := p.evmKeeper.GetSeiAddress(ctx, addr)
	if !found {
		return nil, evmtypes.NewAssociationMissingErr(addr.Hex())
	}
	return seiAddr, nil
}

func (p PrecompileExecutor) getChannelConnection(ctx sdk.Context, port string, channelID string) (*connectiontypes.ConnectionEnd, error) {
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

func (p PrecompileExecutor) getConsensusLatestHeight(ctx sdk.Context, connection connectiontypes.ConnectionEnd) (*clienttypes.Height, error) {
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

func (p PrecompileExecutor) GetAdjustedTimestamp(ctx sdk.Context, clientId string, height clienttypes.Height) (uint64, error) {
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

func (p PrecompileExecutor) validateCommonArgs(ctx sdk.Context, args []interface{}, caller common.Address) (*ValidatedArgs, error) {
	senderSeiAddr, ok := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !ok {
		return nil, errors.New("caller is not a valid SEI address")
	}

	receiverAddressString, ok := args[0].(string)
	if !ok || receiverAddressString == "" {
		return nil, errors.New("receiverAddress is not a string or empty")
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
