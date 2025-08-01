package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"runtime/debug"
	"strings"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	occtypes "github.com/cosmos/cosmos-sdk/types/occ"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	"github.com/sei-protocol/sei-chain/utils"
	seimetrics "github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/erc721"
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

func (k *Keeper) PrepareCtxForEVMTransaction(ctx sdk.Context, tx *ethtypes.Transaction) (sdk.Context, sdk.GasMeter) {
	isWasmdPrecompileCall := wasmd.IsWasmdCall(tx.To())
	if isWasmdPrecompileCall {
		ctx = ctx.WithEVMEntryViaWasmdPrecompile(true)
	}
	// EVM has a special case here, mainly because for an EVM transaction the gas limit is set on EVM payload level, not on top-level GasWanted field
	// as normal transactions (because existing eth client can't). As a result EVM has its own dedicated ante handler chain. The full sequence is:

	// 	1. At the beginning of the ante handler chain, gas meter is set to infinite so that the ante processing itself won't run out of gas (EVM ante is pretty light but it does read a parameter or two)
	// 	2. At the end of the ante handler chain, gas meter is set based on the gas limit specified in the EVM payload; this is only to provide a GasWanted return value to tendermint mempool when CheckTx returns, and not used for anything else.
	// 	3. At the beginning of message server (here), gas meter is set to infinite again, because EVM internal logic will then take over and manage out-of-gas scenarios.
	// 	4. At the end of message server, gas consumed by EVM is adjusted to Sei's unit and counted in the original gas meter, because that original gas meter will be used to count towards block gas after message server returns
	originalGasMeter := ctx.GasMeter()
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	return ctx, originalGasMeter
}

func (server msgServer) EVMTransaction(goCtx context.Context, msg *types.MsgEVMTransaction) (serverRes *types.MsgEVMTransactionResponse, err error) {
	if msg.IsAssociateTx() {
		// no-op in msg server for associate tx; all the work have been done in ante handler
		return &types.MsgEVMTransactionResponse{}, nil
	}
	ctx := sdk.UnwrapSDKContext(goCtx)
	tx, _ := msg.AsTransaction()
	ctx, originalGasMeter := server.PrepareCtxForEVMTransaction(ctx, tx)

	stateDB := state.NewDBImpl(ctx, &server, false)
	emsg := server.GetEVMMessage(ctx, tx, msg.Derived.SenderEVMAddr)
	gp := server.GetGasPool()

	defer func() {
		defer stateDB.Cleanup()
		if pe := recover(); pe != nil {
			if !strings.Contains(fmt.Sprintf("%s", pe), occtypes.ErrReadEstimate.Error()) {
				debug.PrintStack()
				ctx.Logger().Error(fmt.Sprintf("EVM PANIC: %s", pe))
				seimetrics.SafeTelemetryIncrCounter(1, types.ModuleName, "panics")
			}
			panic(pe)
		}
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("Got EVM state transition error (not VM error): %s", err))

			seimetrics.SafeTelemetryIncrCounterWithLabels(
				[]string{types.ModuleName, "errors", "state_transition"},
				1,
				[]metrics.Label{
					telemetry.NewLabel("type", err.Error()),
				},
			)
			return
		}
		extraSurplus := sdk.ZeroInt()
		surplus, ferr := stateDB.Finalize()
		if ferr != nil {
			err = ferr
			ctx.Logger().Error(fmt.Sprintf("failed to finalize EVM stateDB: %s", err))

			seimetrics.SafeTelemetryIncrCounterWithLabels(
				[]string{types.ModuleName, "errors", "stateDB_finalize"},
				1,
				[]metrics.Label{
					telemetry.NewLabel("type", err.Error()),
				},
			)
			return
		}
		if ctx.EVMEntryViaWasmdPrecompile() {
			syntheticReceipt, err := server.GetTransientReceipt(ctx, ctx.TxSum())
			if err == nil {
				for _, l := range syntheticReceipt.Logs {
					stateDB.AddLog(&ethtypes.Log{
						Address: common.HexToAddress(l.Address),
						Topics:  utils.Map(l.Topics, common.HexToHash),
						Data:    l.Data,
					})
				}
				if syntheticReceipt.VmError != "" {
					serverRes.VmError = fmt.Sprintf("%s\n%s\n", serverRes.VmError, syntheticReceipt.VmError)
				}
				server.DeleteTransientReceipt(ctx, ctx.TxSum())
			}
			syntheticDeferredInfo, found := server.GetEVMTxDeferredInfo(ctx)
			if found {
				extraSurplus = extraSurplus.Add(syntheticDeferredInfo.Surplus)
			}
		}
		receipt, rerr := server.WriteReceipt(ctx, stateDB, emsg, uint32(tx.Type()), tx.Hash(), serverRes.GasUsed, serverRes.VmError)
		if rerr != nil {
			err = rerr
			ctx.Logger().Error(fmt.Sprintf("failed to write EVM receipt: %s", err))

			seimetrics.SafeTelemetryIncrCounterWithLabels(
				[]string{types.ModuleName, "errors", "write_receipt"},
				1,
				[]metrics.Label{
					telemetry.NewLabel("type", err.Error()),
				},
			)
			return
		}

		// Add metrics for receipt status
		if receipt.Status == uint32(ethtypes.ReceiptStatusFailed) {
			seimetrics.SafeTelemetryIncrCounter(1, "receipt", "status", "failed")
		} else {
			seimetrics.SafeTelemetryIncrCounter(1, "receipt", "status", "success")
		}

		surplus = surplus.Add(extraSurplus)
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

	res, applyErr := server.applyEVMMessage(ctx, emsg, stateDB, gp, true)
	serverRes = &types.MsgEVMTransactionResponse{
		Hash: tx.Hash().Hex(),
	}
	if applyErr != nil {
		// This should not happen, as anything that could cause applyErr is supposed to
		// be checked in CheckTx first
		err = applyErr

		seimetrics.SafeTelemetryIncrCounterWithLabels(
			[]string{types.ModuleName, "errors", "apply_message"},
			1,
			[]metrics.Label{
				telemetry.NewLabel("type", err.Error()),
			},
		)

		return
	}

	// if applyErr is nil then res must be non-nil
	if res.Err != nil {
		serverRes.VmError = res.Err.Error()

		seimetrics.SafeTelemetryIncrCounterWithLabels(
			[]string{types.ModuleName, "errors", "vm_execution"},
			1,
			[]metrics.Label{
				telemetry.NewLabel("type", serverRes.VmError),
			},
		)
	}

	serverRes.GasUsed = res.UsedGas
	serverRes.ReturnData = res.ReturnData
	serverRes.Logs = types.NewLogsFromEth(stateDB.GetAllLogs())

	return
}

