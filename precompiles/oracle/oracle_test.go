package oracle_test

import (
	"encoding/hex"
	"testing"

	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGetExchangeRate(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())

	p, err := oracle.NewPrecompile(testApp.OracleKeeper, k)

	query, err := p.ABI.MethodById(p.GetExchangeRatesId)
	require.Nil(t, err)

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
