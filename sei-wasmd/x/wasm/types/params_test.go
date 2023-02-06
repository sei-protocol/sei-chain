package types

import (
	"encoding/json"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateParams(t *testing.T) {
	var (
		anyAddress     sdk.AccAddress = make([]byte, ContractAddrLen)
		invalidAddress                = "invalid address"
	)

	specs := map[string]struct {
		src    Params
		expErr bool
	}{
		"all good with defaults": {
			src: DefaultParams(),
		},
		"all good with nobody": {
			src: Params{
				CodeUploadAccess:             AllowNobody,
				InstantiateDefaultPermission: AccessTypeNobody,
			},
		},
		"all good with everybody": {
			src: Params{
				CodeUploadAccess:             AllowEverybody,
				InstantiateDefaultPermission: AccessTypeEverybody,
			},
		},
		"all good with only address": {
			src: Params{
				CodeUploadAccess:             AccessTypeOnlyAddress.With(anyAddress),
				InstantiateDefaultPermission: AccessTypeOnlyAddress,
			},
		},
		"reject empty type in instantiate permission": {
			src: Params{
				CodeUploadAccess: AllowNobody,
			},
			expErr: true,
		},
		"reject unknown type in instantiate": {
			src: Params{
				CodeUploadAccess:             AllowNobody,
				InstantiateDefaultPermission: 1111,
			},
			expErr: true,
		},
		"reject invalid address in only address": {
			src: Params{
				CodeUploadAccess:             AccessConfig{Permission: AccessTypeOnlyAddress, Address: invalidAddress},
				InstantiateDefaultPermission: AccessTypeOnlyAddress,
			},
			expErr: true,
		},
		"reject CodeUploadAccess Everybody with obsolete address": {
			src: Params{
				CodeUploadAccess:             AccessConfig{Permission: AccessTypeEverybody, Address: anyAddress.String()},
				InstantiateDefaultPermission: AccessTypeOnlyAddress,
			},
			expErr: true,
		},
		"reject CodeUploadAccess Nobody with obsolete address": {
			src: Params{
				CodeUploadAccess:             AccessConfig{Permission: AccessTypeNobody, Address: anyAddress.String()},
				InstantiateDefaultPermission: AccessTypeOnlyAddress,
			},
			expErr: true,
		},
		"reject empty CodeUploadAccess": {
			src: Params{
				InstantiateDefaultPermission: AccessTypeOnlyAddress,
			},
			expErr: true,
		},
		"reject undefined permission in CodeUploadAccess": {
			src: Params{
				CodeUploadAccess:             AccessConfig{Permission: AccessTypeUnspecified},
				InstantiateDefaultPermission: AccessTypeOnlyAddress,
			},
			expErr: true,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			err := spec.src.ValidateBasic()
			if spec.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAccessTypeMarshalJson(t *testing.T) {
	specs := map[string]struct {
		src AccessType
		exp string
	}{
		"Unspecified": {src: AccessTypeUnspecified, exp: `"Unspecified"`},
		"Nobody":      {src: AccessTypeNobody, exp: `"Nobody"`},
		"OnlyAddress": {src: AccessTypeOnlyAddress, exp: `"OnlyAddress"`},
		"Everybody":   {src: AccessTypeEverybody, exp: `"Everybody"`},
		"unknown":     {src: 999, exp: `"Unspecified"`},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			got, err := json.Marshal(spec.src)
			require.NoError(t, err)
			assert.Equal(t, []byte(spec.exp), got)
		})
	}
}

func TestAccessTypeUnmarshalJson(t *testing.T) {
	specs := map[string]struct {
		src string
		exp AccessType
	}{
		"Unspecified": {src: `"Unspecified"`, exp: AccessTypeUnspecified},
		"Nobody":      {src: `"Nobody"`, exp: AccessTypeNobody},
		"OnlyAddress": {src: `"OnlyAddress"`, exp: AccessTypeOnlyAddress},
		"Everybody":   {src: `"Everybody"`, exp: AccessTypeEverybody},
		"unknown":     {src: `""`, exp: AccessTypeUnspecified},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			var got AccessType
			err := json.Unmarshal([]byte(spec.src), &got)
			require.NoError(t, err)
			assert.Equal(t, spec.exp, got)
		})
	}
}

func TestParamsUnmarshalJson(t *testing.T) {
	specs := map[string]struct {
		src string
		exp Params
	}{
		"defaults": {
			src: `{"code_upload_access": {"permission": "Everybody"},
				"instantiate_default_permission": "Everybody"}`,
			exp: DefaultParams(),
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			var val Params
			interfaceRegistry := codectypes.NewInterfaceRegistry()
			marshaler := codec.NewProtoCodec(interfaceRegistry)

			err := marshaler.UnmarshalJSON([]byte(spec.src), &val)
			require.NoError(t, err)
			assert.Equal(t, spec.exp, val)
		})
	}
}
