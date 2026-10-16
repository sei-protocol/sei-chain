package types

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// ValidateTxLimits checks etx's initcode size and, under Osaka, its EIP-7825 gas cap.
func ValidateTxLimits(etx *ethtypes.Transaction, rules *params.Rules) error {
	if etx.To() == nil {
		if err := vm.CheckMaxInitCodeSize(rules, uint64(len(etx.Data()))); err != nil {
			return err
		}
	}
	if rules.IsOsaka && etx.Gas() > params.MaxTxGas {
		return fmt.Errorf("%w (cap: %d, tx: %d)", core.ErrGasLimitTooHigh, params.MaxTxGas, etx.Gas())
	}
	return nil
}
