package types

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/crypto/secp256k1"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

func TestToMap(t *testing.T) {
	tests := struct {
		votes   []VoteForTally
		isValid []bool
	}{
		[]VoteForTally{
			{
				Voter:        sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address()),
				Denom:        utils.MicroAtomDenom,
				ExchangeRate: sdk.NewDec(1600),
				Power:        100,
			},
		},
		[]bool{true},
	}

	pb := ExchangeRateBallot(tests.votes)
	mapData := pb.ToMap()
	for i, vote := range tests.votes {
		exchangeRate, ok := mapData[string(vote.Voter)]
		if tests.isValid[i] {
			require.True(t, ok)
			require.Equal(t, exchangeRate, vote.ExchangeRate)
		} else {
			require.False(t, ok)
		}
	}
}

func TestToCrossRate(t *testing.T) {
	data := []struct {
		base     sdk.Dec
		quote    sdk.Dec
		expected sdk.Dec
	}{
		{
			base:     sdk.NewDec(1600),
			quote:    sdk.NewDec(100),
			expected: sdk.NewDec(16),
		},
		{
			base:     sdk.NewDec(0),
			quote:    sdk.NewDec(100),
			expected: sdk.NewDec(16),
		},
		{
			base:     sdk.NewDec(1600),
			quote:    sdk.NewDec(0),
			expected: sdk.NewDec(16),
		},
	}

	pbBase := ExchangeRateBallot{}
	pbQuote := ExchangeRateBallot{}
	cb := ExchangeRateBallot{}
	for _, data := range data {
		valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())
		if !data.base.IsZero() {
			pbBase = append(pbBase, NewVoteForTally(data.base, utils.MicroAtomDenom, valAddr, 100))
		}

		pbQuote = append(pbQuote, NewVoteForTally(data.quote, utils.MicroAtomDenom, valAddr, 100))

		if !data.base.IsZero() && !data.quote.IsZero() {
			cb = append(cb, NewVoteForTally(data.base.Quo(data.quote), utils.MicroAtomDenom, valAddr, 100))
		} else {
			cb = append(cb, NewVoteForTally(sdk.ZeroDec(), utils.MicroAtomDenom, valAddr, 0))
		}
	}

	sort.Sort(cb)

	baseMapBallot := pbBase.ToMap()
	require.Equal(t, cb, pbQuote.ToCrossRateWithSort(baseMapBallot))
}

func TestSqrt(t *testing.T) {
	num := sdk.NewDecWithPrec(144, 4)
	floatNum, err := strconv.ParseFloat(num.String(), 64)
	require.NoError(t, err)

	floatNum = math.Sqrt(floatNum)
	num, err = sdk.NewDecFromStr(fmt.Sprintf("%f", floatNum))
	require.NoError(t, err)

	require.Equal(t, sdk.NewDecWithPrec(12, 2), num)
}

func TestPBPower(t *testing.T) {
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, nil)
	_, valAccAddrs, sk := GenerateRandomTestCase()
	pb := ExchangeRateBallot{}
	ballotPower := int64(0)

	for i := 0; i < len(sk.Validators()); i++ {
		power := sk.Validator(ctx, valAccAddrs[i]).GetConsensusPower(sdk.DefaultPowerReduction)
		vote := NewVoteForTally(
			sdk.ZeroDec(),
			utils.MicroAtomDenom,
			valAccAddrs[i],
			power,
		)

		pb = append(pb, vote)

		require.NotEqual(t, int64(0), vote.Power)

		ballotPower += vote.Power
	}

	require.Equal(t, ballotPower, pb.Power())

	// Mix in a fake validator, the total power should not have changed.
	pubKey := secp256k1.GenPrivKey().PubKey()
	faceValAddr := sdk.ValAddress(pubKey.Address())
	fakeVote := NewVoteForTally(
		sdk.OneDec(),
		utils.MicroAtomDenom,
		faceValAddr,
		0,
	)

	pb = append(pb, fakeVote)
	require.Equal(t, ballotPower, pb.Power())
}

