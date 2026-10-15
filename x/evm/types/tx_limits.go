package types

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// MaxInitCodeSize returns the initcode size limit at blockTime (EIP-3860, raised by Sei's EIP-7954).
func MaxInitCodeSize(blockTime int64) int {
	if IsSeiAmsterdam(blockTime) {
		return params.MaxInitCodeSizeSeiAmsterdam
	}
	return params.MaxInitCodeSize
}

// ValidateTxLimits checks etx's initcode size and, under Osaka, its EIP-7825 gas cap.
func ValidateTxLimits(etx *ethtypes.Transaction, blockTime int64) error {
	if limit := MaxInitCodeSize(blockTime); etx.To() == nil && len(etx.Data()) > limit {
		return fmt.Errorf("%w: code size %v, limit %v", vm.ErrMaxInitCodeSizeExceeded, len(etx.Data()), limit)
	}
	if IsOsaka(blockTime) && etx.Gas() > params.MaxTxGas {
		return fmt.Errorf("%w (cap: %d, tx: %d)", core.ErrGasLimitTooHigh, params.MaxTxGas, etx.Gas())
	}
	return nil
}
