package teststaking

import (
	tmcrypto "github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	cryptocodec "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/codec"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// GetTmConsPubKey gets the validator's public key as a tmcrypto.PubKey.
func GetTmConsPubKey(v types.Validator) (tmcrypto.PubKey, error) {
	pk, err := v.ConsPubKey()
	if err != nil {
		return tmcrypto.PubKey{}, err
	}

	return cryptocodec.ToTmPubKeyInterface(pk)
}

// ToTmValidator casts an SDK validator to a tendermint type Validator.
func ToTmValidator(v types.Validator, r sdk.Int) (*tmtypes.Validator, error) {
	tmPk, err := GetTmConsPubKey(v)
	if err != nil {
		return nil, err
	}

	return tmtypes.NewValidator(tmPk, v.ConsensusPower(r)), nil
}

// ToTmValidators casts all validators to the corresponding tendermint type.
func ToTmValidators(v types.Validators, r sdk.Int) ([]*tmtypes.Validator, error) {
	validators := make([]*tmtypes.Validator, len(v))
	var err error
	for i, val := range v {
		validators[i], err = ToTmValidator(val, r)
		if err != nil {
			return nil, err
		}
	}

	return validators, nil
}