func TestPBWeightedMedian(t *testing.T) {
	tests := []struct {
		inputs      []int64
		weights     []int64
		isValidator []bool
		median      sdk.Dec
	}{
		{
			// Supermajority one number
			[]int64{1, 2, 10, 100000},
			[]int64{1, 1, 100, 1},
			[]bool{true, true, true, true},
			sdk.NewDec(10),
		},
		{
			// Adding fake validator doesn't change outcome
			[]int64{1, 2, 10, 100000, 10000000000},
			[]int64{1, 1, 100, 1, 10000},
			[]bool{true, true, true, true, false},
			sdk.NewDec(10),
		},
		{
			// Tie votes
			[]int64{1, 2, 3, 4},
			[]int64{1, 100, 100, 1},
			[]bool{true, true, true, true},
			sdk.NewDec(2),
		},
		{
			// No votes
			[]int64{},
			[]int64{},
			[]bool{true, true, true, true},
			sdk.NewDec(0),
		},
	}

	for _, tc := range tests {
		pb := ExchangeRateBallot{}
		for i, input := range tc.inputs {
			valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())

			power := tc.weights[i]
			if !tc.isValidator[i] {
				power = 0
			}

			vote := NewVoteForTally(
				sdk.NewDec(int64(input)),
				utils.MicroAtomDenom,
				valAddr,
				power,
			)

			pb = append(pb, vote)
		}

		require.Equal(t, tc.median, pb.WeightedMedianWithAssertion())
	}
}

func TestPBStandardDeviation(t *testing.T) {
	tests := []struct {
		inputs            []float64
		weights           []int64
		isValidator       []bool
		standardDeviation sdk.Dec
	}{
		{
			// Supermajority one number
			[]float64{1.0, 2.0, 10.0, 100000.0},
			[]int64{1, 1, 100, 1},
			[]bool{true, true, true, true},
			sdk.NewDecWithPrec(4999500036300, OracleDecPrecision),
		},
		{
			// Adding fake validator doesn't change outcome
			[]float64{1.0, 2.0, 10.0, 100000.0, 10000000000},
			[]int64{1, 1, 100, 1, 10000},
			[]bool{true, true, true, true, false},
			sdk.NewDecWithPrec(447213595075100600, OracleDecPrecision),
		},
		{
			// Tie votes
			[]float64{1.0, 2.0, 3.0, 4.0},
			[]int64{1, 100, 100, 1},
			[]bool{true, true, true, true},
			sdk.NewDecWithPrec(122474500, OracleDecPrecision),
		},
		{
			// No votes
			[]float64{},
			[]int64{},
			[]bool{true, true, true, true},
			sdk.NewDecWithPrec(0, 0),
		},
	}

	base := math.Pow10(OracleDecPrecision)
	for _, tc := range tests {
		pb := ExchangeRateBallot{}
		for i, input := range tc.inputs {
			valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())

			power := tc.weights[i]
			if !tc.isValidator[i] {
				power = 0
			}

			vote := NewVoteForTally(
				sdk.NewDecWithPrec(int64(input*base), int64(OracleDecPrecision)),
				utils.MicroAtomDenom,
				valAddr,
				power,
			)

			pb = append(pb, vote)
		}

		require.Equal(t, tc.standardDeviation, pb.StandardDeviation(pb.WeightedMedianWithAssertion()))
	}
}

func TestPBStandardDeviationOverflow(t *testing.T) {
	valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())
	exchangeRate, err := sdk.NewDecFromStr("100000000000000000000000000000000000000000000000000000000.0")
	require.NoError(t, err)

	pb := ExchangeRateBallot{NewVoteForTally(
		sdk.ZeroDec(),
		utils.MicroAtomDenom,
		valAddr,
		2,
	), NewVoteForTally(
		exchangeRate,
		utils.MicroAtomDenom,
		valAddr,
		1,
	)}

	require.Equal(t, sdk.ZeroDec(), pb.StandardDeviation(pb.WeightedMedianWithAssertion()))
}
