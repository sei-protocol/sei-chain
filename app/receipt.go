package app

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/utils"
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
	ownerReplacements := map[uint]common.Hash{}
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
			log, eligible := app.translateCW20Event(wasmToEvmEventCtx, wasmEvent, pointerAddr, contractAddr)
			if eligible {
				log.Index = uint(len(logs))
				logs = append(logs, log)
			}
			continue
		}
		// check if there is a ERC721 pointer to contract Addr
		pointerAddr, _, exists = app.EvmKeeper.GetERC721CW721Pointer(wasmToEvmEventCtx, contractAddr)
		if exists {
			log, realOwner, eligible := app.translateCW721Event(wasmToEvmEventCtx, wasmEvent, pointerAddr,
				contractAddr, ownerEventsMap, cw721TransferCounterMap)
			if eligible {
				log.Index = uint(len(logs))
				logs = append(logs, log)
				if (realOwner != common.Hash{}) {
					ownerReplacements[log.Index] = realOwner
				}
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
		existingLogCnt := len(r.Logs)
		r.Logs = append(r.Logs, utils.Map(logs, evmkeeper.ConvertSyntheticEthLog)...)
		for i, l := range r.Logs {
			l.Index = uint32(i)
		}
		bloom = ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: evmkeeper.GetLogsForTx(r)}})
		r.LogsBloom = bloom[:]
		for i, o := range ownerReplacements {
			r.Logs[existingLogCnt+int(i)].Topics[1] = o.Hex()
		}
		_ = app.EvmKeeper.SetTransientReceipt(wasmToEvmEventCtx, txHash, r)
	} else {
		bloom = ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: logs}})
		for i, o := range ownerReplacements {
			logs[int(i)].Topics[1] = o
		}
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

func (app *App) translateCW20Event(ctx sdk.Context, wasmEvent abci.Event, pointerAddr common.Address, contractAddr string) (*ethtypes.Log, bool) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[Error] Panic caught during translateCW20Event: type=%T, value=%+v\n", r, r)
		}
	}()

	action, found := GetAttributeValue(wasmEvent, "action")
	if !found {
		return nil, false
	}
	var topics []common.Hash
	switch action {
	case "mint", "burn", "send", "transfer", "transfer_from", "send_from", "burn_from":
		topics = []common.Hash{
			ERC20TransferTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "from"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "to"),
		}
		amount, found := GetAmountAttribute(wasmEvent)
		if !found {
			return nil, false
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    common.BigToHash(amount).Bytes(),
		}, true
	case "increase_allowance", "decrease_allowance":
		ownerStr, found := GetAttributeValue(wasmEvent, "owner")
		if !found {
			return nil, false
		}
		spenderStr, found := GetAttributeValue(wasmEvent, "spender")
		if !found {
			return nil, false
		}
		topics := []common.Hash{
			ERC20ApprovalTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "owner"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "spender"),
		}
		res, err := app.WasmKeeper.QuerySmart(
			ctx,
			sdk.MustAccAddressFromBech32(contractAddr),
			[]byte(fmt.Sprintf("{\"allowance\":{\"owner\":\"%s\",\"spender\":\"%s\"}}", ownerStr, spenderStr)),
		)
		if err != nil {
			return nil, false
		}
		allowanceResponse := &AllowanceResponse{}
		if err := json.Unmarshal(res, allowanceResponse); err != nil {
			return nil, false
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    common.BigToHash(allowanceResponse.Allowance.BigInt()).Bytes(),
		}, true
	}
	return nil, false
}

func (app *App) translateCW721Event(ctx sdk.Context, wasmEvent abci.Event, pointerAddr common.Address, contractAddr string,
	ownerEventsMap map[string][]abci.Event, cw721TransferCounterMap map[string]int) (*ethtypes.Log, common.Hash, bool) {
	action, found := GetAttributeValue(wasmEvent, "action")
	if !found {
		return nil, common.Hash{}, false
	}
	var topics []common.Hash
	switch action {
	case "transfer_nft", "send_nft", "burn":
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, common.Hash{}, false
		} else {
			ctx.Logger().Error("Translate CW721 error: null token ID")
		}
		sender := common.Hash{}
		ownerEventKey := getOwnerEventKey(contractAddr, tokenID.String())
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
		topics = []common.Hash{
			ERC721TransferTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "recipient"),
			common.BigToHash(tokenID),
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    EmptyHash.Bytes(),
		}, sender, true
	case "mint":
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, common.Hash{}, false
		}
		topics = []common.Hash{
			ERC721TransferTopic,
			EmptyHash,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "owner"),
			common.BigToHash(tokenID),
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    EmptyHash.Bytes(),
		}, common.Hash{}, true
	case "approve":
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, common.Hash{}, false
		}
		topics = []common.Hash{
			ERC721ApprovalTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "spender"),
			common.BigToHash(tokenID),
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    EmptyHash.Bytes(),
		}, common.Hash{}, true
	case "revoke":
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, common.Hash{}, false
		}
		topics = []common.Hash{
			ERC721ApprovalTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			EmptyHash,
			common.BigToHash(tokenID),
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    EmptyHash.Bytes(),
		}, common.Hash{}, true
	case "approve_all":
		topics = []common.Hash{
			ERC721ApproveAllTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "operator"),
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    TrueHash.Bytes(),
		}, common.Hash{}, true
	case "revoke_all":
		topics = []common.Hash{
			ERC721ApproveAllTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "operator"),
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    EmptyHash.Bytes(),
		}, common.Hash{}, true
	}
	return nil, common.Hash{}, false
}

func (app *App) GetEvmAddressAttribute(ctx sdk.Context, event abci.Event, attribute string) common.Hash {
	addrStr, found := GetAttributeValue(event, attribute)
	if found {
		seiAddr, err := sdk.AccAddressFromBech32(addrStr)
		if err == nil {
			evmAddr := app.EvmKeeper.GetEVMAddressOrDefault(ctx, seiAddr)
			return common.BytesToHash(evmAddr[:])
		}
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

func GetAmountAttribute(event abci.Event) (*big.Int, bool) {
	amount, found := GetAttributeValue(event, "amount")
	if found {
		amountInt, ok := sdk.NewIntFromString(amount)
		if ok {
			return amountInt.BigInt(), true
		}
	}
	return nil, false
}

func GetTokenIDAttribute(event abci.Event) *big.Int {
	tokenID, found := GetAttributeValue(event, "token_id")
	if !found {
		return nil
	}
	tokenIDInt, ok := sdk.NewIntFromString(tokenID)
	if !ok {
		return nil
	}
	return tokenIDInt.BigInt()
}
