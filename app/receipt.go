package app

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw1155"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

const ShellEVMTxType = math.MaxUint32

var ERC20ApprovalTopic = common.HexToHash("0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925")
var ERC20TransferTopic = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
var ERC721TransferTopic = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
var ERC721ApprovalTopic = common.HexToHash("0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925")
var ERC721ApproveAllTopic = common.HexToHash("0x17307eab39ab6107e8899845ad3d59bd9653f200f220920489ca2b5937696c31")
var ERC1155TransferSingleTopic = common.HexToHash("0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62")
var ERC1155TransferBatchTopic = common.HexToHash("0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb")
var ERC1155ApprovalForAllTopic = common.HexToHash("0x17307eab39ab6107e8899845ad3d59bd9653f200f220920489ca2b5937696c31")
var ERC1155URITopic = common.HexToHash("0x6bb7ff708619ba0610cba295a58592e0451dee2622938c8755667688daf3529b")
var EmptyHash = common.HexToHash("0x0")
var TrueHash = common.HexToHash("0x1")

type AllowanceResponse struct {
	Allowance sdk.Int         `json:"allowance"`
	Expires   json.RawMessage `json:"expires"`
}

func getOwnerEventKey(contractAddr string, tokenID string) string {
	return fmt.Sprintf("%s-%s", contractAddr, tokenID)
}

func (app *App) AddCosmosEventsToEVMReceiptIfApplicable(ctx sdk.Context, tx sdk.Tx, checksum [32]byte, response sdk.DeliverTxHookInput) {
	// hooks will only be called if DeliverTx is successful
	wasmEvents := GetEventsOfType(response, wasmtypes.WasmModuleEventType)
	if len(wasmEvents) == 0 {
		return
	}
	logs := []*ethtypes.Log{}
	// Note: txs with a very large number of WASM events may run out of gas due to
	// additional gas consumption from EVM receipt generation and event translation
	wasmToEvmEventGasLimit := app.EvmKeeper.GetDeliverTxHookWasmGasLimit(ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1)))
	wasmToEvmEventCtx := ctx.WithGasMeter(sdk.NewGasMeterWithMultiplier(ctx, wasmToEvmEventGasLimit))
	// unfortunately CW721 transfer events differ from ERC721 transfer events
	// in that CW721 include sender (which can be different than owner) whereas
	// ERC721 always include owner. The following logic refer to the owner
	// event emitted before the transfer and use that instead to populate the
	// synthetic ERC721 event.
	ownerEvents := GetEventsOfType(response, wasmtypes.EventTypeCW721PreTransferOwner)
	ownerEventsMap := map[string][]abci.Event{}
	for _, ownerEvent := range ownerEvents {
		if len(ownerEvent.Attributes) != 3 {
			ctx.Logger().Error("received owner event with number of attributes != 3")
			continue
		}
		ownerEventKey := getOwnerEventKey(string(ownerEvent.Attributes[0].Value), string(ownerEvent.Attributes[1].Value))
		if events, ok := ownerEventsMap[ownerEventKey]; ok {
			ownerEventsMap[ownerEventKey] = append(events, ownerEvent)
		} else {
			ownerEventsMap[ownerEventKey] = []abci.Event{ownerEvent}
		}
	}
	cw721TransferCounterMap := map[string]int{}
	for _, wasmEvent := range wasmEvents {
		contractAddr, found := GetAttributeValue(wasmEvent, wasmtypes.AttributeKeyContractAddr)
		if !found {
			continue
		}
		pointerAddr, _, exists := app.EvmKeeper.GetERC20CW20Pointer(wasmToEvmEventCtx, contractAddr)
		if exists {
			for _, log := range app.translateCW20Event(wasmToEvmEventCtx, wasmEvent, pointerAddr, contractAddr) {
				log.Index = uint(len(logs))
				logs = append(logs, log)
			}
			continue
		}
		// check if there is a ERC721 pointer to contract Addr
		pointerAddr, _, exists = app.EvmKeeper.GetERC721CW721Pointer(wasmToEvmEventCtx, contractAddr)
		if exists {
			for _, log := range app.translateCW721Event(wasmToEvmEventCtx, wasmEvent, pointerAddr, contractAddr, ownerEventsMap, cw721TransferCounterMap) {
				log.Index = uint(len(logs))
				logs = append(logs, log)
			}
			continue
		}
		// check if there is a ERC1155 pointer to contract Addr
		pointerAddr, _, exists = app.EvmKeeper.GetERC1155CW1155Pointer(wasmToEvmEventCtx, contractAddr)
		if exists {
			for _, log := range app.translateCW1155Event(wasmToEvmEventCtx, wasmEvent, pointerAddr, contractAddr) {
				log.Index = uint(len(logs))
				logs = append(logs, log)
			}
			continue
		}
	}
	if len(logs) == 0 {
		return
	}
	txHash := common.BytesToHash(checksum[:])
	if response.EvmTxInfo != nil {
		txHash = common.HexToHash(response.EvmTxInfo.TxHash)
	}
	var bloom ethtypes.Bloom
	if r, err := app.EvmKeeper.GetTransientReceipt(wasmToEvmEventCtx, txHash); err == nil && r != nil {
		r.Logs = append(r.Logs, utils.Map(logs, evmkeeper.ConvertSyntheticEthLog)...)
		for i, l := range r.Logs {
			l.Index = uint32(i)
		}
		bloom = ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: evmkeeper.GetLogsForTx(r)}})
		r.LogsBloom = bloom[:]
		_ = app.EvmKeeper.SetTransientReceipt(wasmToEvmEventCtx, txHash, r)
	} else {
		bloom = ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: logs}})
		receipt := &evmtypes.Receipt{
			TxType:           ShellEVMTxType,
			TxHashHex:        txHash.Hex(),
			GasUsed:          ctx.GasMeter().GasConsumed(),
			BlockNumber:      uint64(ctx.BlockHeight()),
			TransactionIndex: uint32(ctx.TxIndex()),
			Logs:             utils.Map(logs, evmkeeper.ConvertSyntheticEthLog),
			LogsBloom:        bloom[:],
			Status:           uint32(ethtypes.ReceiptStatusSuccessful), // we don't create shell receipt for failed Cosmos tx since there is no event anyway
		}
		sigTx, ok := tx.(authsigning.SigVerifiableTx)
		if ok && len(sigTx.GetSigners()) > 0 {
			// use the first signer as the `from`
			receipt.From = app.EvmKeeper.GetEVMAddressOrDefault(wasmToEvmEventCtx, sigTx.GetSigners()[0]).Hex()
		}
		_ = app.EvmKeeper.SetTransientReceipt(wasmToEvmEventCtx, txHash, receipt)
	}
	if d, found := app.EvmKeeper.GetEVMTxDeferredInfo(ctx); found {
		app.EvmKeeper.AppendToEvmTxDeferredInfo(wasmToEvmEventCtx, bloom, txHash, d.Surplus)
	} else {
		app.EvmKeeper.AppendToEvmTxDeferredInfo(wasmToEvmEventCtx, bloom, txHash, sdk.ZeroInt())
	}
}

