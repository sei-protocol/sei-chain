package types

import (
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// ValidateTxLimits checks etx's initcode size and, under Osaka, its EIP-7825 gas cap. It returns
// ErrTxTooLarge or ErrOutOfGas wrapping the go-ethereum reason.
func ValidateTxLimits(etx *ethtypes.Transaction, rules *params.Rules) error {
	if etx.To() == nil {
		if err := vm.CheckMaxInitCodeSize(rules, uint64(len(etx.Data()))); err != nil {
			return sdkerrors.Wrap(sdkerrors.ErrTxTooLarge, err.Error())
		}
	}
	if rules.IsOsaka && etx.Gas() > params.MaxTxGas {
		return sdkerrors.Wrapf(sdkerrors.ErrOutOfGas, "%s (cap: %d, tx: %d)", core.ErrGasLimitTooHigh, params.MaxTxGas, etx.Gas())
	}
	return nil
}
