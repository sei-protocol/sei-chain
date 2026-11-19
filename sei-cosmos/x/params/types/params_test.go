package types

import (
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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
	registry := codectypes.NewInterfaceRegistry()
	sdk.RegisterInterfaces(registry)
	testdata.RegisterInterfaces(registry)
	testMsg := sdk.MsgTypeURL(&testdata.TestMsg{})
	createDog := sdk.MsgTypeURL(&testdata.MsgCreateDog{})
	specs := map[string]struct {
		value  interface{}
		expErr bool
	}{
		"nil":                    {value: []string(nil)},
		"empty":                  {value: []string{}},
		"registered messages":    {value: []string{testMsg, createDog}},
		"misspelled message":     {value: []string{testMsg, "/testdata.MsgCreateDg"}, expErr: true},
		"registered non-message": {value: []string{"/" + proto.MessageName(&testdata.Dog{})}, expErr: true},
		"empty entry":            {value: []string{testMsg, ""}, expErr: true},
		"slash only":             {value: []string{"/"}, expErr: true},
		"no leading slash":       {value: []string{strings.TrimPrefix(testMsg, "/")}, expErr: true},
		"not a string slice":     {value: testMsg, expErr: true},
	}
	validate := disabledMsgTypeURLsValidator(registry)
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			err := validate(spec.value)
			if spec.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