func (app *App) translateCW20Event(ctx sdk.Context, wasmEvent abci.Event, pointerAddr common.Address, contractAddr string) (res []*ethtypes.Log) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[Error] Panic caught during translateCW20Event: type=%T, value=%+v\n", r, r)
		}
	}()

	for _, action := range app.GetActionsFromWasmEvent(ctx, wasmEvent) {
		switch action.Type {
		case "mint", "burn", "send", "transfer", "transfer_from", "send_from", "burn_from":
			if action.Amount == nil {
				continue
			}
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC20TransferTopic,
					action.From,
					action.To,
				},
				Data: common.BigToHash(action.Amount).Bytes(),
			})
		case "increase_allowance", "decrease_allowance":
			topics := []common.Hash{
				ERC20ApprovalTopic,
				action.Owner,
				action.Spender,
			}
			ret, err := app.WasmKeeper.QuerySmart(
				ctx,
				sdk.MustAccAddressFromBech32(contractAddr),
				[]byte(fmt.Sprintf(
					"{\"allowance\":{\"owner\":\"%s\",\"spender\":\"%s\"}}",
					app.EvmKeeper.GetSeiAddressOrDefault(ctx, common.BytesToAddress(action.Owner[:])).String(),
					app.EvmKeeper.GetSeiAddressOrDefault(ctx, common.BytesToAddress(action.Spender[:])).String())),
			)
			if err != nil {
				continue
			}
			allowanceResponse := &AllowanceResponse{}
			if err := json.Unmarshal(ret, allowanceResponse); err != nil {
				continue
			}
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics:  topics,
				Data:    common.BigToHash(allowanceResponse.Allowance.BigInt()).Bytes(),
			})
		}
	}
	return
}

