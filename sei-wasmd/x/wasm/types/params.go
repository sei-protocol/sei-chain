package types

import (
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

var (
	ParamStoreKeyUploadAccess      = []byte("uploadAccess")
	ParamStoreKeyInstantiateAccess = []byte("instantiateAccess")
)

var AllAccessTypes = []AccessType{
	AccessTypeNobody,
	AccessTypeOnlyAddress,
	AccessTypeEverybody,
}

func (a AccessType) With(addr sdk.AccAddress) AccessConfig {
	switch a {
	case AccessTypeNobody:
		return AllowNobody
	case AccessTypeOnlyAddress:
		if err := sdk.VerifyAddressFormat(addr); err != nil {
			panic(err)
		}
		return AccessConfig{Permission: AccessTypeOnlyAddress, Address: addr.String()}
	case AccessTypeEverybody:
		return AllowEverybody
	}
	panic("unsupported access type")
}

func (a AccessType) String() string {
	switch a {
	case AccessTypeNobody:
		return "Nobody"
	case AccessTypeOnlyAddress:
		return "OnlyAddress"
	case AccessTypeEverybody:
		return "Everybody"
	}
	return "Unspecified"
}

func (a *AccessType) UnmarshalText(text []byte) error {
	for _, v := range AllAccessTypes {
		if v.String() == string(text) {
			*a = v
			return nil
		}
	}
	*a = AccessTypeUnspecified
	return nil
}

func (a AccessType) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *AccessType) MarshalJSONPB(_ *jsonpb.Marshaler) ([]byte, error) {
	return json.Marshal(a)
}

func (a *AccessType) UnmarshalJSONPB(_ *jsonpb.Unmarshaler, data []byte) error {
	return json.Unmarshal(data, a)
}

func (a AccessConfig) Equals(o AccessConfig) bool {
	return a.Permission == o.Permission && a.Address == o.Address
}

var (
	DefaultUploadAccess = AllowEverybody
	AllowEverybody      = AccessConfig{Permission: AccessTypeEverybody}
	AllowNobody         = AccessConfig{Permission: AccessTypeNobody}
)

// ParamKeyTable returns the parameter key table.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// DefaultParams returns default wasm parameters
func DefaultParams() Params {
	return Params{
		CodeUploadAccess:             AllowEverybody,
		InstantiateDefaultPermission: AccessTypeEverybody,
	}
}

func (p Params) String() string {
	out, err := yaml.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(out)
}

// ParamSetPairs returns the parameter set pairs.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(ParamStoreKeyUploadAccess, &p.CodeUploadAccess, validateAccessConfig),
		paramtypes.NewParamSetPair(ParamStoreKeyInstantiateAccess, &p.InstantiateDefaultPermission, validateAccessType),
	}
}

// ValidateBasic performs basic validation on wasm parameters
func (p Params) ValidateBasic() error {
	if err := validateAccessType(p.InstantiateDefaultPermission); err != nil {
		return errors.Wrap(err, "instantiate default permission")
	}
	if err := validateAccessConfig(p.CodeUploadAccess); err != nil {
		return errors.Wrap(err, "upload access")
	}
	return nil
}

func validateAccessConfig(i interface{}) error {
	v, ok := i.(AccessConfig)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return v.ValidateBasic()
}

func validateAccessType(i interface{}) error {
	a, ok := i.(AccessType)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if a == AccessTypeUnspecified {
		return sdkerrors.Wrap(ErrEmpty, "type")
	}
	for _, v := range AllAccessTypes {
		if v == a {
			return nil
		}
	}
	return sdkerrors.Wrapf(ErrInvalid, "unknown type: %q", a)
}

func (a AccessConfig) ValidateBasic() error {
	switch a.Permission {
	case AccessTypeUnspecified:
		return sdkerrors.Wrap(ErrEmpty, "type")
	case AccessTypeNobody, AccessTypeEverybody:
		if len(a.Address) != 0 {
			return sdkerrors.Wrap(ErrInvalid, "address not allowed for this type")
		}
		return nil
	case AccessTypeOnlyAddress:
		_, err := sdk.AccAddressFromBech32(a.Address)
		return err
	}
	return sdkerrors.Wrapf(ErrInvalid, "unknown type: %q", a.Permission)
}

func (a AccessConfig) Allowed(actor sdk.AccAddress) bool {
	switch a.Permission {
	case AccessTypeNobody:
		return false
	case AccessTypeEverybody:
		return true
	case AccessTypeOnlyAddress:
		return a.Address == actor.String()
	default:
		panic("unknown type")
	}
}
