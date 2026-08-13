package baseapp

import (
	"errors"
	"fmt"
	"time"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Paramspace defines the parameter subspace to be used for the paramstore.
const Paramspace = "baseapp"

// Parameter store keys for all the consensus parameter types.
var (
	ParamStoreKeyEvidenceParams  = []byte("EvidenceParams")
	ParamStoreKeyValidatorParams = []byte("ValidatorParams")
	ParamStoreKeyBlockParams     = []byte("BlockParams")
	ParamStoreKeyVersionParams   = []byte("VersionParams")
	ParamStoreKeySynchronyParams = []byte("SynchronyParams")
	ParamStoreKeyTimeoutParams   = []byte("TimeoutParams")
	ParamStoreKeyABCIParams      = []byte("ABCIParams")
)

// ParamStore defines the interface the parameter store used by the BaseApp must
// fulfill.
type ParamStore interface {
	Get(ctx sdk.Context, key []byte, ptr interface{})
	Has(ctx sdk.Context, key []byte) bool
	Set(ctx sdk.Context, key []byte, param interface{})
}

// ValidateBlockParams defines a stateless validation on BlockParams. This function
// is called whenever the parameters are updated or stored.
func ValidateBlockParams(i interface{}) error {
	v, ok := i.(tmproto.BlockParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.MaxBytes <= 0 {
		return fmt.Errorf("block maximum bytes must be positive: %d", v.MaxBytes)
	}

	if v.MaxGas < -1 {
		return fmt.Errorf("block maximum gas must be greater than or equal to -1: %d", v.MaxGas)
	}

	if v.MinTxsInBlock < 0 {
		return fmt.Errorf("block min txs in block must be non-negative: %d", v.MinTxsInBlock)
	}

	return nil
}

// ValidateEvidenceParams defines a stateless validation on EvidenceParams. This
// function is called whenever the parameters are updated or stored.
func ValidateEvidenceParams(i interface{}) error {
	v, ok := i.(tmproto.EvidenceParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.MaxAgeNumBlocks <= 0 {
		return fmt.Errorf("evidence maximum age in blocks must be positive: %d", v.MaxAgeNumBlocks)
	}

	if v.MaxAgeDuration <= 0 {
		return fmt.Errorf("evidence maximum age time duration must be positive: %v", v.MaxAgeDuration)
	}

	if v.MaxBytes < 0 {
		return fmt.Errorf("maximum evidence bytes must be non-negative: %v", v.MaxBytes)
	}

	return nil
}

// ValidateValidatorParams defines a stateless validation on ValidatorParams. This
// function is called whenever the parameters are updated or stored.
func ValidateValidatorParams(i interface{}) error {
	v, ok := i.(tmproto.ValidatorParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if len(v.PubKeyTypes) == 0 {
		return errors.New("validator allowed pubkey types must not be empty")
	}

	return nil
}

func ValidateVersionParams(i interface{}) error {
	_, ok := i.(tmproto.VersionParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}

func ValidateSynchronyParams(i interface{}) error {
	v, ok := i.(tmproto.SynchronyParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if err := validateDurationPointer(v.MessageDelay, "message delay"); err != nil {
		return err
	}

	if err := validateDurationPointer(v.Precision, "synchrony precision"); err != nil {
		return err
	}

	return nil
}

func ValidateTimeoutParams(i interface{}) error {
	v, ok := i.(tmproto.TimeoutParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if err := validateDurationPointer(v.Propose, "propose timeout"); err != nil {
		return err
	}

	if err := validateDurationPointer(v.ProposeDelta, "propose delta timeout"); err != nil {
		return err
	}

	if err := validateDurationPointer(v.Vote, "vote timeout"); err != nil {
		return err
	}

	if err := validateDurationPointer(v.VoteDelta, "vote delta timeout"); err != nil {
		return err
	}

	if err := validateDurationPointer(v.Commit, "commit timeout"); err != nil {
		return err
	}

	return nil
}

func ValidateABCIParams(i interface{}) error {
	v, ok := i.(tmproto.ABCIParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.VoteExtensionsEnableHeight < 0 {
		return errors.New("invalid vote extensions enable height")
	}

	return nil
}

func validateDurationPointer(i *time.Duration, name string) error {
	if i == nil || *i < 0 {
		return fmt.Errorf("invalid %s", name)
	}
	return nil
}
