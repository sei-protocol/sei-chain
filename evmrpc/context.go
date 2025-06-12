package evmrpc

import (
	"context"
	"encoding/json"
	"fmt"
)

type contextKey string

const tendermintTraceKey contextKey = "tendermintTrace"

type TendermintTraces struct {
	Traces []TendermintTrace `json:"traces"`
}

func (tt *TendermintTraces) MustMarshalToJson() json.RawMessage {
	bz, _ := json.Marshal(tt)
	return bz
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

func stringifyInt64Ptr(i *int64) string {
	if i == nil {
		return ""
	}
	return fmt.Sprintf("%d", *i)
}