func (app *App) translateCW721Event(ctx sdk.Context, wasmEvent abci.Event, pointerAddr common.Address, contractAddr string,
	ownerEventsMap map[string][]abci.Event, cw721TransferCounterMap map[string]int) (res []*ethtypes.Log) {
	for _, action := range app.GetActionsFromWasmEvent(ctx, wasmEvent) {
		switch action.Type {
		case "transfer_nft", "send_nft", "burn":
			if action.TokenId == nil {
				continue
			}
			sender := action.Sender
			ownerEventKey := getOwnerEventKey(contractAddr, action.TokenId.String())
			var currentCounter int
			if c, ok := cw721TransferCounterMap[ownerEventKey]; ok {
				currentCounter = c
			}
			cw721TransferCounterMap[ownerEventKey] = currentCounter + 1
			if ownerEvents, ok := ownerEventsMap[ownerEventKey]; ok {
				if len(ownerEvents) > currentCounter {
					ownerSeiAddrStr := string(ownerEvents[currentCounter].Attributes[2].Value)
					if ownerSeiAddr, err := sdk.AccAddressFromBech32(ownerSeiAddrStr); err == nil {
						ownerEvmAddr := app.EvmKeeper.GetEVMAddressOrDefault(ctx, ownerSeiAddr)
						sender = common.BytesToHash(ownerEvmAddr[:])
					} else {
						ctx.Logger().Error("Translate CW721 error: invalid bech32 owner", "error", err, "address", ownerSeiAddrStr)
					}
				} else {
					ctx.Logger().Error("Translate CW721 error: insufficient owner events", "key", ownerEventKey, "counter", currentCounter, "events", len(ownerEvents))
				}
			} else {
				ctx.Logger().Error("Translate CW721 error: owner event not found", "key", ownerEventKey)
			}
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC721TransferTopic,
					sender,
					action.Recipient,
					common.BigToHash(action.TokenId),
				},
				Data: EmptyHash.Bytes(),
			})
		case "mint":
			if action.TokenId == nil {
				continue
			}
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC721TransferTopic,
					EmptyHash,
					action.Owner,
					common.BigToHash(action.TokenId),
				},
				Data: EmptyHash.Bytes(),
			})
		case "approve":
			if action.TokenId == nil {
				continue
			}
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC721ApprovalTopic,
					action.Sender,
					action.Spender,
					common.BigToHash(action.TokenId),
				},
				Data: EmptyHash.Bytes(),
			})
		case "revoke":
			if action.TokenId == nil {
				continue
			}
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC721ApprovalTopic,
					action.Sender,
					EmptyHash,
					common.BigToHash(action.TokenId),
				},
				Data: EmptyHash.Bytes(),
			})
		case "approve_all":
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC721ApproveAllTopic,
					action.Sender,
					action.Operator,
				},
				Data: TrueHash.Bytes(),
			})
		case "revoke_all":
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC721ApproveAllTopic,
					action.Sender,
					action.Operator,
				},
				Data: EmptyHash.Bytes(),
			})
		}
	}
	return
}

