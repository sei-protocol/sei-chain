package keeper

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"runtime/debug"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	cmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
	artifactsutils "github.com/sei-protocol/sei-chain/x/evm/artifacts/utils"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type msgServer struct {
	*Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func (server msgServer) EVMTransaction(goCtx context.Context, msg *types.MsgEVMTransaction) (serverRes *types.MsgEVMTransactionResponse, err error) {
	if msg.IsAssociateTx() {
		// no-op in msg server for associate tx; all the work have been done in ante handler
		return &types.MsgEVMTransactionResponse{}, nil
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	// EVM has a special case here, mainly because for an EVM transaction the gas limit is set on EVM payload level, not on top-level GasWanted field
	// as normal transactions (because existing eth client can't). As a result EVM has its own dedicated ante handler chain. The full sequence is:

	// 	1. At the beginning of the ante handler chain, gas meter is set to infinite so that the ante processing itself won't run out of gas (EVM ante is pretty light but it does read a parameter or two)
	// 	2. At the end of the ante handler chain, gas meter is set based on the gas limit specified in the EVM payload; this is only to provide a GasWanted return value to tendermint mempool when CheckTx returns, and not used for anything else.
	// 	3. At the beginning of message server (here), gas meter is set to infinite again, because EVM internal logic will then take over and manage out-of-gas scenarios.
	// 	4. At the end of message server, gas consumed by EVM is adjusted to Sei's unit and counted in the original gas meter, because that original gas meter will be used to count towards block gas after message server returns
	originalGasMeter := ctx.GasMeter()
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())

	stateDB := state.NewDBImpl(ctx, &server, false)
	stateDB.AddSurplus(msg.Derived.AnteSurplus)
	tx, _ := msg.AsTransaction()
	emsg := server.GetEVMMessage(ctx, tx, msg.Derived.SenderEVMAddr)
	gp := server.GetGasPool()

	defer func() {
		if pe := recover(); pe != nil {
			// there is not supposed to be any panic
			debug.PrintStack()
			ctx.Logger().Error(fmt.Sprintf("EVM PANIC: %s", pe))
			telemetry.IncrCounter(1, types.ModuleName, "panics")
			server.AppendErrorToEvmTxDeferredInfo(ctx, tx.Hash(), fmt.Sprintf("%s", pe))

			panic(pe)
		}
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Got EVM state transition error (not VM error): %s", err))

			telemetry.IncrCounterWithLabels(
				[]string{types.ModuleName, "errors", "state_transition"},
				1,
				[]metrics.Label{
					telemetry.NewLabel("type", err.Error()),
				},
			)
			return
		}
		receipt, err := server.writeReceipt(ctx, msg, tx, emsg, serverRes, stateDB)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to write EVM receipt: %s", err))

			telemetry.IncrCounterWithLabels(
				[]string{types.ModuleName, "errors", "write_receipt"},
				1,
				[]metrics.Label{
					telemetry.NewLabel("type", err.Error()),
				},
			)
			return
		}
		surplus, ferr := stateDB.Finalize()
		if ferr != nil {
			err = ferr
			ctx.Logger().Error(fmt.Sprintf("failed to finalize EVM stateDB: %s", err))

			telemetry.IncrCounterWithLabels(
				[]string{types.ModuleName, "errors", "stateDB_finalize"},
				1,
				[]metrics.Label{
					telemetry.NewLabel("type", err.Error()),
				},
			)

			return
		}
		bloom := ethtypes.Bloom{}
		bloom.SetBytes(receipt.LogsBloom)
		server.AppendToEvmTxDeferredInfo(ctx, bloom, tx.Hash(), surplus)

		// GasUsed in serverRes is in EVM's gas unit, not Sei's gas unit.
		// PriorityNormalizer is the coefficient that's used to adjust EVM
		// transactions' priority, which is based on gas limit in EVM unit,
		// to Sei transactions' priority, which is based on gas limit in
		// Sei unit, so we use the same coefficient to convert gas unit here.
		adjustedGasUsed := server.GetPriorityNormalizer(ctx).MulInt64(int64(serverRes.GasUsed))
		originalGasMeter.ConsumeGas(adjustedGasUsed.TruncateInt().Uint64(), "evm transaction")
	}()

	res, applyErr := server.applyEVMMessage(ctx, emsg, stateDB, gp)
	serverRes = &types.MsgEVMTransactionResponse{
		Hash: tx.Hash().Hex(),
	}
	if applyErr != nil {
		// This should not happen, as anything that could cause applyErr is supposed to
		// be checked in CheckTx first
		err = applyErr

		telemetry.IncrCounterWithLabels(
			[]string{types.ModuleName, "errors", "apply_message"},
			1,
			[]metrics.Label{
				telemetry.NewLabel("type", err.Error()),
			},
		)

	} else {
		// if applyErr is nil then res must be non-nil
		if res.Err != nil {
			serverRes.VmError = res.Err.Error()

			telemetry.IncrCounterWithLabels(
				[]string{types.ModuleName, "errors", "vm_execution"},
				1,
				[]metrics.Label{
					telemetry.NewLabel("type", serverRes.VmError),
				},
			)
		}
		serverRes.GasUsed = res.UsedGas
		serverRes.ReturnData = res.ReturnData
	}

	return
}

