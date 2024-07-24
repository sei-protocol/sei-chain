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

func (app *App) AddCosmosEventsToEVMReceiptIfApplicable(ctx sdk.Context, tx sdk.Tx, checksum [32]byte, response abci.ResponseDeliverTx) {
	if response.Code > 0 {
		return
	}
	wasmEvents := GetEventsOfType(response, wasmtypes.WasmModuleEventType)
	logs := []*ethtypes.Log{}
	for _, wasmEvent := range wasmEvents {
		contractAddr, found := GetAttributeValue(wasmEvent, wasmtypes.AttributeKeyContractAddr)
		if !found {
			continue
		}
		// check if there is a ERC20 pointer to contractAddr
		pointerAddr, _, exists := app.EvmKeeper.GetERC20CW20Pointer(ctx, contractAddr)
		if exists {
			log, eligible := app.translateCW20Event(ctx, wasmEvent, pointerAddr, contractAddr)
			if eligible {
				log.Index = uint(len(logs))
				logs = append(logs, log)
			}
			continue
		}
		// check if there is a ERC721 pointer to contract Addr
		pointerAddr, _, exists = app.EvmKeeper.GetERC721CW721Pointer(ctx, contractAddr)
		if exists {
			log, eligible := app.translateCW721Event(ctx, wasmEvent, pointerAddr, contractAddr)
			if eligible {
				log.Index = uint(len(logs))
				logs = append(logs, log)
			}
			continue
		}
		// check if there is a ERC1155 pointer to contract Addr
		pointerAddr, _, exists = app.EvmKeeper.GetERC1155CW1155Pointer(ctx, contractAddr)
		if exists {
			log, eligible := app.translateCW1155Event(ctx, wasmEvent, pointerAddr, contractAddr)
			if eligible {
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
	if r, err := app.EvmKeeper.GetTransientReceipt(ctx, txHash); err == nil && r != nil {
		r.Logs = append(r.Logs, utils.Map(logs, evmkeeper.ConvertSyntheticEthLog)...)
		bloom = ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: evmkeeper.GetLogsForTx(r)}})
		r.LogsBloom = bloom[:]
		_ = app.EvmKeeper.SetTransientReceipt(ctx, txHash, r)
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
			receipt.From = app.EvmKeeper.GetEVMAddressOrDefault(ctx, sigTx.GetSigners()[0]).Hex()
		}
		_ = app.EvmKeeper.SetTransientReceipt(ctx, txHash, receipt)
	}
	if d, found := app.EvmKeeper.GetEVMTxDeferredInfo(ctx); found {
		app.EvmKeeper.AppendToEvmTxDeferredInfo(ctx, bloom, txHash, d.Surplus)
	} else {
		app.EvmKeeper.AppendToEvmTxDeferredInfo(ctx, bloom, txHash, sdk.ZeroInt())
	}
}

func (app *App) translateCW20Event(ctx sdk.Context, wasmEvent abci.Event, pointerAddr common.Address, contractAddr string) (*ethtypes.Log, bool) {
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

func (app *App) translateCW721Event(ctx sdk.Context, wasmEvent abci.Event, pointerAddr common.Address, contractAddr string) (*ethtypes.Log, bool) {
	action, found := GetAttributeValue(wasmEvent, "action")
	if !found {
		return nil, false
	}
	var topics []common.Hash
	switch action {
	case "transfer_nft", "send_nft":
		topics = []common.Hash{
			ERC721TransferTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "recipient"),
		}
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, false
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    common.BigToHash(tokenID).Bytes(),
		}, true
	case "burn":
		topics = []common.Hash{
			ERC721TransferTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			EmptyHash,
		}
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, false
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    common.BigToHash(tokenID).Bytes(),
		}, true
	case "mint":
		topics = []common.Hash{
			ERC721TransferTopic,
			EmptyHash,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "owner"),
		}
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, false
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    common.BigToHash(tokenID).Bytes(),
		}, true
	case "approve":
		topics = []common.Hash{
			ERC721ApprovalTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "spender"),
		}
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, false
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    common.BigToHash(tokenID).Bytes(),
		}, true
	case "revoke":
		topics = []common.Hash{
			ERC721ApprovalTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			EmptyHash,
		}
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, false
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    common.BigToHash(tokenID).Bytes(),
		}, true
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
		}, true
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
		}, true
	}
	return nil, false
}

