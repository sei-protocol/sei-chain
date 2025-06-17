package evmrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type contextKey string

const tendermintTraceKey contextKey = "tendermintTrace"
const receiptTraceKey contextKey = "receiptTrace"

type TendermintTraces struct {
	Traces []TendermintTrace `json:"traces"`
}

func (tt *TendermintTraces) MustMarshalToJson() json.RawMessage {
	bz, _ := json.Marshal(tt)
	return bz
}

type ReceiptTraces struct {
	Traces []RawResponseReceipt `json:"traces"`
}

func (rt *ReceiptTraces) MustMarshalToJson() json.RawMessage {
	bz, _ := json.Marshal(rt)
	return bz
}

type RawResponseReceipt struct {
	BlockNumber       hexutil.Uint64  `json:"blockNumber"`
	ContractAddress   *common.Address `json:"contractAddress"`
	CumulativeGasUsed hexutil.Uint64  `json:"cumulativeGasUsed"`
	EffectiveGasPrice *hexutil.Big    `json:"effectiveGasPrice"`
	From              common.Address  `json:"from"`
	To                *common.Address `json:"to"`
	GasUsed           hexutil.Uint64  `json:"gasUsed"`
	Status            hexutil.Uint    `json:"status"`
	Type              hexutil.Uint    `json:"type"`
	TransactionHash   common.Hash     `json:"transactionHash"`
	TransactionIndex  hexutil.Uint64  `json:"transactionIndex"`
}

type TendermintTrace struct {
	Endpoint  string          `json:"endpoint"`
	Arguments []string        `json:"arguments"`
	Response  json.RawMessage `json:"response"`
}

func WithTendermintTraces(ctx context.Context, traces *TendermintTraces) context.Context {
	return context.WithValue(ctx, tendermintTraceKey, traces)
}

func TraceTendermintIfApplicable(ctx context.Context, endpoint string, arguments []string, response interface{}) {
	encodedResponse, err := json.Marshal(response)
	if err != nil {
		panic(err)
	}
	trace := TendermintTrace{
		Endpoint:  endpoint,
		Arguments: arguments,
		Response:  encodedResponse,
	}
	existing := ctx.Value(tendermintTraceKey)
	if existing == nil {
		return
	}
	typed := existing.(*TendermintTraces)
	typed.Traces = append(typed.Traces, trace)
}

func TendermintTracesFromContext(ctx context.Context) *TendermintTraces {
	v := ctx.Value(tendermintTraceKey)
	if v == nil {
		return nil
	}
	return v.(*TendermintTraces)
}

func WithReceiptTraces(ctx context.Context, traces *ReceiptTraces) context.Context {
	return context.WithValue(ctx, receiptTraceKey, traces)
}

func TraceReceiptIfApplicable(ctx context.Context, receipt *types.Receipt) {
	rrr := &RawResponseReceipt{
		BlockNumber:       hexutil.Uint64(receipt.BlockNumber),
		CumulativeGasUsed: hexutil.Uint64(receipt.CumulativeGasUsed),
		EffectiveGasPrice: (*hexutil.Big)(new(big.Int).SetUint64(receipt.EffectiveGasPrice)),
		From:              common.HexToAddress(receipt.From),
		GasUsed:           hexutil.Uint64(receipt.GasUsed),
		Status:            hexutil.Uint(receipt.Status),
		Type:              hexutil.Uint(receipt.TxType),
		TransactionHash:   common.HexToHash(receipt.TxHashHex),
		TransactionIndex:  hexutil.Uint64(receipt.TransactionIndex),
	}
	if receipt.ContractAddress != "" {
		ca := common.HexToAddress(receipt.ContractAddress)
		rrr.ContractAddress = &ca
	}
	if receipt.To != "" {
		to := common.HexToAddress(receipt.To)
		rrr.To = &to
	}
	existing := ctx.Value(receiptTraceKey)
	if existing == nil {
		return
	}
	typed := existing.(*ReceiptTraces)
	typed.Traces = append(typed.Traces, *rrr)
}

func stringifyInt64Ptr(i *int64) string {
	if i == nil {
		return ""
	}
	return fmt.Sprintf("%d", *i)
}
