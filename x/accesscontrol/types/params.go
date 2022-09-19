package types

import (
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

// Parameter store keys
var (
	KeyMessageDependencyMapping = []byte("MessageDependencyMapping")
)

func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyMessageDependencyMapping, &p.MessageDependencyMapping, validateMessageDependencyMapping),
	}
}

func NewParams(messageDependencyMapping []MessageDependencyMapping) Params {
	return Params{
		MessageDependencyMapping: messageDependencyMapping,
	}
}

// default access control module parameters
func DefaultParams() Params {
	return NewParams([]MessageDependencyMapping{})
}

func (p Params) Validate() error {
	if err := validateMessageDependencyMapping(p.MessageDependencyMapping); err != nil {
		return err
	}
	return nil
}

// TODO(bweng):: add validation logic for msg dep mapping
func validateMessageDependencyMapping(i interface{}) error {
	return nil
}
