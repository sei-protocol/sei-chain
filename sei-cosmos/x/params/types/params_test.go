package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCosmosGasParams(t *testing.T) {
	// Verify validation: numerator and denominator != 0
	invalidCosmosGasParam := NewCosmosGasParams(0, 1)
	err := invalidCosmosGasParam.Validate()
	require.NotNil(t, err)

	invalidCosmosGasParam = NewCosmosGasParams(1, 0)
	err = invalidCosmosGasParam.Validate()
	require.NotNil(t, err)
}

func TestValidateDisabledMsgTypeURLs(t *testing.T) {
	specs := map[string]struct {
		value  interface{}
		expErr bool
	}{
		"nil":                {value: []string(nil)},
		"empty":              {value: []string{}},
		"type URLs":          {value: []string{"/cosmos.bank.v1beta1.MsgSend", "/cosmos.staking.v1beta1.MsgDelegate"}},
		"empty entry":        {value: []string{"/cosmos.bank.v1beta1.MsgSend", ""}, expErr: true},
		"slash only":         {value: []string{"/"}, expErr: true},
		"no leading slash":   {value: []string{"cosmos.bank.v1beta1.MsgSend"}, expErr: true},
		"not a string slice": {value: "/cosmos.bank.v1beta1.MsgSend", expErr: true},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			err := validateDisabledMsgTypeURLs(spec.value)
			if spec.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
