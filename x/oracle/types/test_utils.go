package types

import (
	"math"
	"math/rand"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/tendermint/tendermint/crypto/secp256k1"
	tmprotocrypto "github.com/tendermint/tendermint/proto/tendermint/crypto"
)

// OracleDecPrecision nolint
const OracleDecPrecision = 8

// GenerateRandomTestCase nolint
// nolint:staticcheck
func GenerateRandomTestCase() (rates []float64, valValAddrs []sdk.ValAddress, stakingKeeper DummyStakingKeeper) {
	valValAddrs = []sdk.ValAddress{}
	mockValidators := []MockValidator{}

	base := math.Pow10(OracleDecPrecision)

	r := rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	numInputs := 10 + (r.Int() % 100)
	for i := 0; i < numInputs; i++ {
		rate := float64(int64(r.Float64()*base)) / base
		rates = append(rates, rate)

		pubKey := secp256k1.GenPrivKey().PubKey()
		valValAddr := sdk.ValAddress(pubKey.Address())
		valValAddrs = append(valValAddrs, valValAddr)

		power := r.Int63()%1000 + 1
		mockValidator := NewMockValidator(valValAddr, power)
		mockValidators = append(mockValidators, mockValidator)
	}

	stakingKeeper = NewDummyStakingKeeper(mockValidators)

	return
}

var _ StakingKeeper = DummyStakingKeeper{}

// DummyStakingKeeper dummy staking keeper to test ballot
type DummyStakingKeeper struct {
	validators []MockValidator
}

// NewDummyStakingKeeper returns new DummyStakingKeeper instance
func NewDummyStakingKeeper(validators []MockValidator) DummyStakingKeeper {
	return DummyStakingKeeper{
		validators: validators,
	}
}

// Validators nolint
func (sk DummyStakingKeeper) Validators() []MockValidator {
	return sk.validators
}

// Validator nolint
func (sk DummyStakingKeeper) Validator(_ sdk.Context, address sdk.ValAddress) stakingtypes.ValidatorI {
	for _, validator := range sk.validators {
		if validator.GetOperator().Equals(address) {
			return validator
		}
	}

	return nil
}

// TotalBondedTokens nolint
func (DummyStakingKeeper) TotalBondedTokens(_ sdk.Context) sdk.Int {
	return sdk.ZeroInt()
}

// Slash nolint
func (DummyStakingKeeper) Slash(sdk.Context, sdk.ConsAddress, int64, int64, sdk.Dec) {}

// ValidatorsPowerStoreIterator
func (DummyStakingKeeper) ValidatorsPowerStoreIterator(_ sdk.Context) sdk.Iterator {
	return sdk.KVStoreReversePrefixIterator(nil, nil)
}

// Jail
func (DummyStakingKeeper) Jail(sdk.Context, sdk.ConsAddress) {
}

// GetLastValidatorPower
func (sk DummyStakingKeeper) GetLastValidatorPower(ctx sdk.Context, operator sdk.ValAddress) (power int64) {
	return sk.Validator(ctx, operator).GetConsensusPower(sdk.DefaultPowerReduction)
}

// MaxValidators returns the maximum amount of bonded validators
func (DummyStakingKeeper) MaxValidators(sdk.Context) uint32 {
	return 100
}

// PowerReduction - is the amount of staking tokens required for 1 unit of consensus-engine power
func (DummyStakingKeeper) PowerReduction(_ sdk.Context) (res sdk.Int) {
	res = sdk.DefaultPowerReduction
	return
}

// MockValidator
type MockValidator struct {
	power    int64
	operator sdk.ValAddress
}

var _ stakingtypes.ValidatorI = MockValidator{}

func (MockValidator) IsJailed() bool                          { return false }
func (MockValidator) GetMoniker() string                      { return "" }
func (MockValidator) GetStatus() stakingtypes.BondStatus      { return stakingtypes.Bonded }
func (MockValidator) IsBonded() bool                          { return true }
func (MockValidator) IsUnbonded() bool                        { return false }
func (MockValidator) IsUnbonding() bool                       { return false }
func (v MockValidator) GetOperator() sdk.ValAddress           { return v.operator }
func (MockValidator) ConsPubKey() (cryptotypes.PubKey, error) { return nil, nil }
func (MockValidator) TmConsPublicKey() (tmprotocrypto.PublicKey, error) {
	return tmprotocrypto.PublicKey{}, nil
}
func (MockValidator) GetConsAddr() (sdk.ConsAddress, error) { return nil, nil }
func (v MockValidator) GetTokens() sdk.Int {
	return sdk.TokensFromConsensusPower(v.power, sdk.DefaultPowerReduction)
}

func (v MockValidator) GetBondedTokens() sdk.Int {
	return sdk.TokensFromConsensusPower(v.power, sdk.DefaultPowerReduction)
}
func (v MockValidator) GetConsensusPower(_ sdk.Int) int64           { return v.power }
func (v *MockValidator) SetConsensusPower(power int64)              { v.power = power }
func (v MockValidator) GetCommission() sdk.Dec                      { return sdk.ZeroDec() }
func (v MockValidator) GetMinSelfDelegation() sdk.Int               { return sdk.OneInt() }
func (v MockValidator) GetDelegatorShares() sdk.Dec                 { return sdk.NewDec(v.power) }
func (v MockValidator) TokensFromShares(sdk.Dec) sdk.Dec            { return sdk.ZeroDec() }
func (v MockValidator) TokensFromSharesTruncated(sdk.Dec) sdk.Dec   { return sdk.ZeroDec() }
func (v MockValidator) TokensFromSharesRoundUp(sdk.Dec) sdk.Dec     { return sdk.ZeroDec() }
func (v MockValidator) SharesFromTokens(_ sdk.Int) (sdk.Dec, error) { return sdk.ZeroDec(), nil }
func (v MockValidator) SharesFromTokensTruncated(_ sdk.Int) (sdk.Dec, error) {
	return sdk.ZeroDec(), nil
}

func NewMockValidator(valAddr sdk.ValAddress, power int64) MockValidator {
	return MockValidator{
		power:    power,
		operator: valAddr,
	}
}
