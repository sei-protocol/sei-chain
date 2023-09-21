package types

import (
	context "context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

type ContextKeyType string

const ContextEthCfgKey = ContextKeyType("eth_cfg")
const ContextEthTxKey = ContextKeyType("eth_tx")
const ContextTxDataKey = ContextKeyType("tx_data")
const ContextEVMAddressKey = ContextKeyType("evm_addr")
const ContextSeiAddressKey = ContextKeyType("sei_addr")

func SetContextEtCfg(ctx sdk.Context, cfg *params.ChainConfig) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), ContextEthCfgKey, cfg))
}

func SetContextEthTx(ctx sdk.Context, tx *ethtypes.Transaction) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), ContextEthTxKey, tx))
}

func SetContextTxData(ctx sdk.Context, txdata ethtx.TxData) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), ContextTxDataKey, txdata))
}

func SetContextEVMAddress(ctx sdk.Context, addr common.Address) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), ContextEVMAddressKey, addr))
}

func SetContextSeiAddress(ctx sdk.Context, addr sdk.AccAddress) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), ContextSeiAddressKey, addr))
}

func GetContextEthCfg(ctx sdk.Context) (*params.ChainConfig, bool) {
	if ctx.Context() == nil {
		return nil, false
	}
	cfg := ctx.Context().Value(ContextEthCfgKey)
	if cfg == nil {
		return nil, false
	}
	return cfg.(*params.ChainConfig), true
}

func GetContextEthTx(ctx sdk.Context) (*ethtypes.Transaction, bool) {
	if ctx.Context() == nil {
		return nil, false
	}
	tx := ctx.Context().Value(ContextEthTxKey)
	if tx == nil {
		return nil, false
	}
	return tx.(*ethtypes.Transaction), true
}

func GetContextTxData(ctx sdk.Context) (ethtx.TxData, bool) {
	if ctx.Context() == nil {
		return nil, false
	}
	tx := ctx.Context().Value(ContextTxDataKey)
	if tx == nil {
		return nil, false
	}
	return tx.(ethtx.TxData), true
}

func GetContextEVMAddress(ctx sdk.Context) (common.Address, bool) {
	if ctx.Context() == nil {
		return [common.AddressLength]byte{}, false
	}
	addr := ctx.Context().Value(ContextEVMAddressKey)
	if addr == nil {
		return [common.AddressLength]byte{}, false
	}
	return addr.(common.Address), true
}

func GetContextSeiAddress(ctx sdk.Context) (sdk.AccAddress, bool) {
	if ctx.Context() == nil {
		return nil, false
	}
	addr := ctx.Context().Value(ContextSeiAddressKey)
	if addr == nil {
		return nil, false
	}
	return addr.(sdk.AccAddress), true
}
