package oracle_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
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

	query, err := p.ABI.MethodById(p.GetExchangeRatesId)
	require.Nil(t, err)
	precompileRes, err := p.Run(&evm, common.Address{}, p.GetExchangeRatesId, nil)
	require.Nil(t, err)
	exchangeRates, err := query.Outputs.Unpack(precompileRes)
	require.Nil(t, err)
	require.Equal(t, 1, len(exchangeRates))
	fmt.Printf("exchangeRates final %+v\n", exchangeRates[0])
	require.Equal(t, oracle.DenomOracleExchangeRatePair{Denom: "usei", OracleExchangeRateVal: oracle.OracleExchangeRate{ExchangeRate: "1700.000000000000000000", LastUpdate: "2", LastUpdateTimestamp: -62135596800000}}, exchangeRates[0].([]oracle.DenomOracleExchangeRatePair))
}

func TestGetOracleTwaps(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())

	p, err := oracle.NewPrecompile(testApp.OracleKeeper, k)

	query, err := p.ABI.MethodById(p.GetOracleTwapsId)
	require.Nil(t, err)

}
