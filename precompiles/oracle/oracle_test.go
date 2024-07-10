package oracle_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGetExchangeRate(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	rate := sdk.NewDec(1700)
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	testApp.OracleKeeper.SetBaseExchangeRate(ctx, utils.MicroAtomDenom, rate)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	p, err := oracle.NewPrecompile(testApp.OracleKeeper, k)
	require.Nil(t, err)

	query, err := p.ABI.MethodById(p.GetExecutor().(*oracle.PrecompileExecutor).GetExchangeRatesId)
	require.Nil(t, err)
	precompileRes, err := p.Run(&evm, common.Address{}, common.Address{}, p.GetExecutor().(*oracle.PrecompileExecutor).GetExchangeRatesId, nil, true)
	require.Nil(t, err)
	exchangeRates, err := query.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(exchangeRates))

	// TODO: Use type assertion for nested struct
	require.Equal(t, []struct {
		Denom                 string `json:"denom"`
		OracleExchangeRateVal struct {
			ExchangeRate        string `json:"exchangeRate"`
			LastUpdate          string `json:"lastUpdate"`
			LastUpdateTimestamp int64  `json:"lastUpdateTimestamp"`
		} `json:"oracleExchangeRateVal"`
	}{
		{
			Denom: "uatom",
			OracleExchangeRateVal: struct {
				ExchangeRate        string `json:"exchangeRate"`
				LastUpdate          string `json:"lastUpdate"`
				LastUpdateTimestamp int64  `json:"lastUpdateTimestamp"`
			}{
				ExchangeRate:        "1700.000000000000000000",
				LastUpdate:          "2",
				LastUpdateTimestamp: -62135596800000,
			},
		},
	}, exchangeRates[0])
}

func TestGetOracleTwaps(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockTime(time.Unix(5400, 0))

	priceSnapshots := types.PriceSnapshots{
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(10),
				LastUpdate:   sdk.NewInt(3600),
			}),
		}, 3600),
		types.NewPriceSnapshot(types.PriceSnapshotItems{
			types.NewPriceSnapshotItem(utils.MicroEthDenom, types.OracleExchangeRate{
				ExchangeRate: sdk.NewDec(20),
				LastUpdate:   sdk.NewInt(4500),
			}),
		}, 4500),
	}
	for _, snap := range priceSnapshots {
		testApp.OracleKeeper.SetPriceSnapshot(ctx, snap)
	}

	k := &testApp.EvmKeeper
	defaults := types.DefaultParams()
	testApp.OracleKeeper.SetParams(ctx, defaults)
	for _, denom := range defaults.Whitelist {
		testApp.OracleKeeper.SetVoteTarget(ctx, denom.Name)
	}

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	p, err := oracle.NewPrecompile(testApp.OracleKeeper, k)
	require.Nil(t, err)

	query, err := p.ABI.MethodById(p.GetExecutor().(*oracle.PrecompileExecutor).GetOracleTwapsId)
	require.Nil(t, err)
	args, err := query.Inputs.Pack(uint64(3600))
	require.Nil(t, err)
	precompileRes, err := p.Run(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*oracle.PrecompileExecutor).GetOracleTwapsId, args...), nil, true)
	require.Nil(t, err)
	twap, err := query.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(twap))

	require.Equal(t, []struct {
		Denom           string `json:"denom"`
		Twap            string `json:"twap"`
		LookbackSeconds int64  `json:"lookbackSeconds"`
	}{
		{
			Denom:           "ueth",
			Twap:            "15.000000000000000000",
			LookbackSeconds: 1800,
		},
	}, twap[0])
}
