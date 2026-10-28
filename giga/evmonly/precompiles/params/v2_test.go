package params_test

import (
	"bytes"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

// TestParamsMethodMatchesV2 pins the params method to precompiles/params'.
func TestParamsMethodMatchesV2(t *testing.T) {
	bz, err := os.ReadFile("../../../../precompiles/params/abi.json")
	require.NoError(t, err)
	v2, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	require.Len(t, v2.Methods, 1)
	want := v2.Methods[params.ParamsMethod]
	got := params.ABI.Methods[params.ParamsMethod]
	require.Equal(t, want.Sig+" "+want.String(), got.Sig+" "+got.String())
}

// TestRequiredGasMatchesV2 pins RequiredGas to V2's flat query cost.
func TestRequiredGasMatchesV2(t *testing.T) {
	args := make([]byte, 100)
	for name, method := range params.ABI.Methods {
		input := append(append([]byte{}, method.ID...), args...)
		require.Equal(t, pcommon.DefaultGasCost(args, false), params.Precompile{}.RequiredGas(input), name)
	}
	require.Equal(t, pcommon.UnknownMethodCallGas, params.Precompile{}.RequiredGas([]byte{1, 2, 3, 4}))
}

// TestParamsMatchesV2Amino compares params' JSON for each gov key with x/gov's
// amino encoding of the same values.
func TestParamsMatchesV2Amino(t *testing.T) {
	cdc := codec.NewLegacyAmino()
	g := params.Gov{
		MaxDepositPeriod:      172_800_000_000_000,
		VotingPeriod:          259_200_500_000_000,
		ExpeditedVotingPeriod: 86_400_000_000_000,
		MinDeposit:            big.NewInt(3_500_000_000),
		MinExpeditedDeposit:   big.NewInt(7_000_000_000),
		Quorum:                big.NewInt(0),
		Threshold:             big.NewInt(500_000_000_000_000_000),
		VetoThreshold:         big.NewInt(1),
		ExpeditedQuorum:       big.NewInt(667_000_000_000_000_000),
		ExpeditedThreshold:    big.NewInt(1_000_000_000_000_000_000),
	}
	require.NoError(t, g.Validate())
	dec := func(v *big.Int) sdk.Dec { return sdk.NewDecFromBigIntWithPrec(v, sdk.Precision) }
	coins := func(v *big.Int) sdk.Coins { return sdk.NewCoins(sdk.NewCoin("usei", sdk.NewIntFromBigInt(v))) }
	v2 := map[string]interface{}{
		"depositparams": govtypes.DepositParams{
			MinDeposit:          coins(g.MinDeposit),
			MaxDepositPeriod:    time.Duration(g.MaxDepositPeriod),
			MinExpeditedDeposit: coins(g.MinExpeditedDeposit),
		},
		"votingparams": govtypes.VotingParams{
			VotingPeriod:          time.Duration(g.VotingPeriod),
			ExpeditedVotingPeriod: time.Duration(g.ExpeditedVotingPeriod),
		},
		"tallyparams": govtypes.TallyParams{
			Quorum:             dec(g.Quorum),
			Threshold:          dec(g.Threshold),
			VetoThreshold:      dec(g.VetoThreshold),
			ExpeditedQuorum:    dec(g.ExpeditedQuorum),
			ExpeditedThreshold: dec(g.ExpeditedThreshold),
		},
	}
	state := newState()
	require.NoError(t, params.WriteGov(state, g))
	stored := params.ReadGov(state)
	method := params.ABI.Methods[params.ParamsMethod]
	for key, value := range v2 {
		want, err := cdc.MarshalAsJSON(value)
		require.NoError(t, err)
		input, err := method.Inputs.Pack("gov", key)
		require.NoError(t, err)
		out, err := params.Precompile{}.Run(&precompiles.Context{State: state}, append(method.ID, input...))
		require.NoError(t, err)
		values, err := method.Outputs.Unpack(out)
		require.NoError(t, err)
		require.Equal(t, string(want), values[0].(string), key)

		// The stored JSON reads back to the same values.
		changes, err := params.FromLegacy([]params.LegacyChange{{Subspace: "gov", Key: key, Value: want}})
		require.NoError(t, err)
		require.NoError(t, params.Apply(state, changes))
		require.Equal(t, stored, params.ReadGov(state), key)
	}
}

// memState is a storage-only precompiles.State.
type memState map[common.Address]map[common.Hash]common.Hash

func newState() memState { return memState{} }

func (m memState) GetState(a common.Address, k common.Hash) common.Hash { return m[a][k] }

func (m memState) SetState(a common.Address, k, v common.Hash) {
	if m[a] == nil {
		m[a] = map[common.Hash]common.Hash{}
	}
	m[a][k] = v
}

func (memState) GetBalance(common.Address) *big.Int        { panic("unsupported") }
func (memState) AddBalance(common.Address, *big.Int)       { panic("unsupported") }
func (memState) SubBalance(common.Address, *big.Int) error { panic("unsupported") }
func (memState) GetNonce(common.Address) uint64            { panic("unsupported") }
func (memState) SetNonce(common.Address, uint64)           { panic("unsupported") }
func (memState) GetCode(common.Address) []byte             { panic("unsupported") }
