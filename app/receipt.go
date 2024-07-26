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
		for i, l := range r.Logs {
			l.Index = uint32(i)
		}
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
	case "transfer_nft", "send_nft", "burn":
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