func (app *App) translateCW1155Event(ctx sdk.Context, wasmEvent abci.Event, pointerAddr common.Address, contractAddr string) (*ethtypes.Log, bool) {
	action, found := GetAttributeValue(wasmEvent, "action")
	if !found {
		return nil, false
	}
	var topics []common.Hash
	switch action {
	case "transfer_single", "mint_single", "burn_single":
		fromHash := EmptyHash
		toHash := EmptyHash
		if action != "mint_single" {
			fromHash = app.GetEvmAddressAttribute(ctx, wasmEvent, "owner")
		}
		if action != "burn_single" {
			toHash = app.GetEvmAddressAttribute(ctx, wasmEvent, "recipient")
		}
		topics = []common.Hash{
			ERC1155TransferSingleTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			fromHash,
			toHash,
		}
		tokenID := GetTokenIDAttribute(wasmEvent)
		if tokenID == nil {
			return nil, false
		}
		tokenAmount, found := GetAmountAttribute(wasmEvent)
		if !found {
			return nil, false
		}
		dataHash1 := common.BigToHash(tokenID).Bytes()
		dataHash2 := common.BigToHash(tokenAmount).Bytes()
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    append(dataHash1, dataHash2...),
		}, true
	case "transfer_batch", "mint_batch", "burn_batch":
		fromHash := EmptyHash
		toHash := EmptyHash
		if action != "mint_single" {
			fromHash = app.GetEvmAddressAttribute(ctx, wasmEvent, "owner")
		}
		if action != "burn_single" {
			toHash = app.GetEvmAddressAttribute(ctx, wasmEvent, "recipient")
		}
		topics = []common.Hash{
			ERC1155TransferSingleTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			fromHash,
			toHash,
		}
		tokenIDs, found := GetTokenIDsAttribute(wasmEvent)
		if !found {
			return nil, false
		}
		tokenAmounts, found := GetAmountsAttribute(wasmEvent)
		if !found {
			return nil, false
		}
		value := EncodeBigIntArray(tokenIDs)
		value = append(value, EncodeBigIntArray(tokenAmounts)...)
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    value,
		}, true
	case "approve_all", "revoke_all":
		topics = []common.Hash{
			ERC1155ApprovalForAllTopic,
			app.GetEvmAddressAttribute(ctx, wasmEvent, "sender"),
			app.GetEvmAddressAttribute(ctx, wasmEvent, "operator"),
		}
		var dataHash []byte
		if action == "approve_all" {
			dataHash = TrueHash.Bytes()
		} else {
			dataHash = EmptyHash.Bytes()
		}
		return &ethtypes.Log{
			Address: pointerAddr,
			Topics:  topics,
			Data:    dataHash,
		}, true
	}
	return nil, false
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

func GetEventsOfType(rdtx abci.ResponseDeliverTx, ty string) (res []abci.Event) {
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

func GetAmountsAttribute(event abci.Event) ([]*big.Int, bool) {
	var results []*big.Int
	amounts, found := GetAttributeValue(event, "amounts")
	if !found {
		return results, false
	}
	for _, amt := range strings.Split(amounts, ",") {
		amtInt, ok := sdk.NewIntFromString(amt)
		if !ok {
			return results, false
		}
		results = append(results, amtInt.BigInt())
	}
	return results, true
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

func GetTokenIDsAttribute(event abci.Event) ([]*big.Int, bool) {
	var results []*big.Int
	tokenIDs, found := GetAttributeValue(event, "token_ids")
	if !found {
		return results, false
	}
	for _, tid := range strings.Split(tokenIDs, ",") {
		tidInt, ok := sdk.NewIntFromString(tid)
		if !ok {
			return results, false
		}
		results = append(results, tidInt.BigInt())
	}
	return results, true
}

func EncodeBigIntArray(inputs []*big.Int) []byte {
	// Arrays are broken up into components:
	// - offset byte (always 32)
	// - length of array
	// - ...array values
	offset := big.NewInt(32)
	length := big.NewInt(int64(len(inputs)))
	value := append(
		common.BigToHash(offset).Bytes(),
		common.BigToHash(length).Bytes()...,
	)
	for _, i := range inputs {
		value = append(value, common.BigToHash(i).Bytes()...)
	}
	return value
}