func (k *Keeper) GetGasPool() core.GasPool {
	return math.MaxUint64
}

func (k *Keeper) GetEVMMessage(ctx sdk.Context, tx *ethtypes.Transaction, sender common.Address) *core.Message {
	msg := &core.Message{
		Nonce:             tx.Nonce(),
		GasLimit:          tx.Gas(),
		GasPrice:          new(big.Int).Set(tx.GasPrice()),
		GasFeeCap:         new(big.Int).Set(tx.GasFeeCap()),
		GasTipCap:         new(big.Int).Set(tx.GasTipCap()),
		To:                tx.To(),
		Value:             tx.Value(),
		Data:              tx.Data(),
		AccessList:        tx.AccessList(),
		SkipAccountChecks: false,
		BlobHashes:        tx.BlobHashes(),
		BlobGasFeeCap:     tx.BlobGasFeeCap(),
		From:              sender,
	}
	// If baseFee provided, set gasPrice to effectiveGasPrice.
	baseFee := k.GetBaseFee(ctx)
	if baseFee != nil {
		msg.GasPrice = cmath.BigMin(msg.GasPrice.Add(msg.GasTipCap, baseFee), msg.GasFeeCap)
	}
	return msg
}

func (k Keeper) applyEVMMessage(ctx sdk.Context, msg *core.Message, stateDB *state.DBImpl, gp core.GasPool) (*core.ExecutionResult, error) {
	blockCtx, err := k.GetVMBlockContext(ctx, gp)
	if err != nil {
		return nil, err
	}
	cfg := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	txCtx := core.NewEVMTxContext(msg)
	evmInstance := vm.NewEVM(*blockCtx, txCtx, stateDB, cfg, vm.Config{})
	stateDB.SetEVM(evmInstance)
	st := core.NewStateTransition(evmInstance, msg, &gp, true) // fee already charged in ante handler
	res, err := st.TransitionDb()
	return res, err
}

func (server msgServer) writeReceipt(ctx sdk.Context, origMsg *types.MsgEVMTransaction, tx *ethtypes.Transaction, msg *core.Message, response *types.MsgEVMTransactionResponse, stateDB *state.DBImpl) (*types.Receipt, error) {
	ethLogs := stateDB.GetAllLogs()
	bloom := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: ethLogs}})
	receipt := &types.Receipt{
		TxType:            uint32(tx.Type()),
		CumulativeGasUsed: uint64(0),
		TxHashHex:         tx.Hash().Hex(),
		GasUsed:           response.GasUsed,
		BlockNumber:       uint64(ctx.BlockHeight()),
		TransactionIndex:  uint32(ctx.TxIndex()),
		EffectiveGasPrice: tx.GasPrice().Uint64(),
		VmError:           response.VmError,
		Logs:              utils.Map(ethLogs, ConvertEthLog),
		LogsBloom:         bloom[:],
	}

	if msg.To == nil {
		receipt.ContractAddress = crypto.CreateAddress(msg.From, msg.Nonce).Hex()
	} else {
		receipt.To = msg.To.Hex()
		if len(msg.Data) > 0 {
			receipt.ContractAddress = msg.To.Hex()
		}
	}

	if response.VmError == "" {
		receipt.Status = uint32(ethtypes.ReceiptStatusSuccessful)
	} else {
		receipt.Status = uint32(ethtypes.ReceiptStatusFailed)
	}

	receipt.From = origMsg.Derived.SenderEVMAddr.Hex()

	return receipt, server.SetReceipt(ctx, tx.Hash(), receipt)
}

