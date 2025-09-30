package types

import (
	"fmt"
	"time"

	yaml "gopkg.in/yaml.v2"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// Default period for deposits & voting
const (
	DefaultPeriod          time.Duration = time.Hour * 24 * 2 // 2 days
	DefaultExpeditedPeriod time.Duration = time.Hour * 24     // 1 day
)

// Default governance params
var (
	DefaultMinDepositTokens          = sdk.NewInt(10000000)
	DefaultMinExpeditedDepositTokens = sdk.NewInt(20000000)
	DefaultQuorum                    = sdk.NewDecWithPrec(334, 3)
	DefaultExpeditedQuorum           = sdk.NewDecWithPrec(667, 3)
	DefaultThreshold                 = sdk.NewDecWithPrec(5, 1)
	DefaultExpeditedThreshold        = sdk.NewDecWithPrec(667, 3)
	DefaultVetoThreshold             = sdk.NewDecWithPrec(334, 3)
)

// Parameter store key
var (
	ParamStoreKeyDepositParams = []byte("depositparams")
	ParamStoreKeyVotingParams  = []byte("votingparams")
	ParamStoreKeyTallyParams   = []byte("tallyparams")
)

// ParamKeyTable - Key declaration for parameters
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable(
		paramtypes.NewParamSetPair(ParamStoreKeyDepositParams, DepositParams{}, validateDepositParams),
		paramtypes.NewParamSetPair(ParamStoreKeyVotingParams, VotingParams{}, validateVotingParams),
		paramtypes.NewParamSetPair(ParamStoreKeyTallyParams, TallyParams{}, validateTallyParams),
	)
}

// NewDepositParams creates a new DepositParams object
func NewDepositParams(minDeposit sdk.Coins, minExpeditedDeposit sdk.Coins, maxDepositPeriod time.Duration) DepositParams {
	return DepositParams{
		MinDeposit:          minDeposit,
		MaxDepositPeriod:    maxDepositPeriod,
		MinExpeditedDeposit: minExpeditedDeposit,
	}
}

// DefaultDepositParams default parameters for deposits
func DefaultDepositParams() DepositParams {
	return NewDepositParams(
		sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, DefaultMinDepositTokens)),
		sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, DefaultMinExpeditedDepositTokens)),
		DefaultPeriod,
	)
}

// String implements stringer insterface
func (dp DepositParams) String() string {
	out, _ := yaml.Marshal(dp)
	return string(out)
}

// GetMinimumDeposit returns minimum deposit based on the value isExpedited
func (dp DepositParams) GetMinimumDeposit(isExpedited bool) sdk.Coins {
	if isExpedited {
		return dp.MinExpeditedDeposit
	}
	return dp.MinDeposit
}

// Equal checks equality of DepositParams
func (dp DepositParams) Equal(dp2 DepositParams) bool {
	return dp.MinDeposit.IsEqual(dp2.MinDeposit) &&
		dp.MinExpeditedDeposit.IsEqual(dp2.MinExpeditedDeposit) &&
		dp.MaxDepositPeriod == dp2.MaxDepositPeriod
}

