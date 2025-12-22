package types

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
)

func TestValidatorProtoBuf(t *testing.T) {
	ctx := t.Context()

	val, _, err := randValidator(ctx, true, 100)
	require.NoError(t, err)

	testCases := []struct {
		msg      string
		v1       *Validator
		expPass1 bool
		expPass2 bool
	}{
		{"success validator", val, true, true},
		{"failure nil", nil, false, false},
	}
	for _, tc := range testCases {
		protoVal, err := tc.v1.ToProto()

		if tc.expPass1 {
			require.NoError(t, err, tc.msg)
		} else {
			require.Error(t, err, tc.msg)
		}

		val, err := ValidatorFromProto(protoVal)
		if tc.expPass2 {
			require.NoError(t, err, tc.msg)
			require.Equal(t, tc.v1, val, tc.msg)
		} else {
			require.Error(t, err, tc.msg)
		}
	}
}

func TestValidatorValidateBasic(t *testing.T) {
	ctx := t.Context()

	priv := NewMockPV()
	pubKey, _ := priv.GetPubKey(ctx)
	testCases := []struct {
		val *Validator
		err utils.Option[error]
	}{
		{
			val: NewValidator(pubKey, 1),
		},
		{
			val: nil,
			err: utils.Some(ErrNilValidator),
		},
		{
			val: NewValidator(pubKey, -1),
			err: utils.Some(ErrNegativeVotingPower),
		},
		{
			val: &Validator{
				PubKey:  pubKey,
				Address: nil,
			},
			err: utils.Some(ErrBadAddressSize),
		},
		{
			val: &Validator{
				PubKey:  pubKey,
				Address: []byte{'a'},
			},
			err: utils.Some(ErrBadAddressSize),
		},
	}

	for _, tc := range testCases {
		err := tc.val.ValidateBasic()
		if wantErr, ok := tc.err.Get(); ok {
			assert.True(t, errors.Is(err, wantErr))
		} else {
			assert.NoError(t, err)
		}
	}
}

// Testing util functions

// deterministicValidator returns a deterministic validator, useful for testing.
// UNSTABLE
func deterministicValidator(ctx context.Context, t *testing.T, key crypto.PrivKey) (*Validator, PrivValidator) {
	t.Helper()
	privVal := NewMockPV()
	privVal.PrivKey = key
	var votePower int64 = 50
	pubKey, err := privVal.GetPubKey(ctx)
	require.NoError(t, err, "could not retrieve pubkey")
	val := NewValidator(pubKey, votePower)
	return val, privVal
}