func (app *App) translateCW1155Event(ctx sdk.Context, wasmEvent abci.Event, pointerAddr common.Address, contractAddr string) (res []*ethtypes.Log) {
	for _, action := range app.GetActionsFromWasmEvent(ctx, wasmEvent) {
		switch action.Type {
		case "transfer_single", "mint_single", "burn_single":
			fromHash := EmptyHash
			toHash := EmptyHash
			if action.Type != "mint_single" {
				fromHash = action.Owner
			}
			if action.Type != "burn_single" {
				toHash = action.Recipient
			}
			if action.TokenId == nil {
				continue
			}
			if action.Amount == nil {
				continue
			}
			dataHash1 := common.BigToHash(action.TokenId).Bytes()
			dataHash2 := common.BigToHash(action.Amount).Bytes()
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC1155TransferSingleTopic,
					action.Sender,
					fromHash,
					toHash,
				},
				Data: append(dataHash1, dataHash2...),
			})
		case "transfer_batch", "mint_batch", "burn_batch":
			fromHash := EmptyHash
			toHash := EmptyHash
			if action.Type != "mint_batch" {
				fromHash = action.Owner
			}
			if action.Type != "burn_batch" {
				toHash = action.Recipient
			}
			if len(action.TokenIds) == 0 {
				continue
			}
			if len(action.Amounts) == 0 {
				continue
			}
			dataArgs := cw1155.GetParsedABI().Events["TransferBatch"].Inputs.NonIndexed()
			value, err := dataArgs.Pack(action.TokenIds, action.Amounts)
			if err != nil {
				ctx.Logger().Error(fmt.Sprintf("failed to parse TransferBatch event data due to %s", err))
				continue
			}
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC1155TransferBatchTopic,
					action.Sender,
					fromHash,
					toHash,
				},
				Data: value,
			})
		case "approve_all":
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC1155ApprovalForAllTopic,
					action.Sender,
					action.Operator,
				},
				Data: TrueHash.Bytes(),
			})
		case "revoke_all":
			res = append(res, &ethtypes.Log{
				Address: pointerAddr,
				Topics: []common.Hash{
					ERC1155ApprovalForAllTopic,
					action.Sender,
					action.Operator,
				},
				Data: EmptyHash.Bytes(),
			})
		}
	}
	return
}

func (app *App) GetEvmAddressHash(ctx sdk.Context, addrStr string) common.Hash {
	seiAddr, err := sdk.AccAddressFromBech32(addrStr)
	if err == nil {
		evmAddr := app.EvmKeeper.GetEVMAddressOrDefault(ctx, seiAddr)
		evmAddrHash := common.BytesToHash(evmAddr[:])
		return evmAddrHash
	}
	return EmptyHash
}

func GetEventsOfType(rdtx sdk.DeliverTxHookInput, ty string) (res []abci.Event) {
	for _, event := range rdtx.Events {
		if event.Type == ty {
			res = append(res, event)
		}
	}
	return
}

func GetAttributeValue(event abci.Event, attribute string) (string, bool) {
	for _, attr := range event.Attributes {
		if string(attr.Key) == attribute {
			return string(attr.Value), true
		}
	}
	return "", false
}

func (app *App) GetActionsFromWasmEvent(ctx sdk.Context, event abci.Event) (actions []*Action) {
	for _, attr := range event.Attributes {
		key := string(attr.Key)
		value := string(attr.Value)
		if key == "action" {
			actions = append(actions, &Action{Type: value})
			continue
		}
		if len(actions) == 0 {
			continue
		}
		curAction := actions[len(actions)-1]
		switch key {
		case "amount":
			curAction.Amount = safeBigIntFromString(value)
		case "amounts":
			curAction.Amounts = utils.Map(strings.Split(value, ","), safeBigIntFromString)
		case "token_id":
			curAction.TokenId = safeBigIntFromString(value)
		case "token_ids":
			curAction.TokenIds = utils.Map(strings.Split(value, ","), safeBigIntFromString)
		case "sender":
			curAction.Sender = app.GetEvmAddressHash(ctx, value)
		case "recipient":
			curAction.Recipient = app.GetEvmAddressHash(ctx, value)
		case "spender":
			curAction.Spender = app.GetEvmAddressHash(ctx, value)
		case "operator":
			curAction.Operator = app.GetEvmAddressHash(ctx, value)
		case "owner":
			curAction.Owner = app.GetEvmAddressHash(ctx, value)
		case "from":
			curAction.From = app.GetEvmAddressHash(ctx, value)
		case "to":
			curAction.To = app.GetEvmAddressHash(ctx, value)
		}
	}
	return
}

func safeBigIntFromString(s string) *big.Int {
	sdkInt, ok := sdk.NewIntFromString(s)
	if !ok {
		return nil
	}
	return sdkInt.BigInt()
}

type Action struct {
	Type      string
	Amount    *big.Int
	Amounts   []*big.Int
	TokenId   *big.Int
	TokenIds  []*big.Int
	Sender    common.Hash
	Recipient common.Hash
	Spender   common.Hash
	Operator  common.Hash
	Owner     common.Hash
	From      common.Hash
	To        common.Hash
}