func (server msgServer) Send(goCtx context.Context, msg *types.MsgSend) (*types.MsgSendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	recipient := server.GetSeiAddressOrDefault(ctx, common.HexToAddress(msg.ToAddress))
	_, err := bankkeeper.NewMsgServerImpl(server.BankKeeper()).Send(goCtx, &banktypes.MsgSend{
		FromAddress: msg.FromAddress,
		ToAddress:   recipient.String(),
		Amount:      msg.Amount,
	})
	if err != nil {
		return nil, err
	}
	return &types.MsgSendResponse{}, nil
}

func (server msgServer) RegisterPointer(goCtx context.Context, msg *types.MsgRegisterPointer) (*types.MsgRegisterPointerResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	var existingPointer sdk.AccAddress
	var existingVersion uint16
	var currentVersion uint16
	var exists bool
	switch msg.PointerType {
	case types.PointerType_ERC20:
		currentVersion = erc20.CurrentVersion
		existingPointer, existingVersion, exists = server.GetCW20ERC20Pointer(ctx, common.HexToAddress(msg.ErcAddress))
	case types.PointerType_ERC721:
		currentVersion = erc721.CurrentVersion
		existingPointer, existingVersion, exists = server.GetCW721ERC721Pointer(ctx, common.HexToAddress(msg.ErcAddress))
	default:
		panic("unknown pointer type")
	}
	if exists && existingVersion >= currentVersion {
		return nil, fmt.Errorf("pointer %s already registered at version %d", existingPointer.String(), existingVersion)
	}
	store := server.PrefixStore(ctx, types.PointerCWCodePrefix)
	payload := map[string]interface{}{}
	switch msg.PointerType {
	case types.PointerType_ERC20:
		store = prefix.NewStore(store, types.PointerCW20ERC20Prefix)
		payload["erc20_address"] = msg.ErcAddress
	case types.PointerType_ERC721:
		store = prefix.NewStore(store, types.PointerCW721ERC721Prefix)
		payload["erc721_address"] = msg.ErcAddress
	default:
		panic("unknown pointer type")
	}
	codeID := binary.BigEndian.Uint64(store.Get(artifactsutils.GetVersionBz(currentVersion)))
	bz, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	moduleAcct := server.accountKeeper.GetModuleAddress(types.ModuleName)
	pointerAddr, _, err := server.wasmKeeper.Instantiate(ctx, codeID, moduleAcct, moduleAcct, bz, fmt.Sprintf("Pointer of %s", msg.ErcAddress), sdk.NewCoins())
	if err != nil {
		return nil, err
	}
	switch msg.PointerType {
	case types.PointerType_ERC20:
		err = server.SetCW20ERC20Pointer(ctx, common.HexToAddress(msg.ErcAddress), pointerAddr.String())
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "erc20"),
			sdk.NewAttribute(types.AttributeKeyPointerAddress, pointerAddr.String()), sdk.NewAttribute(types.AttributeKeyPointee, msg.ErcAddress),
			sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", erc20.CurrentVersion))))
	case types.PointerType_ERC721:
		err = server.SetCW721ERC721Pointer(ctx, common.HexToAddress(msg.ErcAddress), pointerAddr.String())
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "erc721"),
			sdk.NewAttribute(types.AttributeKeyPointerAddress, pointerAddr.String()), sdk.NewAttribute(types.AttributeKeyPointee, msg.ErcAddress),
			sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", erc721.CurrentVersion))))
	default:
		panic("unknown pointer type")
	}
	return &types.MsgRegisterPointerResponse{PointerAddress: pointerAddr.String()}, err
}
