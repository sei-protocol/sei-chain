package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
	"github.com/stretchr/testify/require"
)

func TestNewPriceSnapshotItem(t *testing.T) {
	item := NewPriceSnapshotItem(utils.MicroAtomDenom, OracleExchangeRate{
		ExchangeRate: sdk.NewDec(11),
		LastUpdate:   sdk.NewInt(20),
	})

	expected := PriceSnapshotItem{
		Denom: utils.MicroAtomDenom,
		OracleExchangeRate: OracleExchangeRate{
			ExchangeRate: sdk.NewDec(11),
			LastUpdate:   sdk.NewInt(20),
		},
	}

	require.Equal(t, expected, item)
}

func TestNewPriceSnapshot(t *testing.T) {
	snapshot := NewPriceSnapshot([]PriceSnapshotItem{
		NewPriceSnapshotItem(utils.MicroEthDenom, OracleExchangeRate{
			ExchangeRate: sdk.NewDec(11),
			LastUpdate:   sdk.NewInt(20),
		}),
		NewPriceSnapshotItem(utils.MicroAtomDenom, OracleExchangeRate{
			ExchangeRate: sdk.NewDec(12),
			LastUpdate:   sdk.NewInt(20),
		}),
		NewPriceSnapshotItem(utils.MicroBtcDenom, OracleExchangeRate{
			ExchangeRate: sdk.NewDec(13),
			LastUpdate:   sdk.NewInt(20),
		}),
		NewPriceSnapshotItem(utils.MicroSeiDenom, OracleExchangeRate{
			ExchangeRate: sdk.NewDec(14),
			LastUpdate:   sdk.NewInt(20),
		}),
		NewPriceSnapshotItem(utils.MicroUst2Denom, OracleExchangeRate{
			ExchangeRate: sdk.NewDec(15),
			LastUpdate:   sdk.NewInt(20),
		}),
	}, 1)

	expected := PriceSnapshot{
		SnapshotTimestamp: 1,
		PriceSnapshotItems: []PriceSnapshotItem{
			{
				Denom: utils.MicroEthDenom,
				OracleExchangeRate: OracleExchangeRate{
					ExchangeRate: sdk.NewDec(11),
					LastUpdate:   sdk.NewInt(20),
				},
			},
			{
				Denom: utils.MicroAtomDenom,
				OracleExchangeRate: OracleExchangeRate{
					ExchangeRate: sdk.NewDec(12),
					LastUpdate:   sdk.NewInt(20),
				},
			},
			{
				Denom: utils.MicroBtcDenom,
				OracleExchangeRate: OracleExchangeRate{
					ExchangeRate: sdk.NewDec(13),
					LastUpdate:   sdk.NewInt(20),
				},
			},
			{
				Denom: utils.MicroSeiDenom,
				OracleExchangeRate: OracleExchangeRate{
					ExchangeRate: sdk.NewDec(14),
					LastUpdate:   sdk.NewInt(20),
				},
			},
			{
				Denom: utils.MicroUst2Denom,
				OracleExchangeRate: OracleExchangeRate{
					ExchangeRate: sdk.NewDec(15),
					LastUpdate:   sdk.NewInt(20),
				},
			},
		},
	}

	require.Equal(t, expected, snapshot)
}