func (k *Keeper) GetGasPool() core.GasPool {
	return math.MaxUint64
}

func (k *Keeper) GetEVMMessage(ctx sdk.Context, tx *ethtypes.Transaction, sender common.Address) *core.Message {
	msg := &core.Message{
		Nonce:                 tx.Nonce(),
		GasLimit:              tx.Gas(),
		GasPrice:              new(big.Int).Set(tx.GasPrice()),
		GasFeeCap:             new(big.Int).Set(tx.GasFeeCap()),
		GasTipCap:             new(big.Int).Set(tx.GasTipCap()),
		To:                    tx.To(),
		Value:                 tx.Value(),
		Data:                  tx.Data(),
		AccessList:            tx.AccessList(),
		BlobHashes:            tx.BlobHashes(),
		BlobGasFeeCap:         tx.BlobGasFeeCap(),
		SetCodeAuthorizations: tx.SetCodeAuthorizations(),
		From:                  sender,
	}
	// If baseFee provided, set gasPrice to effectiveGasPrice.
	baseFee := k.GetBaseFee(ctx)
	if baseFee != nil {
		msg.GasPrice = msg.GasPrice.Add(msg.GasTipCap, baseFee)
		if msg.GasPrice.Cmp(msg.GasFeeCap) > 0 {
			msg.GasPrice = msg.GasFeeCap
		}
	}
	return msg
}

