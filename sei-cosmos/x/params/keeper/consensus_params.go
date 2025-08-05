package keeper

import (
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/x/params/types"
)

// ConsensusParamsKeyTable returns an x/params module keyTable to be used in
// the BaseApp's ParamStore. The KeyTable registers the types along with the
// standard validation functions. Applications can choose to adopt this KeyTable
// or provider their own when the existing validation functions do not suite their
// needs.
func ConsensusParamsKeyTable() types.KeyTable {
	return types.NewKeyTable(
		types.NewParamSetPair(
			baseapp.ParamStoreKeyBlockParams, tmproto.BlockParams{}, baseapp.ValidateBlockParams,
		),
		types.NewParamSetPair(
			baseapp.ParamStoreKeyEvidenceParams, tmproto.EvidenceParams{}, baseapp.ValidateEvidenceParams,
		),
		types.NewParamSetPair(
			baseapp.ParamStoreKeyValidatorParams, tmproto.ValidatorParams{}, baseapp.ValidateValidatorParams,
		),
		types.NewParamSetPair(
			baseapp.ParamStoreKeyVersionParams, tmproto.VersionParams{}, baseapp.ValidateVersionParams,
		),
		types.NewParamSetPair(
			baseapp.ParamStoreKeySynchronyParams, tmproto.SynchronyParams{}, baseapp.ValidateSynchronyParams,
		),
		types.NewParamSetPair(
			baseapp.ParamStoreKeyTimeoutParams, tmproto.TimeoutParams{}, baseapp.ValidateTimeoutParams,
		),
		types.NewParamSetPair(
			baseapp.ParamStoreKeyABCIParams, tmproto.ABCIParams{}, baseapp.ValidateABCIParams,
		),
	)
}
