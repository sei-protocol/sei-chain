package types

import (
	"encoding/hex"
	"errors"
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var (
	KeyPriorityNormalizer                     = []byte("KeyPriorityNormalizer")
	KeyBaseFeePerGas                          = []byte("KeyBaseFeePerGas")
	KeyMinFeePerGas                           = []byte("KeyMinFeePerGas")
	KeyWhitelistedCwCodeHashesForDelegateCall = []byte("KeyWhitelistedCwCodeHashesForDelegateCall")
)

var DefaultPriorityNormalizer = sdk.NewDec(1)

// DefaultBaseFeePerGas determines how much usei per gas spent is
// burnt rather than go to validators (similar to base fee on
// Ethereum).
var DefaultBaseFeePerGas = sdk.NewDec(0)
var DefaultMinFeePerGas = sdk.NewDec(1000000000)

var DefaultWhitelistedCwCodeHashesForDelegateCall = generateDefaultWhitelistedCwCodeHashesForDelegateCall()

var _ paramtypes.ParamSet = (*Params)(nil)

func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func DefaultParams() Params {
	return Params{
		PriorityNormalizer:                     DefaultPriorityNormalizer,
		BaseFeePerGas:                          DefaultBaseFeePerGas,
		MinimumFeePerGas:                       DefaultMinFeePerGas,
		WhitelistedCwCodeHashesForDelegateCall: DefaultWhitelistedCwCodeHashesForDelegateCall,
	}
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriorityNormalizer, &p.PriorityNormalizer, validatePriorityNormalizer),
		paramtypes.NewParamSetPair(KeyBaseFeePerGas, &p.BaseFeePerGas, validateBaseFeePerGas),
		paramtypes.NewParamSetPair(KeyMinFeePerGas, &p.MinimumFeePerGas, validateMinFeePerGas),
		paramtypes.NewParamSetPair(KeyWhitelistedCwCodeHashesForDelegateCall, &p.WhitelistedCwCodeHashesForDelegateCall, validateWhitelistedCwHashesForDelegateCall),
	}
}

func (p Params) Validate() error {
	if err := validatePriorityNormalizer(p.PriorityNormalizer); err != nil {
		return err
	}
	if err := validateBaseFeePerGas(p.BaseFeePerGas); err != nil {
		return err
	}
	if err := validateMinFeePerGas(p.MinimumFeePerGas); err != nil {
		return err
	}
	if p.MinimumFeePerGas.LT(p.BaseFeePerGas) {
		return errors.New("minimum fee cannot be lower than base fee")
	}
	return validateWhitelistedCwHashesForDelegateCall(p.WhitelistedCwCodeHashesForDelegateCall)
}

func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

func validatePriorityNormalizer(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if !v.IsPositive() {
		return fmt.Errorf("nonpositive priority normalizer: %d", v)
	}

	return nil
}

func validateBaseFeePerGas(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("negative base fee per gas: %d", v)
	}

	return nil
}

func validateMinFeePerGas(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("negative min fee per gas: %d", v)
	}

	return nil
}

func validateWhitelistedCwHashesForDelegateCall(i interface{}) error {
	_, ok := i.([][]byte)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}

func generateDefaultWhitelistedCwCodeHashesForDelegateCall() [][]byte {
	cw20, _ := hex.DecodeString("A25D78D7ACD2EE47CC39C224E162FE79B53E6BBE6ED2A56E8C0A86593EBE6102")
	cw721, _ := hex.DecodeString("68093f9bd8f3232ecced7f0498ba0e94f7c1b04eada4f4b5a4ea59c358b66325")
	return [][]byte{cw20, cw721}
}