func validateDepositParams(i interface{}) error {
	v, ok := i.(DepositParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if !v.MinDeposit.IsValid() {
		return fmt.Errorf("invalid minimum deposit: %s", v.MinDeposit)
	}
	if !v.MinExpeditedDeposit.IsValid() {
		return fmt.Errorf("invalid minimum expedited deposit: %s", v.MinExpeditedDeposit)
	}
	if v.MinExpeditedDeposit.IsAllLTE(v.MinDeposit) {
		return fmt.Errorf("minimum expedited deposit: %s should be larger than minimum deposit: %s", v.MinExpeditedDeposit, v.MinDeposit)
	}
	if v.MaxDepositPeriod <= 0 {
		return fmt.Errorf("maximum deposit period must be positive: %d", v.MaxDepositPeriod)
	}

	return nil
}

// NewTallyParams creates a new TallyParams object
func NewTallyParams(quorum, expeditedQuorum, threshold, expeditedThreshold, vetoThreshold sdk.Dec) TallyParams {
	return TallyParams{
		Quorum:             quorum,
		ExpeditedQuorum:    expeditedQuorum,
		Threshold:          threshold,
		VetoThreshold:      vetoThreshold,
		ExpeditedThreshold: expeditedThreshold,
	}
}

// DefaultTallyParams default parameters for tallying
func DefaultTallyParams() TallyParams {
	return NewTallyParams(DefaultQuorum, DefaultExpeditedQuorum, DefaultThreshold, DefaultExpeditedThreshold, DefaultVetoThreshold)
}

// GetThreshold returns threshold based on the value isExpedited
func (tp TallyParams) GetThreshold(isExpedited bool) sdk.Dec {
	if isExpedited {
		return tp.ExpeditedThreshold
	}
	return tp.Threshold
}

// GetQuorum returns quorum based on the value isExpedited
func (tp TallyParams) GetQuorum(isExpedited bool) sdk.Dec {
	if isExpedited {
		return tp.ExpeditedQuorum
	}
	return tp.Quorum
}

// Equal checks equality of TallyParams
func (tp TallyParams) Equal(other TallyParams) bool {
	return tp.Quorum.Equal(other.Quorum) &&
		tp.ExpeditedQuorum.Equal(other.ExpeditedQuorum) &&
		tp.Threshold.Equal(other.Threshold) &&
		tp.ExpeditedThreshold.Equal(other.ExpeditedThreshold) &&
		tp.VetoThreshold.Equal(other.VetoThreshold)
}

// String implements stringer insterface
func (tp TallyParams) String() string {
	out, _ := yaml.Marshal(tp)
	return string(out)
}

func validateTallyParams(i interface{}) error {
	v, ok := i.(TallyParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.Quorum.IsNegative() {
		return fmt.Errorf("quorom cannot be negative: %s", v.Quorum)
	}
	if v.Quorum.GT(sdk.OneDec()) {
		return fmt.Errorf("quorom too large: %s", v)
	}
	if v.ExpeditedQuorum.IsNegative() {
		return fmt.Errorf("expedited quorom cannot be negative: %s", v.ExpeditedQuorum)
	}
	if v.ExpeditedQuorum.GT(sdk.OneDec()) {
		return fmt.Errorf("expedited quorom too large: %s", v.ExpeditedQuorum)
	}
	if v.ExpeditedQuorum.LTE(v.Quorum) {
		return fmt.Errorf("expedited quorum %s, must be greater than the regular quorum %s", v.ExpeditedQuorum, v.Quorum)
	}
	if !v.Threshold.IsPositive() {
		return fmt.Errorf("vote threshold must be positive: %s", v.Threshold)
	}
	if v.Threshold.GT(sdk.OneDec()) {
		return fmt.Errorf("vote threshold too large: %s", v.Threshold)
	}
	if !v.ExpeditedThreshold.IsPositive() {
		return fmt.Errorf("expedited ote threshold must be positive: %s", v.ExpeditedThreshold)
	}
	if v.ExpeditedThreshold.GT(sdk.OneDec()) {
		return fmt.Errorf("expedited vote threshold too large: %s", v.ExpeditedThreshold)
	}
	if v.ExpeditedThreshold.LTE(v.Threshold) {
		return fmt.Errorf("expedited vote threshold %s, must be greater than the regular threshold %s", v.ExpeditedThreshold, v.Threshold)
	}
	if !v.VetoThreshold.IsPositive() {
		return fmt.Errorf("veto threshold must be positive: %s", v.Threshold)
	}
	if v.VetoThreshold.GT(sdk.OneDec()) {
		return fmt.Errorf("veto threshold too large: %s", v)
	}

	return nil
}

// NewVotingParams creates a new VotingParams object
func NewVotingParams(votingPeriod time.Duration, expeditedPeriod time.Duration) VotingParams {
	return VotingParams{
		VotingPeriod:          votingPeriod,
		ExpeditedVotingPeriod: expeditedPeriod,
	}
}

// GetVotingPeriod returns voting period based on whether isExpedited is requested.
func (vp VotingParams) GetVotingPeriod(isExpedited bool) time.Duration {
	if isExpedited {
		return vp.ExpeditedVotingPeriod
	}
	return vp.VotingPeriod
}

// DefaultVotingParams default parameters for voting
func DefaultVotingParams() VotingParams {
	return NewVotingParams(DefaultPeriod, DefaultExpeditedPeriod)
}

// Equal checks equality of TallyParams
func (vp VotingParams) Equal(other VotingParams) bool {
	return vp.VotingPeriod == other.VotingPeriod && vp.ExpeditedVotingPeriod == other.ExpeditedVotingPeriod
}

// String implements stringer interface
func (vp VotingParams) String() string {
	out, _ := yaml.Marshal(vp)
	return string(out)
}

func validateVotingParams(i interface{}) error {
	v, ok := i.(VotingParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.VotingPeriod <= 0 {
		return fmt.Errorf("voting period must be positive: %s", v.VotingPeriod)
	}

	if v.ExpeditedVotingPeriod <= 0 {
		return fmt.Errorf("expedited voting period must be positive: %s", v.ExpeditedVotingPeriod)
	}

	if v.ExpeditedVotingPeriod >= v.VotingPeriod {
		return fmt.Errorf("expedited voting period %s must less than the regular voting period %s", v.ExpeditedVotingPeriod, v.VotingPeriod)
	}

	return nil
}

// Params returns all of the governance params
type Params struct {
	VotingParams  VotingParams  `json:"voting_params" yaml:"voting_params"`
	TallyParams   TallyParams   `json:"tally_params" yaml:"tally_params"`
	DepositParams DepositParams `json:"deposit_params" yaml:"deposit_params"`
}

func (gp Params) String() string {
	return gp.VotingParams.String() + "\n" +
		gp.TallyParams.String() + "\n" + gp.DepositParams.String()
}

// NewParams creates a new gov Params instance
func NewParams(vp VotingParams, tp TallyParams, dp DepositParams) Params {
	return Params{
		VotingParams:  vp,
		DepositParams: dp,
		TallyParams:   tp,
	}
}

// DefaultParams default governance params
func DefaultParams() Params {
	return NewParams(DefaultVotingParams(), DefaultTallyParams(), DefaultDepositParams())
}
