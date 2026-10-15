package bindings

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type CallEVM struct {
	Value *sdk.Int `json:"value"`
	To    string   `json:"to"`
	Data  string   `json:"data"` // base64
}

type DelegateCallEVM struct {
	To   string `json:"to"`
	Data string `json:"data"` // base64
}
