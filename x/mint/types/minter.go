package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewMinter returns a new Minter object with the given inflation and annual
// provisions values.
func NewMinter(epochProvisions sdk.Dec) Minter {
	return Minter{
		EpochProvisions: epochProvisions,
	}
}

// InitialMinter returns an initial Minter object with a given inflation value.
func InitialMinter() Minter {
	return NewMinter(
		sdk.NewDec(0),
	)
}

// DefaultInitialMinter returns a default initial Minter object for a new chain
// which uses an inflation rate of 13%.
func DefaultInitialMinter() Minter {
	return InitialMinter()
}

// validate minter
func ValidateMinter(minter Minter) error {
	return nil
}

func (m Minter) NextEpochProvisions(params Params) sdk.Dec {
	return m.EpochProvisions.Mul(params.ReductionFactor)
}

// EpochProvision returns the provisions for a block based on the epoch
// provisions rate.
func (m Minter) EpochProvision(params Params) sdk.Coin {
	provisionAmt := m.EpochProvisions
	return sdk.NewCoin(params.MintDenom, provisionAmt.TruncateInt())
}