func (k Keeper) applyEVMMessage(ctx sdk.Context, msg *core.Message, stateDB *state.DBImpl, gp core.GasPool, shouldIncrementNonce bool) (*core.ExecutionResult, error) {
	blockCtx, err := k.GetVMBlockContext(ctx, gp)
	if err != nil {
		return nil, err
	}
	cfg := types.DefaultChainConfig().EthereumConfig(k.ChainID(ctx))
	txCtx := core.NewEVMTxContext(msg)
	evmInstance := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, k.CustomPrecompiles(ctx))
	evmInstance.SetTxContext(txCtx)
	st := core.NewStateTransition(evmInstance, msg, &gp, true, shouldIncrementNonce) // fee already charged in ante handler
	return st.Execute()
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
	if server.GetParams(ctx).RegisterPointerDisabled {
		return nil, fmt.Errorf("registering CW->ERC pointers has been disabled")
	}
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
	case types.PointerType_ERC1155:
		currentVersion = erc1155.CurrentVersion
		existingPointer, existingVersion, exists = server.GetCW1155ERC1155Pointer(ctx, common.HexToAddress(msg.ErcAddress))
	default:
		panic("unknown pointer type")
	}
	if exists && existingVersion >= currentVersion {
		return nil, fmt.Errorf("pointer %s already registered at version %d", existingPointer.String(), existingVersion)
	}
	payload := map[string]interface{}{}
	switch msg.PointerType {
	case types.PointerType_ERC20:
		payload["erc20_address"] = msg.ErcAddress
	case types.PointerType_ERC721:
		payload["erc721_address"] = msg.ErcAddress
	case types.PointerType_ERC1155:
		payload["erc1155_address"] = msg.ErcAddress
	default:
		panic("unknown pointer type")
	}
	codeID := server.GetStoredPointerCodeID(ctx, msg.PointerType)
	moduleAcct := server.accountKeeper.GetModuleAddress(types.ModuleName)
	var err error
	var pointerAddr sdk.AccAddress
	if exists {
		bz, _ := json.Marshal(map[string]interface{}{})
		pointerAddr = existingPointer
		_, err = server.wasmKeeper.Migrate(ctx, existingPointer, moduleAcct, codeID, bz)
	} else {
		bz, jerr := json.Marshal(payload)
		if jerr != nil {
			return nil, jerr
		}
		pointerAddr, _, err = server.wasmKeeper.Instantiate(ctx, codeID, moduleAcct, moduleAcct, bz, fmt.Sprintf("Pointer of %s", msg.ErcAddress), sdk.NewCoins())
	}
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
	case types.PointerType_ERC1155:
		err = server.SetCW1155ERC1155Pointer(ctx, common.HexToAddress(msg.ErcAddress), pointerAddr.String())
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypePointerRegistered, sdk.NewAttribute(types.AttributeKeyPointerType, "erc1155"),
			sdk.NewAttribute(types.AttributeKeyPointerAddress, pointerAddr.String()), sdk.NewAttribute(types.AttributeKeyPointee, msg.ErcAddress),
			sdk.NewAttribute(types.AttributeKeyPointerVersion, fmt.Sprintf("%d", erc1155.CurrentVersion))))
	default:
		panic("unknown pointer type")
	}
	return &types.MsgRegisterPointerResponse{PointerAddress: pointerAddr.String()}, err
}

func (server msgServer) AssociateContractAddress(goCtx context.Context, msg *types.MsgAssociateContractAddress) (*types.MsgAssociateContractAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	addr := sdk.MustAccAddressFromBech32(msg.Address) // already validated
	// check if address is for a contract
	if server.wasmViewKeeper.GetContractInfo(ctx, addr) == nil {
		return nil, errors.New("no wasm contract found at the given address")
	}
	evmAddr := common.BytesToAddress(addr)
	existingEvmAddr, ok := server.GetEVMAddress(ctx, addr)
	if ok {
		if existingEvmAddr.Cmp(evmAddr) != 0 {
			ctx.Logger().Error(fmt.Sprintf("unexpected associated EVM address %s exists for contract %s: expecting %s", existingEvmAddr.Hex(), addr.String(), evmAddr.Hex()))
		}
		return nil, errors.New("contract already has an associated address")
	}
	server.SetAddressMapping(ctx, addr, evmAddr)
	return &types.MsgAssociateContractAddressResponse{}, nil
}

func (server msgServer) Associate(context.Context, *types.MsgAssociate) (*types.MsgAssociateResponse, error) {
	return &types.MsgAssociateResponse{}, nil
}
